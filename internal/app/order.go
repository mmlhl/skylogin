package app

// 账号列表的顺序。
//
// 顺序不存字段：**文件里的行序就是界面顺序**（loadAccounts 保序、
// accountsView 原序输出）。这样零迁移，saveAccount 的"原地替换/末尾追加"
// 语义天然保留，新账号出现在末尾也符合直觉。
//
// 所以"排序"这件事在后端只有一种能力：按给定顺序重排文件行序。
// 按名称、按最近登录这些规则由前端算好顺序再传上来——规则只有一份，
// 不会出现前后端两套排序逻辑打架。

import "fmt"

// reorderAccounts 按 ids 给出的顺序重排账号文件的行序。
//
// 三条规则（都有测试）：
//   - ids 里没提到的行保持原相对顺序追加在末尾：前端筛选后只传当前可见的
//     顺序也不至于把别的账号弄丢；
//   - 未知 id 忽略，重复 id 只认第一次；
//   - 行数必须一一对应，宁可放弃这次重排也不丢账号。
//
// 不碰凭证、不碰 current.txt、不碰 updated_at：改顺序不是"修改账号"。
func reorderAccounts(ids []string) error {
	if len(ids) == 0 {
		return fmt.Errorf("缺少顺序")
	}

	accountsMu.Lock()
	defer accountsMu.Unlock()
	list, err := loadAccounts()
	if err != nil {
		return fmt.Errorf("读账号失败: %w", err)
	}

	byKey := make(map[string]Account, len(list))
	for _, a := range list {
		byKey[accountKey(a)] = a
	}

	seen := make(map[string]bool, len(list))
	out := make([]Account, 0, len(list))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		a, ok := byKey[id]
		if !ok {
			continue
		}
		seen[id] = true
		out = append(out, a)
	}
	for _, a := range list {
		if !seen[accountKey(a)] {
			out = append(out, a)
		}
	}

	// 走到这里 out 一定等于 list 的条数（上面两条分支覆盖了所有行），
	// 这条检查是防以后有人改坏逻辑时静默丢账号。
	if len(out) != len(list) {
		return fmt.Errorf("顺序不完整（%d/%d），已放弃这次重排", len(out), len(list))
	}
	if err := saveList(out); err != nil {
		return fmt.Errorf("保存顺序失败: %w", err)
	}
	return nil
}
