package channel

import "skylogin/internal/neterr"

// IsNetwork 判断这次失败是不是网络原因（连不上、超时、DNS 失败），
// 而不是凭证问题。
//
// 为什么渠道包需要它：2026-11-10 有用户的机器连 baidu / qq / github 全部
// 报 10061（连接被拒），B站登录因此失败。旧代码把这种错误直接说成
// "B站凭证验证失败"，用户拿着好端端的 access_key 去重扫，白折腾。
//
// 判据统一走 internal/neterr，和 mpay 那边是同一套。
func IsNetwork(err error) bool { return neterr.IsNetwork(err) }
