package app

// 标签与行序两条链路的回归测试。
//
// 背景（2026-11-15）：界面要加"按标签分组/筛选"和"手动排列顺序"。
// 这两件事都会去改用户的账号库文件，所以边界必须钉死：
//   - 改标签只动自己那一行的 tag 字段，不碰凭证、不碰当前账号、不碰别人；
//   - 改顺序绝不丢账号、绝不动内容。

import (
	"encoding/json"
	"os"
	"testing"
)

// setupStore 起一个临时数据目录，写入若干账号，返回恢复函数。
func setupStore(t *testing.T, list []Account) {
	t.Helper()
	root := t.TempDir()
	dataDirFn = func() string { return root }
	t.Cleanup(func() { dataDirFn = defaultDataDir })

	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(accountsPath(), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestNormalizeTags(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"去空格去空串", []string{" 大号 ", "", "   "}, []string{"大号"}},
		{"大小写视为同一个并保留首次写法", []string{"VIP", "vip", "Vip"}, []string{"VIP"}},
		{"重复只留一个", []string{"朋友", "朋友"}, []string{"朋友"}},
		{"超长截断到 16 字", []string{"一二三四五六七八九十一二三四五六七八"}, []string{"一二三四五六七八九十一二三四五六"}},
		{"超过 8 个只留前 8 个", []string{"1", "2", "3", "4", "5", "6", "7", "8", "9"}, []string{"1", "2", "3", "4", "5", "6", "7", "8"}},
	}
	for _, c := range cases {
		got := normalizeTags(c.in)
		if len(got) != len(c.want) {
			t.Fatalf("%s：期望 %v，实际 %v", c.name, c.want, got)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("%s：第 %d 项期望 %q，实际 %q", c.name, i, c.want[i], got[i])
			}
		}
	}
}

// 标签是"每行独立"的：改了副本的标签，原件和别的行都不能被动。
// 用户 2026-11-15 明确要求"标签独立"。
func TestSaveAccountTagsOnlyTouchesOneRow(t *testing.T) {
	setupStore(t, []Account{
		{Channel: "netease", PlayerID: "900", Name: "星野", Tags: []string{"大号", "朋友"}},
		{Channel: "netease", PlayerID: "900", CopyKey: "900-2", Name: "星野 · 第二区", Tags: []string{"大号"}},
		{Channel: "huawei", PlayerID: "901", Name: "沉船"},
	})

	if err := saveAccountTags("900-2", []string{" 小号 ", "小号", "第三区"}); err != nil {
		t.Fatalf("保存副本标签失败: %v", err)
	}

	list, err := loadAccounts()
	if err != nil {
		t.Fatal(err)
	}
	if got := list[1].Tags; len(got) != 2 || got[0] != "小号" || got[1] != "第三区" {
		t.Fatalf("副本标签应被规范化成 [小号 第三区]，实际 %v", got)
	}
	if got := list[0].Tags; len(got) != 2 || got[0] != "大号" {
		t.Fatalf("原件的标签不该被动，实际 %v", got)
	}
	if list[2].Tags != nil {
		t.Fatalf("没打标签的行不该被写上标签，实际 %v", list[2].Tags)
	}

	// 不认识的账号要报错，而不是静默成功
	if err := saveAccountTags("不存在", []string{"x"}); err == nil {
		t.Fatal("未知账号应返回错误")
	}
}

// tagsOf 对 nil 必须给空数组：JSON 里 null 会让前端 .map() 直接崩。
func TestTagsOfNilIsEmptySlice(t *testing.T) {
	got := tagsOf(Account{})
	if got == nil || len(got) != 0 {
		t.Fatalf("nil 标签应转成空数组，实际 %#v", got)
	}
	b, _ := json.Marshal(got)
	if string(b) != "[]" {
		t.Fatalf("序列化应是 []，实际 %s", b)
	}
}

func TestAllTagsViewOrderAndCount(t *testing.T) {
	setupStore(t, []Account{
		{PlayerID: "1", Tags: []string{"大号", "朋友"}},
		{PlayerID: "2", Tags: []string{"朋友"}},
		{PlayerID: "3"},
	})
	got := allTagsView()
	if len(got) != 2 {
		t.Fatalf("应聚合出 2 个标签，实际 %d 个：%v", len(got), got)
	}
	if got[0]["name"] != "大号" || got[0]["count"] != 1 {
		t.Fatalf("第一个应是 大号×1（按首次出现顺序），实际 %v", got[0])
	}
	if got[1]["name"] != "朋友" || got[1]["count"] != 2 {
		t.Fatalf("第二个应是 朋友×2，实际 %v", got[1])
	}
}

// 重排的三条规则：按给定顺序、未提到的追加在末尾、未知 id 忽略。
func TestReorderAccounts(t *testing.T) {
	setupStore(t, []Account{
		{PlayerID: "a", Name: "A"},
		{PlayerID: "b", Name: "B"},
		{PlayerID: "c", Name: "C"},
		{PlayerID: "d", Name: "D"},
	})

	// 只提到 c 和 a：b、d 应按原相对顺序补在后面
	if err := reorderAccounts([]string{"c", "a", "不存在的键"}); err != nil {
		t.Fatal(err)
	}
	list, _ := loadAccounts()
	want := []string{"C", "A", "B", "D"}
	if len(list) != len(want) {
		t.Fatalf("重排不能丢账号：期望 %d 行，实际 %d 行", len(want), len(list))
	}
	for i, n := range want {
		if list[i].Name != n {
			t.Fatalf("第 %d 行期望 %s，实际 %s（完整顺序：%v）", i, n, list[i].Name, names(list))
		}
	}

	// 重复 id 只认第一次，行数不变
	if err := reorderAccounts([]string{"d", "d", "b"}); err != nil {
		t.Fatal(err)
	}
	list, _ = loadAccounts()
	if len(list) != 4 {
		t.Fatalf("重复 id 不该改变行数，实际 %d 行", len(list))
	}

	// 空列表直接拒绝（避免误清空）
	if err := reorderAccounts(nil); err == nil {
		t.Fatal("空顺序应被拒绝")
	}
}

func names(list []Account) []string {
	out := make([]string, len(list))
	for i := range list {
		out[i] = list[i].Name
	}
	return out
}

// 重排不能碰凭证和当前账号：它只是"换个位置"。
func TestReorderKeepsCredAndCurrent(t *testing.T) {
	setupStore(t, []Account{
		{PlayerID: "a", Name: "A", MpayToken: "1-eyJ", CredState: "ok"},
		{PlayerID: "b", Name: "B", MpayToken: "1-eyK", CredState: "expired"},
	})
	if err := os.WriteFile(currentPath(), []byte("b"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := reorderAccounts([]string{"b", "a"}); err != nil {
		t.Fatal(err)
	}
	list, _ := loadAccounts()
	if list[0].Name != "B" || list[0].MpayToken != "1-eyK" || list[0].CredState != "expired" {
		t.Fatalf("重排把凭证弄丢了：%+v", list[0])
	}
	b, err := os.ReadFile(currentPath())
	if err != nil || string(b) != "b" {
		t.Fatalf("重排不该改当前账号，实际 %q（err=%v）", string(b), err)
	}
}
