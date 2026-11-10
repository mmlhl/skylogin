package app

// 凭证定时查看：后台静默地隔一段时间看一眼每个网易账号的凭证还在不在。
//
// 它做什么
//
//	每隔几小时，对每个网易账号走一次"现签票据"的流程（签出来就丢掉），
//	把结果写进账号的凭证状态：
//	  - 成功 → 标记 ok，更新 LastCheck
//	  - 失败 → 标记 expired，记下原因，界面上就会出现刷新入口
//
// 它不做什么
//
//	不替换 token、不改上下文、不动账号文件里除状态以外的东西。
//	也**不假设**这个动作能让凭证活得更久——只是"看一眼"。
//	如果哪天观察到定期访问确实影响了寿命，再另外记录，不要在代码或文案里下结论。
//
// 为什么值得跑
//
//	用户反馈"扫码的凭证老是过期"，而我们自己几乎不掉。差别可能在"用不用"：
//	本机十几个号天天在签票，用户可能一周才开一次。定时看一眼，至少能：
//	  1. 在凭证真的失效时立刻把状态标出来，而不是等用户点启动才发现
//	  2. 攒出一份"什么时候失效"的记录，回答"到底能活多久"

import (
	"errors"
	"path/filepath"
	"time"

	"skylogin/internal/device"
)

const (
	// credCheckInterval 两次查看之间的间隔。
	//
	// 取 4 小时：一天 6 次，对服务端几乎没有压力；又能比较早地发现失效。
	credCheckInterval = 4 * time.Hour
	// credCheckFirstDelay 程序启动后先等一会儿再跑第一轮，
	// 别和界面初始化、账号续期这些抢着发请求。
	credCheckFirstDelay = 90 * time.Second
	// credCheckGap 单个账号之间的间隔，避免一口气打出去。
	credCheckGap = 3 * time.Second
)

// startCredCheck 在后台起一个定时查看的循环（静默，不弹任何东西）。
func startCredCheck() {
	go func() {
		time.Sleep(credCheckFirstDelay)
		for {
			checkAllCredentials()
			time.Sleep(credCheckInterval)
		}
	}()
}

// checkAllCredentials 把所有网易账号看一遍。
func checkAllCredentials() {
	list, err := loadAccounts()
	if err != nil || len(list) == 0 {
		return
	}
	dev, derr := device.LoadOrCreateClient(filepath.Join(dataDir(), "sdk_device.json"))
	if derr != nil {
		logf("凭证查看：设备标识不可用，跳过本轮")
		return
	}

	checked, bad, relogin := 0, 0, 0
	for _, a := range list {
		// 只看网易账号：渠道服账号有自己的保活链路（Cookie / accessToken 重签）
		if a.Channel != "netease" || a.MpayToken == "" {
			continue
		}
		checked++
		if _, _, terr := mintTicket(a, dev); terr != nil {
			// 没问到服务端（DNS / 超时 / 非 JSON）：凭证好坏未知。
			// 这里必须原地返回，不能标 expired、不能重登——否则一次断网
			// 就会把全部账号刷成"不可用"，还会引发连环重登（见 relogin.go）。
			if credUnverified(terr) {
				logf("凭证查看：%s 暂时无法确认（%v），保持原状态", displayName(a), terr)
				time.Sleep(credCheckGap)
				continue
			}
			// 凭证坏了，先试着自己救：有保存的账号密码就无头重登一次。
			// 一轮最多救 maxReloginPerRound 个，避免密码本身错了还反复打接口。
			if relogin < maxReloginPerRound && hasSavedPassword(a) {
				relogin++
				if fresh, rerr := autoReloginAndVerify(a, dev); rerr == nil {
					setAccountCredState(a.PlayerID, "ok", "")
					logf("凭证查看：%s 已自动重登恢复", displayName(fresh))
					time.Sleep(credCheckGap)
					continue
				} else {
					logf("凭证查看：%s 自动重登没成功（%v）", displayName(a), rerr)
				}
			}
			bad++
			setAccountCredState(a.PlayerID, "expired", terr.Error())
			logf("凭证查看：%s 不可用（%v）", displayName(a), terr)
		} else {
			setAccountCredState(a.PlayerID, "ok", "")
			logf("凭证查看：%s 可用", displayName(a))
		}
		time.Sleep(credCheckGap)
	}
	if checked > 0 {
		logf("凭证查看完成：%d 个网易账号，%d 个不可用（本轮自动重登 %d 次）", checked, bad, relogin)
	}
}

// maxReloginPerRound 是一轮凭证查看里最多自动重登几个账号。
const maxReloginPerRound = 2

// autoReloginAndVerify 自动重登，并用新凭证真的签一次票确认可用。
// 光看"登录成功"不够：服务端可能给出一个换不了票的凭证。
func autoReloginAndVerify(acc Account, dev *device.ClientProfile) (Account, error) {
	// 走同一套冷却：mintTicket 里已经重登过的号，这里不该再来一次。
	// 两处都重登的话，同一个号一轮里会登录两遍，白打接口还更容易触发风控。
	if !allowRelogin(accountKey(acc)) {
		return acc, errors.New("刚自动重登过，本轮不再重试")
	}
	if _, err := autoRelogin(acc); err != nil {
		return acc, err
	}
	list, err := loadAccounts()
	if err != nil {
		return acc, err
	}
	fresh, ok := findAccount(list, acc.PlayerID)
	if !ok {
		return acc, errors.New("重登后账号不见了")
	}
	if _, _, err := mintTicket(fresh, dev); err != nil {
		return fresh, err
	}
	return fresh, nil
}
