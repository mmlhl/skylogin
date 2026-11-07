package app

// 诊断：加载湾到底"记不记得"某个账号。
//
// 背景：扫码换票时带上平台会话（sid），服务端会把扫到的账号写进该 sid 对应的
// "记住槽"；之后 ticket?code=&account_id= 就能免扫出票。渠道服扫码走的是
// uni_sauth（整包直接登录），不经过加载湾，所以槽里应该没有它。
//
// 这个探针就是为了把"应该"变成实测：拿现有的网易账号换 sid，再用渠道账号的
// uid 去问一次，看服务端怎么答。只读不发，不写任何账号。

import (
	"fmt"
	"time"

	"skylogin/internal/launcher"
)

// ProbeAccounts 打印本地账号清单（诊断用）。
//
// 各条链路的 id 命名不一样（网易号是 uid、渠道服是 player_id、扫码行还有行键），
// 排查"为什么找不到某个账号"时先看这个。
func ProbeAccounts() error {
	list, err := loadAccounts()
	if err != nil {
		return fmt.Errorf("读账号失败: %w", err)
	}
	if len(list) == 0 {
		fmt.Println("（没有账号）")
		return nil
	}
	for i := range list {
		a := list[i]
		fmt.Printf("%-30s key=%-26s channel=%-14s uid=%-28s pid=%-28s row=%s\n",
			a.Name, accountKey(a), a.Channel, a.UserID, a.PlayerID, a.RowKey)
		// 凭证是什么时候拿的、走的哪条链路：排查"扫码到底有没有覆盖"靠这个
		upd := "-"
		if a.UpdatedAt > 0 {
			upd = time.Unix(a.UpdatedAt, 0).Format("01-02 15:04:05")
		}
		last := "-"
		if a.LastTicket > 0 {
			last = time.Unix(a.LastTicket, 0).Format("01-02 15:04:05")
		}
		fmt.Printf("    更新=%s 发票=%s source=%s method=%s token=%d字符\n",
			upd, last, a.Source, a.LoginMethod, len(a.MpayToken))
	}
	return nil
}

// ProbeRecall 依次用每个能换 sid 的网易账号，问加载湾认不认识 accountID。
//
// 返回错误时也把每次尝试的结论写进日志——重点不是成不成功，而是服务端
// 对不同 account_id 的答复（6008 / 6006 / 其他）差在哪。
func ProbeRecall(accountID string) error {
	list, err := loadAccounts()
	if err != nil {
		return fmt.Errorf("读账号失败: %w", err)
	}
	if accountID == "" {
		return fmt.Errorf("缺少 account_id")
	}

	tried := 0
	for i := range list {
		a := list[i]
		if a.Channel != "netease" || a.MpayToken == "" || a.UserID == "" {
			continue
		}
		sid, lerr := launcher.Login(a.UserID, a.MpayToken, a.DeviceID)
		if lerr != nil || sid == "" {
			logf("[探针] %s 换不到 sid：%v", displayName(a), lerr)
			continue
		}
		tried++
		// 先看这个槽里现在记的是谁，作为对照
		if saved, serr := launcher.SavedAccount(sid); serr == nil {
			logf("[探针] %s 的槽里记的是 %q", displayName(a), saved)
		} else {
			logf("[探针] %s 的槽读不出来：%v", displayName(a), serr)
		}

		r, terr := launcher.TicketForAccount(accountID, a.DeviceID, sid)
		if terr == nil {
			logf("[探针] 命中：%s 的槽认得 %s（票 %d 字符）", displayName(a), accountID, len(r.Ticket))
			fmt.Printf("命中：%s 的槽认得 %s（票 %d 字符）\n", displayName(a), accountID, len(r.Ticket))
			return nil
		}
		logf("[探针] %s 的槽不认 %s：%v", displayName(a), accountID, terr)
		fmt.Printf("%s：%v\n", displayName(a), terr)
	}

	if tried == 0 {
		return fmt.Errorf("没有能换 sid 的账号（需要账号级 token：密码/短信/官方窗口登录过的）")
	}
	return fmt.Errorf("%d 个槽都不认 %s", tried, accountID)
}
