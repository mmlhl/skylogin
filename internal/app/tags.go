package app

// 账号标签：用户自己打的、可以多个、自由输入。
//
// 只影响界面的分组与筛选，不参与任何登录逻辑。副本与原件各存各的
// （用户明确要求"标签独立"）：副本是独立的一条记录（PlayerID-2 这样的键），
// 名字可以随便改，标签也是它自己的。

import (
	"fmt"
	"strings"
	"time"
)

const (
	// maxTagsPerAccount 一个账号最多存几个标签。
	// 上限不是技术限制，是为了界面：列表里一行最多显示 2 个 + "+N"，
	// 再多就只能进详情看，意义不大。
	maxTagsPerAccount = 8
	// maxTagRunes 单个标签最长多少字。按"字"算，不按字节：
	// 中文标签按字节算会被截断成乱码。
	maxTagRunes = 16
)

// normalizeTags 把界面传来的标签收拾成能存的样子：
// 去首尾空格、丢空串、大小写不同视为同一个（保留第一次出现的写法）、去重、限量。
//
// 为什么大小写要去重：用户手打"VIP"和"vip"是同一个意思，
// 留着两条会让筛选条里出现两个几乎一样的选项。
func normalizeTags(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, raw := range in {
		t := strings.TrimSpace(raw)
		if t == "" {
			continue
		}
		if r := []rune(t); len(r) > maxTagRunes {
			t = string(r[:maxTagRunes])
		}
		key := strings.ToLower(t)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, t)
		if len(out) >= maxTagsPerAccount {
			break
		}
	}
	return out
}

// tagsOf 返回账号的标签视图值。
//
// nil 要变成空数组：JSON 里 null 和 [] 对前端是两回事，
// `tags.map()` 碰到 null 会炸。这是最容易漏的一处。
func tagsOf(a Account) []string {
	if a.Tags == nil {
		return []string{}
	}
	return a.Tags
}

// saveAccountTags 保存某一条账号记录的标签。
//
// 和 saveGameSettings 一样：只改这一行的这一个字段，不走 saveAccount
// （它会顺手 setCurrent，改个标签把用户选中的账号挪走很烦），
// 读-改-写走 accountsMu 串行化。
func saveAccountTags(accountID string, tags []string) error {
	if accountID == "" {
		return fmt.Errorf("缺少账号")
	}
	clean := normalizeTags(tags)

	accountsMu.Lock()
	defer accountsMu.Unlock()
	list, err := loadAccounts()
	if err != nil {
		return fmt.Errorf("读账号失败: %w", err)
	}
	for i := range list {
		if accountKey(list[i]) != accountID {
			continue
		}
		list[i].Tags = clean
		list[i].UpdatedAt = time.Now().Unix()
		if err := saveList(list); err != nil {
			return fmt.Errorf("保存标签失败: %w", err)
		}
		return nil
	}
	return fmt.Errorf("找不到这个账号")
}

// allTagsView 给筛选条用的标签清单：名称 + 命中数，按首次出现顺序。
//
// 顺序固定（不按字典序、不按数量）：用户扫一眼标签条时，位置稳定比"热门在前"重要，
// 否则每次启动标签都在跳，肌肉记忆用不上。
func allTagsView() []map[string]any {
	list, err := loadAccounts()
	if err != nil {
		return []map[string]any{}
	}
	order := make([]string, 0, 8)
	count := make(map[string]int, 8)
	for _, a := range list {
		for _, t := range a.Tags {
			if _, ok := count[t]; !ok {
				order = append(order, t)
			}
			count[t]++
		}
	}
	out := make([]map[string]any, 0, len(order))
	for _, t := range order {
		out = append(out, map[string]any{"name": t, "count": count[t]})
	}
	return out
}
