// Package mpaydb 直接读写游戏本地的 <game>-64-mpay.db（MPay 账号库）。
//
// 为什么能写：网易的 mpay.db 是加密 SQLite，加密/哈希变换在官方单独构建的
// MPay Wasm 模块里（User::Save 变换）。这个 wasm 不带任何导入（连 WASI 都
// 没有），导出 alloc / invoke / result_ptr / result_len / release / memory，
// 用纯 Go 的 wazero 就能跑，不需要 CGO。
//
// 协议（来自参考实现 idv-login 的 mpay_wasm.py / mpay_db_sync.py，ABI 4）：
//
//	invoke(操作码 int, db指针, db长度, argsJSON指针, argsJSON长度) -> 0=成功
//	操作码：4=rename_account 5=put_account 6=delete_account 7=list_accounts
//	args 是 JSON；list_accounts 的结果是 JSON（accounts + errors）；
//	put_account 的结果第一个字节是"是否变更"，其余是**新的整库字节**，
//	调用方拿它覆盖 db 文件。
//
// 与 idv-login 的差异：它用 wasmtime（Python），我们用 wazero（Go），
// ABI 完全一致。wasm 文件来自 idv-login 的 src/resources（有 sha256 校验）。
package mpaydb

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

//go:embed idv-db.wasm
var wasmBinary []byte

// manifest 里的 sha256，加载时校验，防止文件损坏/被换。
const expectedSHA256 = "45ddce2c48ebdf1846090c6fc3c42cc27d95a9aed2ac4a823409f7b21e58454e"

// 操作码（ABI 4）。
const (
	OpRename = 4
	OpPut    = 5
	OpDelete = 6
	OpList   = 7
)

// MaxDBBytes 库大小上限（与参考实现一致：32MB）。
const MaxDBBytes = 32 << 20

// DB 一个已编译的 wasm 模块。并发不安全：每次 Invoke 新建实例，
// 和参考实现一致（wasm 内部有状态）。
type DB struct {
	rt       wazero.Runtime
	compiled wazero.CompiledModule
}

// New 校验并编译 wasm。进程内做一次，后面所有操作共用。
func New() (*DB, error) {
	sum := sha256.Sum256(wasmBinary)
	if hex.EncodeToString(sum[:]) != expectedSHA256 {
		return nil, fmt.Errorf("mpay wasm 校验失败（sha256=%s）", hex.EncodeToString(sum[:]))
	}
	d := &DB{rt: wazero.NewRuntime(context.Background())}
	compiled, err := d.rt.CompileModule(context.Background(), wasmBinary)
	if err != nil {
		d.rt.Close(nil)
		return nil, fmt.Errorf("编译 mpay wasm 失败: %w", err)
	}
	d.compiled = compiled
	return d, nil
}

// Close 释放运行时。
func (d *DB) Close() {
	if d.rt != nil {
		d.rt.Close(nil)
	}
}

// Invoke 跑一次操作：传入整库字节 + 参数。
//
// put 操作返回 (changed, newDB)；list 类操作 newDB 为空、result 是 JSON。
// 每次调用新建一个 wasm 实例（参考实现同样如此）。
func (d *DB) Invoke(op int, database []byte, args map[string]any) (changed bool, newDB []byte, result []byte, err error) {
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return false, nil, nil, err
	}
	return d.InvokeJSON(op, database, argsJSON)
}

// InvokeJSON 同 Invoke，但参数是已编码的 JSON（诊断探针用来试参数形状）。
func (d *DB) InvokeJSON(op int, database []byte, argsJSON []byte) (changed bool, newDB []byte, result []byte, err error) {
	ctx := context.Background()
	if len(database) > MaxDBBytes {
		return false, nil, nil, fmt.Errorf("mpay.db 超过大小上限（%d 字节）", len(database))
	}
	if len(argsJSON) > 1<<20 {
		return false, nil, nil, fmt.Errorf("mpay 操作参数过大（%d 字节）", len(argsJSON))
	}

	inst, err := d.rt.InstantiateModule(ctx, d.compiled, wazero.NewModuleConfig().WithName("mpaydb"))
	if err != nil {
		return false, nil, nil, fmt.Errorf("实例化 mpay wasm 失败: %w", err)
	}
	defer inst.Close(ctx)

	memory := inst.Memory()
	alloc := inst.ExportedFunction("alloc")
	invoke := inst.ExportedFunction("invoke")
	resultPtrFn := inst.ExportedFunction("result_ptr")
	resultLenFn := inst.ExportedFunction("result_len")
	if alloc == nil || invoke == nil || resultPtrFn == nil || resultLenFn == nil || memory == nil {
		return false, nil, nil, fmt.Errorf("mpay wasm 导出接口不全")
	}

	// alloc(len) -> 指针。空库/空参数也按 1 字节占位：0 指针会被 wasm 拒绝
	//（参考实现在 0 指针时同样报"输入分配失败"）。
	dbLen, argLen := len(database), len(argsJSON)
	dbPtr, err := call1(alloc, uint64(max1(dbLen)))
	if err != nil {
		return false, nil, nil, err
	}
	argPtr, err := call1(alloc, uint64(max1(argLen)))
	if err != nil {
		return false, nil, nil, err
	}
	if dbPtr == 0 || argPtr == 0 {
		return false, nil, nil, fmt.Errorf("mpay wasm 输入分配失败")
	}
	if len(database) > 0 && !memory.Write(uint32(dbPtr), database) {
		return false, nil, nil, fmt.Errorf("写 db 内存失败")
	}
	if len(argsJSON) > 0 && !memory.Write(uint32(argPtr), argsJSON) {
		return false, nil, nil, fmt.Errorf("写参数内存失败")
	}

	ret, err := invoke.Call(ctx, uint64(op), dbPtr, uint64(len(database)), argPtr, uint64(len(argsJSON)))
	if err != nil {
		return false, nil, nil, fmt.Errorf("mpay wasm invoke 失败: %w", err)
	}
	if ret[0] != 0 {
		return false, nil, nil, fmt.Errorf("mpay wasm invoke 返回 status=%d", ret[0])
	}

	rp, err := call0(resultPtrFn)
	if err != nil {
		return false, nil, nil, err
	}
	rl, err := call0(resultLenFn)
	if err != nil {
		return false, nil, nil, err
	}
	if rp == 0 || rl > MaxDBBytes {
		return false, nil, nil, fmt.Errorf("mpay wasm 返回了无效结果")
	}
	out, ok := memory.Read(uint32(rp), uint32(rl))
	if !ok {
		return false, nil, nil, fmt.Errorf("读结果内存失败")
	}
	res := make([]byte, len(out))
	copy(res, out)

	if op == OpPut {
		if len(res) < 2 || (res[0] != 0 && res[0] != 1) {
			return false, nil, nil, fmt.Errorf("mpay wasm put 结果无效")
		}
		return res[0] == 1, res[1:], nil, nil
	}
	return false, nil, res, nil
}

func max1(n int) int {
	if n == 0 {
		return 1
	}
	return n
}

func call1(f api.Function, n uint64) (uint64, error) {
	ret, err := f.Call(context.Background(), n)
	if err != nil {
		return 0, fmt.Errorf("mpay wasm alloc 失败: %w", err)
	}
	return ret[0], nil
}

func call0(f api.Function) (uint64, error) {
	ret, err := f.Call(context.Background())
	if err != nil {
		return 0, fmt.Errorf("mpay wasm result 调用失败: %w", err)
	}
	return ret[0], nil
}
