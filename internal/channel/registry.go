package channel

// 渠道注册表。
//
// 所有渠道在 init 阶段注册自己，之后全项目只通过 Lookup 拿 Provider，
// 不再散落 switch / map。加渠道只需要新增一个文件并在 init 里 Register。

import (
	"errors"
	"sort"
)

// ErrNotWired 表示这条登录方式已经声明，但流程还没接到新框架上。
//
// 阶段迁移期用：接口先立起来，行为仍旧走老路径。等到对应渠道迁完，
// 这个错误就不会再出现。它存在的意义是——宁可明确报"没接"，
// 也不要静默返回一个空凭证。
var ErrNotWired = errors.New("该登录方式尚未接入新流程")

// registry 按 Key 索引。init 阶段写入、运行期只读，不需要加锁。
var registry = map[string]Provider{}

// Register 注册一个渠道。重复注册同一个 Key 会 panic——
// 这属于编码错误，越早在启动时炸掉越好。
func Register(p Provider) {
	if p == nil {
		panic("channel: 注册了空的 Provider")
	}
	if _, dup := registry[p.Key()]; dup {
		panic("channel: 重复注册渠道 " + p.Key())
	}
	registry[p.Key()] = p
}

// Lookup 按渠道 Key 取 Provider。
func Lookup(key string) (Provider, bool) {
	p, ok := registry[key]
	return p, ok
}

// All 按 Key 排序返回全部渠道，供界面与诊断使用。
func All() []Provider {
	out := make([]Provider, 0, len(registry))
	for _, p := range registry {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key() < out[j].Key() })
	return out
}
