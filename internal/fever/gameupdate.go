package fever

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// 游戏版本校验（2026-11-14）。
//
// 背景：官方发烧平台启动游戏前会问服务器「最新包版本是多少，我本地的是不是旧了」，
// 旧了就先更新再启动。我们的登录器绕过平台直接拉起 sky.exe，用户完全感知不到
// 游戏更新了 —— 版本对不上时进游戏会出各种 bug。所以这里照抄官方的校验链路，
// 启动器打开时先查一次，落后就在界面上提示用户去发烧平台更新。
//
// 抓包参数（capture-20261029-181435.jsonl / capture-20261108）：
//   GET https://loadingbaycn.webapp.163.com/app/v1/file_distribution/latest_build
//       ?app_ids=[63]
//       &app_origin_versions=[{"app_id":63,"origin_version_code":"v3_5219_..."}]
//   应答 data.apps[0]：
//     version_code    服务器当前最新包版本（如 "v3_5219_3f5b..."）
//     app_origin_version 平台侧记录的本地版本（已是最新时为 null）
//     update_actions  需要执行的更新动作列表（已最新时是 []）
//
// 本地版本号的来源与官方一致：注册表
//   HKCU\Software\FeverGames\FeverGamesInstaller\game\63\VersionCode
// （装机/更新完成后平台写入；64 位下部分渠道落在 HKLM WOW6432Node）

// GameUpdate 是一次游戏版本比对的结果。
type GameUpdate struct {
	LocalVersion  string `json:"local_version"`  // 本地注册表里的包版本（可能为空 = 未走平台安装）
	LatestVersion string `json:"latest_version"` // 服务器上的最新包版本
	UpdateNeeded  bool   `json:"update_needed"`  // 本地 != 最新 → 需要去发烧平台更新
	Checked       bool   `json:"checked"`        // 是否成功完成了一次比对（网络失败时 false）
}

// latestBuildURL 是官方平台查游戏包版本的接口（抓包实测，无鉴权）。
const latestBuildURL = "https://loadingbaycn.webapp.163.com/app/v1/file_distribution/latest_build"

// skyAppID 是光遇在发烧平台里的应用 id（注册表 game 子键、抓包参数一致）。
const skyAppID = 63

