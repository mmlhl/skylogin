// 九游网页扫码登录的纯 HTTP 复刻探针（不依赖浏览器）。
//
//	go run ./cmd/ucqrtest
//
// 完整链路（实测 2026-11-13）：
//
//	startQRCodeLogin        → 二维码图片 + qrToken
//	queryLoginStatus × n    → status 1 等待 / 2 已扫 / 0 完成（带 vcode）/ 4 过期
//	autologinbycode(vcode)  → 下发 ieu_member_sid 等登录 cookie
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"time"

	"skylogin/internal/ucqrcode"
)

func main() {
	timeout := flag.Int("timeout", 240, "总共等待扫码的秒数")
	dir := flag.String("dir", "E:\\skytest\\shots", "二维码保存目录")
	umid := flag.String("umid", "", "阿里 WebUMID（member_umidToken），留空则用占位值")
	verbose := flag.Bool("v", false, "打印请求 URL 与 body")
	flag.Parse()

	c := ucqrcode.New()
	c.UMIDToken = *umid
	c.Verbose = *verbose

	show := func() bool {
		if err := c.StartQRCode(); err != nil {
			fmt.Println("建码失败:", err)
			return false
		}
		// 文件名带 qrToken 片段：图片查看器会缓存同名文件，同名就看不到新码
		name := fmt.Sprintf("%s\\uc-qr-%s.png", *dir, c.QRToken[len(c.QRToken)-8:])
		if err := os.WriteFile(name, c.QRImage, 0o644); err != nil {
			fmt.Println("写图片失败:", err)
			return false
		}
		fmt.Printf("\n① 二维码 %s（%d 字节）\n", name, len(c.QRImage))
		// 用浏览器打开图片：图片查看器会缓存同名文件，浏览器新标签一定能看到新码
		_ = exec.Command(`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`, name).Start()
		return true
	}

	if !show() {
		os.Exit(1)
	}
	fmt.Println("② 请用手机【九游 App】扫码并确认…")

	deadline := time.Now().Add(time.Duration(*timeout) * time.Second)
	for time.Now().Before(deadline) {
		st, detail, err := c.QueryStatusDetail()
		if err != nil {
			fmt.Println("   轮询出错:", err)
			time.Sleep(2 * time.Second)
			continue
		}
		switch st {
		case 0:
			// 扫码完成：这一次的 data 里带 vcode，用它换登录态
			vcode, _ := detail["vcode"].(string)
			scene, _ := detail["connectScene"].(string)
			if vcode == "" {
				break
			}
			fmt.Printf("   状态 0：已确认，vcode=%s…，换登录态\n", vcode[:min(12, len(vcode))])
			if err := c.AutoLoginByCode(vcode, scene); err != nil {
				fmt.Println("   autologinbycode 失败:", err)
				break
			}
			fmt.Println("   登录成功 ✔")
			dump(c)
			return
		case 1:
			// 等待扫码
		case 2:
			fmt.Println("   状态 2：已扫码，等手机确认")
		case 4:
			fmt.Printf("   状态 4：码失效，换一张\n")
			if !show() {
				os.Exit(1)
			}
		default:
			fmt.Printf("   状态 %d\n", st)
		}
		time.Sleep(2 * time.Second)
	}
	fmt.Println("超时，没扫上")
}

// dump 打印登录成功后拿到的 cookie。
func dump(c *ucqrcode.Client) {
	fmt.Println("\n③ 登录态：")
	for k, v := range c.Cookies() {
		if len(v) > 80 {
			v = v[:80] + "…"
		}
		fmt.Printf("   %-24s = %s\n", k, v)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
