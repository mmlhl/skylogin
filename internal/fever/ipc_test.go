package fever

import (
	"encoding/binary"
	"encoding/json"
	"testing"
)

// 构造一个 op13 取票包，验证解析出的实例号与标识正确。
func TestParseTicketRequest(t *testing.T) {
	const instance = 2
	payload := make([]byte, 260)
	binary.LittleEndian.PutUint32(payload[0:4], uint32(instance+101))
	for i := 4; i < 260; i++ {
		payload[i] = byte(i)
	}
	packet := make([]byte, 12+len(payload))
	binary.LittleEndian.PutUint32(packet[0:4], uint32(len(payload)))
	binary.LittleEndian.PutUint32(packet[4:8], 13)
	copy(packet[12:], xorBytes(payload))

	gotInst, ident, ok := ParseTicketRequest(packet)
	if !ok {
		t.Fatal("应当解析成功")
	}
	if gotInst != instance {
		t.Fatalf("instance=%d 期望 %d", gotInst, instance)
	}
	if len(ident) != 256 {
		t.Fatalf("identifier 长度=%d", len(ident))
	}
	if ident[0] != payload[4] {
		t.Fatalf("identifier 内容不对")
	}
}

func TestParseRejectsWrongOpcode(t *testing.T) {
	packet := make([]byte, 12+260)
	binary.LittleEndian.PutUint32(packet[4:8], 14)
	if _, _, ok := ParseTicketRequest(packet); ok {
		t.Fatal("op14 不应被当成取票请求")
	}
}

func TestBuildTicketPacketRoundTrip(t *testing.T) {
	p := BuildTicketPacket("huawei", "TICKET123")
	if len(p) < 12 {
		t.Fatal("包太短")
	}
	if binary.LittleEndian.Uint32(p[4:8]) != 14 {
		t.Fatal("opcode 应为 14")
	}
	body := xorBytes(p[12:])
	var m map[string]string
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("payload 不是合法 JSON: %v", err)
	}
	if m["login_channel"] != "huawei" || m["ticket"] != "TICKET123" {
		t.Fatalf("payload 内容不对: %v", m)
	}
}

func TestXorIsInvolutive(t *testing.T) {
	src := []byte("hello fever ipc 0123456789")
	if string(xorBytes(xorBytes(src))) != string(src) {
		t.Fatal("XOR 应当可逆")
	}
}

func TestClientClassRegex(t *testing.T) {
	cases := []struct {
		in   string
		gid  string
		inst int
		ok   bool
	}{
		{"LHMW_FG_clientFGpma75_i2", "ma75", 2, true},
		{"LHMW_FG_clientFGpa50_i0", "a50", 0, true},
		{"LHMW_FG_Main", "", 0, false},
		{"", "", 0, false},
	}
	for _, c := range cases {
		m := clientClassRe.FindStringSubmatch(c.in)
		if c.ok != (m != nil) {
			t.Fatalf("%q 匹配结果不符预期", c.in)
		}
		if m != nil && m[1] != c.gid {
			t.Fatalf("%q gid=%q 期望 %q", c.in, m[1], c.gid)
		}
	}
}

// 真实窗口的端到端往返：宿主收 op13 → 回 op14。
func TestSelfTestIPC(t *testing.T) {
	if err := SelfTest(); err != nil {
		t.Fatalf("IPC 自检失败: %v", err)
	}
}
