package app

// 凭证判定的"坏结果"必须分成两种，这是本文件存在的唯一理由。
//
// 2026-11-10 的用户日志：机器上 service.mkey.163.com 解析不出来
// （lookup ... no such host），凭证本身完全正常。旧代码把所有失败
// 都归成"token 已失效"，于是：
//
//	解析失败 → 判过期 → 弹刷新横幅 → 触发自动重登
//	        → 重登后还是解析不了 → 再判过期 → 再重登
//
// 全日志 45 次自动重登，单账号 52 秒内重登 5 次。同设备反复重登会把
// 该账号正在跑的客户端顶掉——比"这次签不出票"严重得多。
//
// 所以这里定两个哨兵错误，所有判定点都必须区分：
//
//	errCredExpired    服务端明确说这个凭证不认（1306）。可以标过期、弹横幅、重登。
//	errCredUnverified 这次没问到服务端（DNS / 超时 / 非 JSON）。什么都不许做。

import (
	"errors"
	"fmt"

	"skylogin/internal/mpay"
)

var (
	// errCredExpired 凭证确认失效，需要用户刷新。
	errCredExpired = errors.New("凭证已失效")
	// errCredUnverified 这次没能验证成功，凭证好坏未知。
	errCredUnverified = errors.New("暂时无法确认凭证状态")
)

// credErrFrom 把底层错误翻译成上面两种之一。
//
// 默认落到"暂时无法确认"：宁可让用户下次再试，也不要凭一个看不懂的
// 错误去判凭证死刑——误判的代价是让用户白刷一遍凭证，甚至触发重登顶号。
func credErrFrom(err error) error {
	if err == nil {
		return nil
	}
	if mpay.IsAuth(err) {
		return fmt.Errorf("%w（%v）", errCredExpired, err)
	}
	return fmt.Errorf("%w（%v）", errCredUnverified, err)
}

// credExpired 判断这个错误是否属于"确认失效"。
func credExpired(err error) bool { return errors.Is(err, errCredExpired) }

// credUnverified 判断这个错误是否属于"没问到"。
func credUnverified(err error) bool { return errors.Is(err, errCredUnverified) }
