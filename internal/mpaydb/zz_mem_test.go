package mpaydb

import (
	"github.com/tetratelabs/wazero"
	"context"
	"os"
	"testing"
)

// 内存布局探测：alloc 的两个指针是否重叠、写入后内容是否完好。
func TestZZMemLayout(t *testing.T) {
	src := os.Getenv("MPAYDB_PUT_DB")
	if src == "" {
		t.Skip("跳过")
	}
	original, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	d, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	inst, err := d.rt.InstantiateModule(ctx, d.compiled, wazero.NewModuleConfig().WithName("mpaydb-memtest"))
	if err != nil {
		t.Fatal(err)
	}
	defer inst.Close(ctx)
	mem := inst.Memory()
	alloc := inst.ExportedFunction("alloc")
	p1, err := alloc.Call(ctx, uint64(len(original)))
	if err != nil {
		t.Fatal(err)
	}
	p2, err := alloc.Call(ctx, uint64(600))
	if err != nil {
		t.Fatal(err)
	}
	dbPtr, argPtr := p1[0], p2[0]
	t.Logf("dbPtr=%d argPtr=%d dbLen=%d 重叠=%v", dbPtr, argPtr, len(original),
		argPtr < dbPtr+uint64(len(original)) && argPtr+600 > dbPtr)
	ok1 := mem.Write(uint32(dbPtr), original)
	ok2 := mem.Write(uint32(argPtr), make([]byte, 600))
	t.Logf("写 db=%v 写 args=%v", ok1, ok2)
	back, ok := mem.Read(uint32(dbPtr), 16)
	t.Logf("回读 db 头 16 字节: ok=%v %x", ok, back)
}

