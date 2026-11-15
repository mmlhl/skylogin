package app

// 自动进入的开关优先级：账号单独设置 > 多开默认 / 单开默认。
//
// 这三层是用户明确要求的（账号设置、多开设置、启动配置各一份），
// 所以优先级必须钉死，不能"看情况"。

import (
	"testing"
	"time"
)

func boolPtr(v bool) *bool { return &v }

func TestAutoEnterDefaultOn(t *testing.T) {
	var c Config // 两个字段都是 nil
	if !autoEnterDecision(Account{}, false, c) {
		t.Fatal("单开默认应该是开启")
	}
	if !autoEnterDecision(Account{}, true, c) {
		t.Fatal("多开默认应该是开启")
	}
}

func TestAutoEnterAccountOverridesGlobal(t *testing.T) {
	c := Config{AutoEnterPlay: boolPtr(false), MultiAutoEnter: boolPtr(false)}
	acc := Account{AutoEnter: boolPtr(true)}
	if !autoEnterDecision(acc, false, c) {
		t.Fatal("账号自己开了就该进，哪怕单开默认是关的")
	}
	if !autoEnterDecision(acc, true, c) {
		t.Fatal("账号自己开了就该进，哪怕多开默认是关的")
	}

	acc2 := Account{AutoEnter: boolPtr(false)}
	on := Config{AutoEnterPlay: boolPtr(true), MultiAutoEnter: boolPtr(true)}
	if autoEnterDecision(acc2, false, on) {
		t.Fatal("账号自己关了就不该进，哪怕默认是开的")
	}
	if autoEnterDecision(acc2, true, on) {
		t.Fatal("账号自己关了就不该进，哪怕多开默认是开的")
	}
}

func TestAutoEnterSingleAndMultiAreSeparate(t *testing.T) {
	c := Config{AutoEnterPlay: boolPtr(true), MultiAutoEnter: boolPtr(false)}
	if !autoEnterDecision(Account{}, false, c) {
		t.Fatal("单开开关是开的，单开应该进")
	}
	if autoEnterDecision(Account{}, true, c) {
		t.Fatal("多开开关是关的，多开不该进")
	}

	c2 := Config{AutoEnterPlay: boolPtr(false), MultiAutoEnter: boolPtr(true)}
	if autoEnterDecision(Account{}, false, c2) {
		t.Fatal("单开开关是关的，单开不该进")
	}
	if !autoEnterDecision(Account{}, true, c2) {
		t.Fatal("多开开关是开的，多开应该进")
	}
}

// TestAutoEnterWindowIsTenSeconds 时间窗是产品约束（"只在签票后 10 秒内"），
// 改这个常量必须同时改文档和界面提示，所以钉在测试里。
func TestAutoEnterWindowIsTenSeconds(t *testing.T) {
	if autoEnterWindow != 10*time.Second {
		t.Fatalf("自动进入的时间窗应该是 10 秒，现在是 %v", autoEnterWindow)
	}
}

// TestAutoEnterGate：多开的"窗口稳定"闸门——没放行之前不许开始计时。
func TestAutoEnterGate(t *testing.T) {
	releaseAutoEnterBatch() // 先清掉别的用例可能留下的闸门

	// 没有闸门（单开）时立刻通过
	done := make(chan struct{})
	go func() { waitAutoEnterGate(); close(done) }()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("没有闸门时不该阻塞")
	}

	// 挂上闸门：等待方必须被挡住
	armAutoEnterBatch()
	blocked := make(chan struct{})
	go func() { waitAutoEnterGate(); close(blocked) }()
	select {
	case <-blocked:
		t.Fatal("闸门还没放行就通过了")
	case <-time.After(150 * time.Millisecond):
	}

	// 放行后立刻通过
	releaseAutoEnterBatch()
	select {
	case <-blocked:
	case <-time.After(2 * time.Second):
		t.Fatal("放行之后应该立刻返回")
	}

	// 放行后闸门清空：再等也不会卡住
	again := make(chan struct{})
	go func() { waitAutoEnterGate(); close(again) }()
	select {
	case <-again:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("闸门已放行，后续等待不该阻塞")
	}
}
