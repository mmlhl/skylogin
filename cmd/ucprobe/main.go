package main

// 临时探针：九游 sid → uni_sauth → 登录包 → MPay 票据。
// 用本地已登记的 MPay 设备号走建码，只验票、不启动游戏。跑完即删。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"skylogin/internal/channel"
	"skylogin/internal/device"
	"skylogin/internal/mpay"
	"skylogin/internal/netsec"
	"skylogin/internal/profile"
	"skylogin/internal/protocol"
	"skylogin/internal/secretfile"
)

func main() {
	sid := os.Getenv("UC_SID")
	if sid == "" {
		fmt.Println("需要环境变量 UC_SID")
		return
	}
	root := filepath.Join(os.Getenv("APPDATA"), "SkyLogin")

	// 设备号：MPay 只认它自己发过的号，从本地配置里取
	var cfg struct {
		MpayDeviceID string `json:"mpay_device_id"`
	}
	if b, err := secretfile.ReadFile(filepath.Join(root, "config.json")); err == nil {
		_ = json.Unmarshal(b, &cfg)
	}
	if cfg.MpayDeviceID == "" {
		fmt.Println("本地没有 MPay 设备号，跳过取票")
		return
	}
	fmt.Println("MPay 设备号 =", cfg.MpayDeviceID)

	dev, err := device.LoadOrCreateClient(filepath.Join(root, "sdk_device.json"))
	if err != nil {
		fmt.Println("设备指纹加载失败:", err)
		return
	}

	custom := map[string]any{
		"login_channel":      "uc_platform",
		"app_channel":        "uc_platform",
		"source_app_channel": "uc_platform",
		"sdk_version":        "9.7.12.0",
		"get_access_token":   "1",
	}
	uid0 := os.Getenv("UC_UID")
	if uid0 == "" {
		uid0 = "0"
	}
	sauth := netsec.NewSAUTH(uid0, sid, profile.ShortGameID, custom, dev)
	resp, err := netsec.PostSigned(sauth, profile.ShortGameID)
	if err != nil {
		fmt.Println("uni_sauth 失败:", err)
		return
	}
	fmt.Println("uni_sauth code =", fmt.Sprint(resp["code"]))
	if fmt.Sprint(resp["code"]) != "200" {
		b, _ := json.Marshal(resp)
		fmt.Println("响应:", string(b))
		return
	}
	uid := fmt.Sprint(resp["sdkuid"])

	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
	info, _ := channel.InfoOf(channel.KeyUC)
	payload, err := netsec.NewLoginPayloadChan(resp, sid, sid, uid, "", ts, dev, info)
	if err != nil {
		fmt.Println("组装登录包失败:", err)
		return
	}
	fmt.Printf("登录包字段 %d 个\n", len(payload.Body))

	c := mpay.New()
	c.AppChannel = protocol.Get().AppChannel
	ticket, data, err := c.LoginGame(
		cfg.MpayDeviceID, payload.Body,
		protocol.Get().LongGameID, profile.ShortGameID,
		protocol.Get().AppChannel,
		"c4.20.0", "0.0.0.0", "10.0.19045", "win_x64",
	)
	if err != nil {
		fmt.Println("取票失败:", err)
		b, _ := json.Marshal(data)
		if len(b) > 700 {
			b = b[:700]
		}
		fmt.Println("返回:", string(b))
		return
	}
	fmt.Printf("\n>>> 拿到票据: %s（长度 %d）\n", ticket, len(ticket))
}
