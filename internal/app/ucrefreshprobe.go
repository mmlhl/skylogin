package app

// 九游续期探针（开发用）：验证 refreshToken 能不能换到新 sid。
//
//	go run ./cmd/ucrefresh
//
// 刻意不读密码、不读环境变量：只用账号库里已经登录过的那条九游账号，
// 这样测出来的结果才能代表"以后没人值守时能不能自己续命"。
// 输出直接打到 stdout（这个探针只在开发时用控制台跑）。

import (
	"fmt"
	"os"
	"time"

	"skylogin/internal/channel"
	"skylogin/internal/ucsdk"
)

// ProbeUCRefresh 用账号里存的 refreshToken 把 sid 续期，并逐关验证。
func ProbeUCRefresh() {
	list, err := loadAccounts()
	if err != nil {
		fmt.Println("读账号失败:", err)
		return
	}

	var picked *Account
	for i := range list {
		a := &list[i]
		if a.Channel != channel.KeyUC {
			continue
		}
		if a.RefreshToken == "" || a.GameAuthSign == "" {
			fmt.Printf("跳过 %s：缺 refreshToken 或 sid\n", displayName(*a))
			continue
		}
		picked = a
		break
	}
	if picked == nil {
		fmt.Println("账号库里没有可续期的九游账号")
		return
	}

	fmt.Printf("用账号: %s（playerID=%s）\n", displayName(*picked), picked.PlayerID)
	fmt.Printf("  refreshToken %d 字符，当前 sid %d 字符\n",
		len(picked.RefreshToken), len(picked.GameAuthSign))

	// 先看旧 sid 本身还活着没。
	// 这一步是判断"续期是不是换票式"的关键：如果续一次旧票就作废，
	// 那么没落盘就等于把账号弄废了，必须知道。
	fmt.Println()
	if err := VerifyChannelCredential(channel.KeyUC, picked.UserID, picked.GameAuthSign); err != nil {
		fmt.Println("旧 sid 当前已失效:", err)
	} else {
		fmt.Println("旧 sid 当前仍然有效")
	}

	fmt.Println()
	fmt.Println("调 ucid.user.refreshLogin（sdk.9game.cn，参数 refreshToken + sid）…")
	c := ucsdk.New(ucsdk.SkyGame())
	s, err := c.RefreshLogin(picked.RefreshToken, picked.GameAuthSign)
	if err != nil {
		fmt.Println("续期失败:", err)
		if code := ucsdk.Code(err); code != 0 {
			fmt.Printf("  业务码 = %d\n", code)
		}
		probeWithGivenSID(c, picked.RefreshToken, picked)
		return
	}
	fmt.Println("续期成功")
	fmt.Printf("  新 sid %s\n", s.SID)
	fmt.Printf("  与旧 sid 相同？%v；服务端给的有效期 %d 秒（%.1f 小时）\n",
		s.SID == picked.GameAuthSign, s.Timeout, float64(s.Timeout)/3600)
	if s.RefreshToken != "" {
		fmt.Printf("  新 refreshToken %d 字符，与旧的相同？%v\n",
			len(s.RefreshToken), s.RefreshToken == picked.RefreshToken)
	} else {
		fmt.Println("  响应没带新 refreshToken（沿用旧值）")
	}

	// 新 sid 要过两关才算真的能用：业务接口 + 光遇登录态
	if _, err := c.Call(ucsdk.HostAccount, ucsdk.ServiceUserInfo, map[string]any{"sid": s.SID}); err != nil {
		fmt.Println("  getUserSimpleInfo 失败:", err)
	} else {
		fmt.Println("  getUserSimpleInfo 通过")
	}
	if err := VerifyChannelCredential(channel.KeyUC, picked.UserID, s.SID); err != nil {
		fmt.Println("  uni_sauth 失败:", err)
		return
	}
	fmt.Println("  uni_sauth 通过（新 sid 可直接启动游戏）")

	picked.GameAuthSign = s.SID
	if s.RefreshToken != "" {
		picked.RefreshToken = s.RefreshToken
	}
	picked.CredState = "ok"
	picked.LastCheck = time.Now().Unix()
	picked.UpdatedAt = time.Now().Unix()
	if err := saveAccount(*picked); err != nil {
		fmt.Println("  写回账号库失败:", err)
	} else {
		fmt.Println("  新 sid 已写回账号库")
	}

	probeRefreshAfterExpiry(c, picked.RefreshToken)
}

// probeWithGivenSID 排查"续期是不是一次性的"，同时也是把账号救回来的手段。
//
// 如果账号里存的 sid 续不动了，就手工给一个别的 sid 再试：
// 用环境变量 UC_SID 传进来（只对当前进程生效，不落盘）。
// 续成功说明作废的是那对组合，不是 refreshToken——此时立刻把新票存下来，
// 因为九游是"以旧换新"，换完旧票当场失效，不存就等于又丢一张。
func probeWithGivenSID(c *ucsdk.Client, refreshToken string, acc *Account) {
	alt := os.Getenv("UC_SID")
	if alt == "" {
		fmt.Println()
		fmt.Println("（想再试别的 sid，设环境变量 UC_SID 后重跑）")
		return
	}
	fmt.Println()
	fmt.Printf("用指定 sid 再试一次: %s\n", alt)
	s, err := c.RefreshLogin(refreshToken, alt)
	if err != nil {
		fmt.Printf("  仍然失败: %v（code=%d）\n", err, ucsdk.Code(err))
		fmt.Println("  → 作废的可能是 refreshToken 本身，需要重新登录")
		return
	}
	fmt.Printf("  成功: 新 sid %s，有效期 %d 秒\n", s.SID, s.Timeout)

	// 立刻验证并落盘——这一步不能省，旧票已经废了
	if err := VerifyChannelCredential(channel.KeyUC, acc.UserID, s.SID); err != nil {
		fmt.Println("  但 uni_sauth 没过:", err)
		return
	}
	acc.GameAuthSign = s.SID
	if s.RefreshToken != "" {
		acc.RefreshToken = s.RefreshToken
	}
	acc.CredState = "ok"
	acc.LastCheck = time.Now().Unix()
	acc.UpdatedAt = time.Now().Unix()
	if err := saveAccount(*acc); err != nil {
		fmt.Println("  写回账号库失败:", err)
	} else {
		fmt.Println("  新票已写回账号库，账号恢复正常")
	}
}

// probeRefreshAfterExpiry 回答一个决定"要不要让用户重登"的问题：
// sid 已经过期（或无效）时，光靠 refreshToken 还能不能把会话救回来。
//
// 能救 → 用户永久不用再登录；不能救 → 只能在 sid 还有效的窗口内滚动续期，
// 隔几天没开就得重新输密码。所以必须用假 sid 单独测一次。
func probeRefreshAfterExpiry(c *ucsdk.Client, refreshToken string) {
	fmt.Println()
	fmt.Println("关键对照：sid 无效时还能不能只靠 refreshToken 复活")
	time.Sleep(2 * time.Second)

	s, err := c.RefreshLogin(refreshToken, "ngstmobideadbeefdeadbeefdeadbeefdeadbeef000000")
	if err != nil {
		fmt.Printf("  假 sid 续期失败: %v（code=%d）\n", err, ucsdk.Code(err))
		fmt.Println("  → 结论：sid 必须有效。过期后只能重新登录，续期只能滚动延长。")
		return
	}
	fmt.Printf("  假 sid 居然续期成功: 新 sid %s\n", s.SID)
	fmt.Println("  → 结论：refreshToken 可以单独换会话，sid 过期也能救回来。")
}
