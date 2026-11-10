package app

// 渠道登录的「问答」通道。
//
// channel.Progress 的 Ask 需要停下来等用户输入，而 HTTP 是两次独立请求：
// 前端轮询 /status 看到待答问题，提交 /answer 之后流程才继续。
// 这里用一个带 channel 的会话把两边接起来，Method 那边看到的就是一个同步阻塞的 Ask。

import (
	"encoding/base64"
	"context"
	"fmt"
	"sync"

	"skylogin/internal/channel"
)

// askSession 一次登录流程里的问答状态。
type askSession struct {
	mu      sync.Mutex
	cur     *channel.Slice
	waiting chan map[string]string
}

func newAskSession() *askSession { return &askSession{} }

// Ask 抛一个问题，然后阻塞等前端提交答案。
func (s *askSession) Ask(ctx context.Context, sl channel.Slice) (map[string]string, error) {
	ch := make(chan map[string]string, 1)

	s.mu.Lock()
	s.cur = &sl
	s.waiting = ch
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		s.cur = nil
		s.waiting = nil
		s.mu.Unlock()
	}()

	select {
	case ans := <-ch:
		return ans, nil
	case <-ctx.Done():
		// 超时或用户取消：一定要走这条，否则 goroutine 永远挂着
		return nil, fmt.Errorf("等待输入超时")
	}
}

// Answer 提交答案。没有待答问题时返回错误，避免误伤上一步。
func (s *askSession) Answer(vals map[string]string) error {
	s.mu.Lock()
	ch := s.waiting
	s.mu.Unlock()
	if ch == nil {
		return fmt.Errorf("当前没有等待输入的步骤")
	}
	select {
	case ch <- vals:
		return nil
	default:
		return fmt.Errorf("答案已提交，请勿重复")
	}
}

// Pending 把待答问题转成界面能直接用的结构；没有则返回 nil。
func (s *askSession) Pending() map[string]any {
	s.mu.Lock()
	sl := s.cur
	s.mu.Unlock()
	if sl == nil {
		return nil
	}

	fields := make([]map[string]any, 0, len(sl.Fields))
	for _, f := range sl.Fields {
		fields = append(fields, map[string]any{
			"name":        f.Name,
			"label":       f.Label,
			"type":        f.Type,
			"placeholder": f.Placeholder,
			"maxLen":      f.MaxLen,
		})
	}
	opts := make([]map[string]any, 0, len(sl.Options))
	for _, o := range sl.Options {
		opts = append(opts, map[string]any{"value": o.Value, "label": o.Label})
	}
	return map[string]any{
		"kind":    sl.Kind,
		"title":   sl.Title,
		"fields":  fields,
		"options": opts,
	}
}

// 每个渠道一个问答会话：同渠道的登录是串行的（tracker.begin 会拦重复）。
var (
	channelAsksMu sync.Mutex
	channelAsks   = map[string]*askSession{}
)

func askOf(key string) *askSession {
	channelAsksMu.Lock()
	defer channelAsksMu.Unlock()
	s := channelAsks[key]
	if s == nil {
		s = newAskSession()
		channelAsks[key] = s
	}
	return s
}

// PendingChannelAsk 返回某渠道当前等待用户输入的步骤；没有则 nil。
func PendingChannelAsk(key string) map[string]any { return askOf(key).Pending() }

// AnswerChannelAsk 提交用户输入，唤醒阻塞中的登录流程。
func AnswerChannelAsk(key string, vals map[string]string) error {
	return askOf(key).Answer(vals)
}

// channelProgress 把 channel.Progress 落到界面每秒轮询的那份状态上。
type channelProgress struct {
	tr  *loginTracker
	ask *askSession
}

// Phase 只更新文案：tracker 的 phase 只有 idle/waiting/done/failed，
// Method 传进来的细分阶段名不参与状态机，否则界面会显示在一个不存在的状态上。
func (p *channelProgress) Phase(_, msg string) { p.tr.set("waiting", msg, "", "") }

// QR 把二维码交给界面显示。应用宝的微信 / QQ 扫码走这里——
// 纯协议登录没有窗口，用户只能看我们自己画的二维码。
func (p *channelProgress) QR(png []byte, _ string) {
	if len(png) == 0 {
		return
	}
	p.tr.setQR(base64.StdEncoding.EncodeToString(png))
}

func (p *channelProgress) Ask(ctx context.Context, s channel.Slice) (map[string]string, error) {
	return p.ask.Ask(ctx, s)
}

