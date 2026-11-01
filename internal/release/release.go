// Package release 版本检查 / 更新日志 / 公告 / 下载渠道。
//
// 设计为可独立移除的模块：fork 想去掉更新与公告功能，删掉本目录、
// internal/web/server.go 里两处 "release" 路由注册、
// web 前端 SettingsPanel 的「网盘与关于」区块即可，其余代码零依赖。
//
// 所有对外地址集中在本文件的常量里，fork 后改这几个常量即可接入
// 自己的云端；管理接口（发布公告 / 登记版本）不在客户端，密钥只存在
// 云端项目与 GitHub Secrets 里，本仓库不含任何密钥。
package release

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// ── fork 后需要改的常量 ──────────────────────────────────────────

const (
	// APIBase 版本检查与公告的云端地址
	APIBase = "https://api-skylogin.mmlhl.party"
	// GitHubURL 仓库发布页
	GitHubURL = "https://github.com/mmlhl/skylogin/releases/latest"
	// PanURL 网盘下载页（123 云盘分享链接）
	PanURL = "https://1822397226.share.123pan.cn/123pan/87OJjv-vhMl?pwd=l5sX"
)

// DownloadLinks 关于页 / 更新提示里的下载渠道（顺序即展示顺序）。
func DownloadLinks() []struct{ Name, URL string } {
	return []struct{ Name, URL string }{
		{"GitHub 发布页", GitHubURL},
		{"123 网盘", PanURL},
	}
}

// AllowExternal 判断一个 URL 是否允许由登录器打开（下载渠道白名单）。
func AllowExternal(url string) bool {
	for _, allowed := range []string{GitHubURL, "https://github.com/mmlhl/skylogin/releases", PanURL} {
		if url == allowed || (len(url) > len(allowed) && url[:len(allowed)] == allowed) {
			return true
		}
	}
	return false
}

// ── 版本检查 ─────────────────────────────────────────────────────

// Latest 是云端登记的最新版本信息。
type Latest struct {
	Version     string `json:"version"`
	Build       int    `json:"build"`
	DownloadURL string `json:"download_url"`
	Changelog   string `json:"changelog"`
	Force       bool   `json:"force"`
	PublishedAt string `json:"published_at"`
}

// CheckUpdate 用本地构建号查询云端最新版本。
// 返回 (最新版本信息, 是否有更新, 错误)；云端没登记过版本时 latest 为 nil。
func CheckUpdate(build int) (*Latest, bool, error) {
	var res struct {
		HasUpdate bool    `json:"has_update"`
		Latest    *Latest `json:"latest"`
	}
	if err := getJSON(APIBase+"/version/check?build="+fmtInt(build), &res, 0); err != nil {
		return nil, false, err
	}
	if res.Latest == nil {
		return nil, false, nil
	}
	return res.Latest, res.HasUpdate, nil
}

// ── 公告 ─────────────────────────────────────────────────────────

// AnnouncementContent 公告正文（云端 JSON 字符串解析后的结构）。
type AnnouncementContent struct {
	AuthorWords string   `json:"author_words"`
	Fixes       []string `json:"fixes"`
	Updates     []string `json:"updates"`
}

// Announcement 一条公告。Version 是公告序号（客户端记住见过的最大值做增量）。
type Announcement struct {
	ID        int                 `json:"id"`
	Type      string              `json:"type"`
	Title     string              `json:"title"`
	Content   AnnouncementContent `json:"content"`
	Version   int                 `json:"version"`
	BuildCode int                 `json:"build_code"`
	CreatedAt string              `json:"created_at"`
}

// Announcements 增量拉取 since 之后的公告（按序号升序，最多 50 条）。
func Announcements(since int) ([]Announcement, error) {
	var list []Announcement
	if err := getJSON(APIBase+"/announcements?version="+fmtInt(since), &list, 0); err != nil {
		return nil, err
	}
	return list, nil
}

// ── HTTP 基础 ─────────────────────────────────────────────────────

var client = &http.Client{Timeout: 5 * time.Second}

func getJSON(url string, out any, _ int) error {
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("云端返回 %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return errors.New("响应解析失败")
	}
	return nil
}

func fmtInt(n int) string {
	return fmt.Sprintf("%d", n)
}
