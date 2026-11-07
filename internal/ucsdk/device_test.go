package ucsdk

// 九游上报串的约束：字段顺序、摘要关系、两个阶段的差异。
//
// 机型库本身的自洽性由 internal/phone 的测试负责，这里只验"翻译成九游格式"
// 这一步。

import (
	"crypto/md5"
	"encoding/hex"
	"strings"
	"testing"

	"skylogin/internal/phone"
)

func TestFingerprintShape(t *testing.T) {
	d := phone.New(phone.Catalog[0])
	csid := "11111111-2222-4333-8444-555555555555"

	boot := fingerprint(d, csid, "")
	if strings.HasPrefix(boot, "werewolf:") {
		t.Fatal("引导阶段不该带 werewolf 前缀")
	}
	keys := []string{
		"orient:L", "sola:", "csid:" + csid, "netType:-2", "bssid:",
		"utdid:" + utdidFor(d), "net_id:-1", "imsi:", "osId:" + d.InstallUUID,
		"resY:", "resX:", "|ssid:", "|mac:", "kaisa:", "ackerman:", "imei:",
		"mikasa:", "model:" + d.Model, "mobi:", "net:wifi", "uya:", "oaId:",
	}
	pos := -1
	for _, k := range keys {
		i := strings.Index(boot, k)
		if i < 0 {
			t.Fatalf("指纹缺少 %q: %s", k, boot)
		}
		if i <= pos {
			t.Fatalf("字段顺序错位：%q", k)
		}
		pos = i
	}

	// 摘要关系必须成立：真机上这几个是算出来的，不是随便写的
	if want := md5Upper(t, absentPlaceholder); !strings.Contains(boot, "sola:"+want) ||
		!strings.Contains(boot, "mikasa:"+want) {
		t.Fatalf("sola/mikasa 不是 MD5(%q)", absentPlaceholder)
	}
	if !strings.Contains(boot, "kaisa:"+md5Upper(t, csid)) {
		t.Fatal("kaisa 不等于 MD5(csid)")
	}
	if !strings.Contains(boot, "ackerman:"+md5Upper(t, d.AndroidID)) {
		t.Fatal("ackerman 不等于 MD5(android_id)")
	}
	if !strings.Contains(boot, "uya:"+md5Upper(t, utdidFor(d))) {
		t.Fatal("uya 不等于 MD5(utdid)")
	}

	// 业务阶段：加前缀，并带上 bssid / ssid / oaId
	biz := fingerprint(d, csid, "cafe1234")
	if !strings.HasPrefix(biz, "werewolf:cafe1234|orient:L|") {
		t.Fatalf("业务阶段前缀不对: %s", biz)
	}
	if !strings.Contains(biz, "bssid:02:00:00:00:00:00") || !strings.Contains(biz, "ssid:<unknown ssid>") {
		t.Fatal("业务阶段缺少 bssid / ssid")
	}
	if !strings.Contains(biz, "oaId:"+d.OAID) {
		t.Fatal("业务阶段没带 oaId")
	}
}

// TestDeviceWithoutOAID 覆盖「这台机器取不到广告标识」的情况。
func TestDeviceWithoutOAID(t *testing.T) {
	d := phone.New(phone.Device{
		Model: "X", Vendor: "Y", OSRelease: "14", APILevel: 34,
		ScreenLong: 2400, ScreenShort: 1080, NoOAID: true,
	})
	if !strings.HasSuffix(fingerprint(d, "c", "w"), "oaId:") {
		t.Fatal("没有 oaId 时应该留空")
	}
}

// TestUTDIDStable 同一台设备的 utdid 必须永远一样，换一台才变。
func TestUTDIDStable(t *testing.T) {
	d := phone.New(phone.Catalog[0])
	if utdidFor(d) != utdidFor(d) {
		t.Fatal("同一台设备的 utdid 不稳定")
	}
	e := phone.New(phone.Catalog[0])
	if utdidFor(d) == utdidFor(e) {
		t.Fatal("两台设备的 utdid 撞了")
	}
	if len(utdidFor(d)) != 24 {
		t.Fatalf("utdid 长度不对: %d", len(utdidFor(d)))
	}
}

func md5Upper(t *testing.T, s string) string {
	t.Helper()
	sum := md5.Sum([]byte(s))
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}
