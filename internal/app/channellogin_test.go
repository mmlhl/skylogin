package app

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"skylogin/internal/channel"
)

// 4399 设备身份必须稳定：首次生成落盘，之后每次读到同一份。
// 凭证跟设备绑定，换设备身份 = 凭证作废。
func TestLoadM4399DeviceStable(t *testing.T) {
	root := t.TempDir()
	dataDirFn = func() string { return root }
	defer func() { dataDirFn = defaultDataDir }()

	d1 := loadM4399Device()
	if d1.Udid == "" || d1.DeviceIdentifier == "" {
		t.Fatalf("首次应生成设备身份: %+v", d1)
	}
	if _, err := os.Stat(filepath.Join(root, "s4399_device.json")); err != nil {
		t.Fatalf("设备身份应落盘: %v", err)
	}
	if d2 := loadM4399Device(); d2 != d1 {
		t.Fatalf("设备身份必须稳定: %+v vs %+v", d1, d2)
	}

	// 文件损坏时应重新生成而不是崩掉
	_ = os.WriteFile(filepath.Join(root, "s4399_device.json"), []byte("not json"), 0o600)
	if d3 := loadM4399Device(); d3.Udid == "" {
		t.Fatal("损坏文件后应重新生成设备身份")
	}
}

// 会话目录策略：
//   - B站/4399 登录态不区分账号，共享目录；
//   - vivo 的 Cookie 会话按账号归档（静默续期靠它），
//     但"添加账号"必须用一次性隔离目录——不能复用旧会话，
//     否则会"没输入就自动登录成上一个人"。
func TestChannelProfileDirs(t *testing.T) {
	root := t.TempDir()
	dataDirFn = func() string { return root }
	defer func() { dataDirFn = defaultDataDir }()

	for _, key := range []string{channel.KeyBili, channel.KeyM4399} {
		spec := channelSpecs[key]
		a := spec.profileDir("100001")
		if a == "" || a != spec.profileDir("100002") {
			t.Fatalf("%s 应使用稳定共享目录，实际 %q", key, a)
		}
		if !strings.Contains(a, key) {
			t.Fatalf("%s 的会话目录应按渠道分开：%q", key, a)
		}
	}

	vivo := channelSpecs[channel.KeyVivo]
	add := vivo.profileDir("")
	refresh := vivo.profileDir("100001")
	if add == refresh {
		t.Fatalf("添加账号必须用隔离目录，不能和刷新共用 %q", add)
	}
	if !strings.Contains(refresh, "100001") {
		t.Fatalf("刷新目录应按账号归档：%q", refresh)
	}
	if refresh != vivo.profileDir("100001") {
		t.Fatal("同一账号的会话目录必须稳定，否则丢登录态")
	}
}

// adoptVivoProfile：添加用的临时目录成功后归档到账号名下（重试期间
// WebView2 可能还占着文件），归档后刷新才能静默续期。
func TestAdoptVivoProfile(t *testing.T) {
	root := t.TempDir()
	dataDirFn = func() string { return root }
	defer func() { dataDirFn = defaultDataDir }()

	addDir := filepath.Join(webLoginRoot(), "vivo", "add")
	if err := os.MkdirAll(addDir, 0o755); err != nil {
		t.Fatal(err)
	}
	adoptVivoProfile(addDir, "100001")
	dst := filepath.Join(webLoginRoot(), "vivo", "100001")
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("会话目录应归档到账号名下: %v", err)
	}
	if _, err := os.Stat(addDir); !os.IsNotExist(err) {
		t.Fatal("临时目录应已改名移走")
	}
	// 同目录重复归档应直接返回，不报错
	adoptVivoProfile(dst, "100001")
}

