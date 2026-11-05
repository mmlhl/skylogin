package app

// 应用宝（YSDK）微信扫码探针：验证"自己画码 → 手机微信扫 → 换 openid"这条链。
//
//	skylogin.exe --probe-myapp
//
// 流程和正式入口完全一样，只是把二维码写成 PNG 打开、结果写日志：
//  1. 申请二维码（ysdk → 微信）
//  2. 轮询扫码状态
//  3. 拿到 wx_code 后调 wx_verify_code 换应用宝登录态
//
// 日志：%APPDATA%\SkyLogin\myapp_probe.log

import (
	"os"
	"path/filepath"
	"time"

	"skylogin/internal/device"
	"skylogin/internal/myapp"
)

// ProbeMyApp 跑一遍应用宝微信扫码。
func ProbeMyApp() {
	logPath := filepath.Join(dataDir(), "myapp_probe.log")
	log := func(format string, args ...any) { qrProbeLog(logPath, format, args...) }
	log("=== 应用宝微信扫码探针 ===")

	s, err := myapp.CreateSession()
	if err != nil {
		log("申请二维码失败: %v", err)
		return
	}
	log("二维码已创建: uuid=%s（%d 字节 PNG）", maskMiddle(s.UUID), len(s.QRCodePNG))

	dir := filepath.Join(dataDir(), "qr")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log("建目录失败: %v", err)
		return
	}
	img := filepath.Join(dir, "myapp-current.png")
	if err := os.WriteFile(img, s.QRCodePNG, 0o600); err != nil {
		log("写二维码失败: %v", err)
		return
	}
	log("二维码图片: %s", img)
	openImage(img)

	deadline := time.Now().Add(6 * time.Minute)
	last := myapp.Waiting
	lastRaw := ""
	for time.Now().Before(deadline) {
		time.Sleep(time.Second)
		st, code, raw, perr := s.PollRaw()
		if perr != nil {
			log("轮询出错（继续等）: %v  原始: %s", perr, cut(raw, 200))
			continue
		}
		if st != last || raw != lastRaw {
			last, lastRaw = st, raw
			log("状态: %d（0=未扫 1=已扫/确认 2=取消 3=过期） 原始: %s", st, cut(raw, 240))
		}
		if st == myapp.Canceled {
			log("手机上取消了")
			return
		}
		if st == myapp.Expired {
			log("二维码过期")
			return
		}
		if code == "" {
			continue
		}
		log("拿到 wx_code: %s", maskCode(code))

		// channel 先按抓到的 "00000000"，不行再试 "myapp"
		for _, ch := range []string{"10095285", "00000000", "myapp"} {
			out, verr := s.Verify(code, ch)
			if verr != nil {
				log("校验（channel=%s）失败: %v  响应: %s", ch, verr, cut(redactJSON(out), 300))
				continue
			}
			log("校验通过（channel=%s）！响应: %s", ch, cut(redactJSON(out), 900))
			cred, cerr := credFromVerify(out)
			if cerr != nil {
				log("凭证不完整: %v", cerr)
				return
			}
			dev, derr := loadClientDevice()
			if derr != nil {
				log("设备标识不可用: %v", derr)
				return
			}
			acc, ok := TryMyAppVariants(cred, dev, log)
			if !ok {
				log("所有拼法都被 uni_sauth 拒了")
				return
			}
			if err := saveScannedRow(acc, cred.OpenID, "myapp"); err != nil {
				log("保存账号失败: %v", err)
				return
			}
			log("账号已保存: %s（可以回界面点「启动光遇」验证）", acc.Name)
			return
		}
		return
	}
	log("超时：3 分钟内没有完成扫码")
}

// loadClientDevice 读 PC 客户端设备画像（和 B站/vivo 网页登录用的是同一份）。
func loadClientDevice() (*device.ClientProfile, error) {
	return device.LoadOrCreateClient(filepath.Join(dataDir(), "sdk_device.json"))
}