// SkyVersionCode 读本地游戏包版本（与官方平台读同一处注册表）。
//
// 不写死 app id：枚举 game 子键，挑 StartupPath 是 sky.exe 的那个，
// 和 StartupArgs() 的找法保持一致（多游戏机器上不会认错）。
func SkyVersionCode() string {
	for _, r := range regRoots {
		k, err := registry.OpenKey(r.key, r.path+`\game`, registry.ENUMERATE_SUB_KEYS)
		if err != nil {
			continue
		}
		subs, err := k.ReadSubKeyNames(-1)
		k.Close()
		if err != nil {
			continue
		}
		for _, sub := range subs {
			gk, err := registry.OpenKey(r.key, r.path+`\game\`+sub, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			startup, _, _ := gk.GetStringValue("StartupPath")
			code, _, _ := gk.GetStringValue("VersionCode")
			gk.Close()
			if strings.EqualFold(strings.TrimSpace(startup), "sky.exe") && code != "" {
				return strings.TrimSpace(code)
			}
		}
	}
	return ""
}

// httpGetDefault 是 CheckGameUpdate 的默认下载器（5 秒超时，只收 200）。
func httpGetDefault(req string) ([]byte, error) {
	cli := &http.Client{Timeout: 5 * time.Second}
	resp, err := cli.Get(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("查询游戏版本失败: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// latestBuildReply 只解析我们关心的三个字段，其余忽略。
type latestBuildReply struct {
	Data struct {
		Apps []struct {
			VersionCode      string  `json:"version_code"`
			AppOriginVersion *string `json:"app_origin_version"`
			UpdateActions    []any   `json:"update_actions"`
		} `json:"apps"`
	} `json:"data"`
}

// CheckGameUpdate 按官方平台的参数问一次服务器：本地包是不是旧了。
//
// localCode 传 SkyVersionCode() 的结果；为空时仍会查询（把最新版本带回来），
// 但 UpdateNeeded 恒为 false —— 没有本地基准，不能瞎报"要更新"。
// 网络失败返回 Checked=false 的结果，调用方自行决定怎么提示。
func CheckGameUpdate(localCode string, get func(string) ([]byte, error)) (*GameUpdate, error) {
	if get == nil {
		get = httpGetDefault
	}
	// 参数与抓包逐字段一致（JSON 里字符串转义后整体再 URL 编码）
	origin := fmt.Sprintf(`[{"app_id":%d,"origin_version_code":%q}]`, skyAppID, localCode)
	req := latestBuildURL + "?app_ids=" + url.QueryEscape(fmt.Sprintf("[%d]", skyAppID)) +
		"&app_origin_versions=" + url.QueryEscape(origin)
	body, err := get(req)
	if err != nil {
		return &GameUpdate{LocalVersion: localCode}, err
	}
	var reply latestBuildReply
	if err := json.Unmarshal(body, &reply); err != nil {
		return &GameUpdate{LocalVersion: localCode}, err
	}
	res := &GameUpdate{LocalVersion: localCode, Checked: true}
	if len(reply.Data.Apps) == 0 {
		return res, fmt.Errorf("服务器没有返回光遇的包信息")
	}
	res.LatestVersion = reply.Data.Apps[0].VersionCode
	// 判定规则与官方一致：服务器最新版本 != 本地版本 就说明有更新没打
	// （update_actions 非空是同义信号，两个条件做或，任一命中都提示）
	res.UpdateNeeded = localCode != "" &&
		(res.LatestVersion != localCode || len(reply.Data.Apps[0].UpdateActions) > 0)
	return res, nil
}

// PlatformExe 找发烧平台本体 FeverGamesLauncher.exe 的完整路径。
//
// 依次尝试（2026-11-14 实测：卸载表 UninstallString 登记的
// "D:\Program Files\FeverGames\..." 早已不存在，本体在 "D:\FeverGames\..."）：
//  1. launcherRoot()——皮肤查找用的同一套机制（卸载表 InstallLocation，
//     跟着官方平台走，最可靠）
//  2. 常规安装位置逐个盘根扫 <盘>:\FeverGames\FeverGamesLauncher.exe
//     及 Program Files / LocalAppData 下的 FeverGames 目录
//
// 都找不到返回空字符串（界面退而打开官网 fever.163.com）。
func PlatformExe() string {
	const exeName = "FeverGamesLauncher.exe"
	// 1) 平台安装目录（与 FindMpaySkin 同源）
	if root := launcherRoot(); root != "" {
		if p := filepath.Join(root, exeName); isFile(p) {
			return p
		}
	}
	// 2) 常规安装位置兜底
	dirs := []string{
		filepath.Join(os.Getenv("ProgramFiles"), "FeverGames"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "FeverGames"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "FeverGames"),
	}
	// 逐个盘根扫 <盘>:\FeverGames（官方默认就装在盘根，实测 D:\FeverGames）。
	// 注意盘符必须按**原始位序**算：bit0=A、bit1=B…，位移循环里要自己记下标，
	// 不能用 TrailingZeros（位移后恒为 0，全算成 A 盘）。
	if drs, err := windows.GetLogicalDrives(); err == nil {
		for i := 0; i < 26; i++ {
			if drs&(1<<uint(i)) != 0 {
				dirs = append(dirs, string(rune('A'+i))+`:\FeverGames`)
			}
		}
	}
	for _, dir := range dirs {
		p := filepath.Join(dir, exeName)
		if isFile(p) {
			return p
		}
	}
	return ""
}

// PrettyVersionCode 把 "v3_5219_3f5b..." 里的构建号抠出来给人看，
// 显示不了完整的就原样返回。
func PrettyVersionCode(code string) string {
	m := regexp.MustCompile(`^v\d+_(\d+)_`).FindStringSubmatch(code)
	if len(m) == 2 {
		return "build " + m[1]
	}
	return code
}