// 渠道显示：B站/4399/vivo 账号必须显示各自的渠道名。
func TestAccountViewChannelLabels(t *testing.T) {
	cases := []struct{ ch, want, wantLabel string }{
		{channel.KeyBili, "bilibili", "B站渠道"},
		{channel.KeyM4399, "m4399", "4399渠道"},
		{channel.KeyVivo, "vivo", "vivo渠道"},
		{"netease", "netease", "网易官方"},
		{"", "huawei", "华为渠道"},
	}
	for _, c := range cases {
		v := accountView(Account{Channel: c.ch, PlayerID: "123", NickName: "测试"})
		if v["channel"] != c.want || v["channelLabel"] != c.wantLabel {
			t.Fatalf("渠道 %q 显示错误: %v / %v", c.ch, v["channel"], v["channelLabel"])
		}
	}
}

// findAccountByUserID 必须能按渠道侧 uid 找到账号（重登时保留别名靠它）。
func TestFindAccountByUserIDChannels(t *testing.T) {
	root := t.TempDir()
	dataDirFn = func() string { return root }
	defer func() { dataDirFn = defaultDataDir }()

	list := []Account{{Channel: channel.KeyVivo, UserID: "8888", PlayerID: "8888", Name: "老别名"}}
	raw, _ := json.Marshal(list)
	if err := os.WriteFile(accountsPath(), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	acc, ok := findAccountByUserID("8888")
	if !ok || acc.Name != "老别名" {
		t.Fatalf("应能找到渠道账号: ok=%v acc=%+v", ok, acc)
	}
}

// 渠道注册表：三个渠道的 finalize 都登记了，不认识的渠道不会误启动。
func TestChannelRegistryComplete(t *testing.T) {
	for _, key := range []string{channel.KeyBili, channel.KeyM4399, channel.KeyVivo} {
		spec, ok := channelSpecs[key]
		if !ok {
			t.Fatalf("渠道 %s 未注册", key)
		}
		if spec.finalize == nil || spec.loginURL == nil || spec.profileDir == nil || spec.resultFile == "" {
			t.Fatalf("渠道 %s 的注册项不完整: %+v", key, spec)
		}
	}
	if _, ok := channelSpecs["nonexistent"]; ok {
		t.Fatal("不该有不存在的渠道")
	}
}

// 回归：watchChannelLogin 必须把验证过的账号落库。重构时丢过这一步，
// 症状是"界面显示登录成功，账号列表和账号文件里却什么都没有"。
func TestWatchChannelLoginSavesAccount(t *testing.T) {
	root := t.TempDir()
	dataDirFn = func() string { return root }
	defer func() { dataDirFn = defaultDataDir }()

	// 桩掉 finalize（真实实现要走在线验证），其余流程保持原样
	oldSpec := channelSpecs[channel.KeyVivo]
	stub := *oldSpec
	stub.finalize = func(res CapturedLoginResult) (Account, error) {
		return Account{
			Channel: channel.KeyVivo, UserID: res.UID, PlayerID: res.UID,
			GameAuthSign: res.Secret, Name: res.Uname, NickName: res.Uname,
			CredState: "ok",
		}, nil
	}
	channelSpecs[channel.KeyVivo] = &stub
	defer func() { channelSpecs[channel.KeyVivo] = oldSpec }()

	// 会话目录预先存在（adopt 归档要走改名）
	profileDir := channelSpecs[channel.KeyVivo].profileDir("100001")
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// 预写结果文件（模拟子进程已捕获成功）
	outPath := filepath.Join(root, "vivo-login-result.json")
	res, _ := json.Marshal(CapturedLoginResult{OK: true, UID: "100001", Secret: "tok", Uname: "测试用户"})
	if err := os.WriteFile(outPath, res, 0o600); err != nil {
		t.Fatal(err)
	}

	// 空 cmd 即"子进程立即退出"——结果文件已就位时不应影响判定
	watchChannelLogin(channel.KeyVivo, &exec.Cmd{}, outPath, "100001", profileDir)

	list, err := loadAccounts()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range list {
		if a.Channel == channel.KeyVivo && a.PlayerID == "100001" && a.GameAuthSign == "tok" {
			found = true
		}
	}
	if !found {
		t.Fatalf("登录成功的账号必须落库，实际: %+v", list)
	}
	// 会话目录应归档到账号名下
	if _, err := os.Stat(filepath.Join(webLoginRoot(), "vivo", "100001")); err != nil {
		t.Fatalf("会话目录应归档: %v", err)
	}
}
