package app

// 本地探针（默认跳过）：对指定账号直接跑一次「按渠道签发」，看卡在哪一步。
//
//	MINT_KEY=<账号键> go test ./internal/app -run TestZZMintByPlatform -v -count=1
//
// 会真实访问服务端（uni_sauth / 建码 / 确认 / 换票），但不写寄宿槽、不改账号库。

import (
	"os"
	"path/filepath"
	"testing"

	"skylogin/internal/device"
)

func TestZZMintByPlatform(t *testing.T) {
	key := os.Getenv("MINT_KEY")
	if key == "" {
		t.Skip("未设置 MINT_KEY")
	}
	list, err := loadAccounts()
	if err != nil {
		t.Fatal(err)
	}
	acc, ok := findAccount(list, key)
	if !ok {
		t.Fatalf("没找到账号 %s", key)
	}
	t.Logf("目标：%s uid=%s region=%q sa=%.0f 可签发=%v",
		displayName(acc), acc.UserID, acc.Region, tokenSaValue(acc.MpayToken), channelMintable(acc))
	// 把 token 的签发身份打出来：uni_sauth 的 gameid 必须与之一致，
	// 不一致就会被拒（501），这是区分“凭证类型不对”与“真的不支持”的关键。
	f := tokenFields(acc.MpayToken)
	longID, shortID := ticketContextForToken(acc.MpayToken)
	t.Logf("token: g_i=%v t=%v sa=%v fgi=%v ulen=%d ctx=%s/%s",
		f["g_i"], f["t"], f["sa"], f["fgi"], len(acc.MpayToken), shortID, longID)

	dev, derr := device.LoadOrCreateClient(filepath.Join(dataDir(), "sdk_device.json"))
	if derr != nil {
		t.Fatalf("设备信息不可用: %v", derr)
	}
	ticket, ch, merr := mintTicketByPlatform(acc, dev)
	if merr != nil {
		t.Fatalf("渠道签发失败: %v", merr)
	}
	t.Logf("渠道签发成功：票 %d 字符，channel=%s", len(ticket), ch)
}
