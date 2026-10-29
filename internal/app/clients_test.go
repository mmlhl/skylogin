package app

import "testing"

func TestParseClientTitle(t *testing.T) {
	cases := []struct {
		in   string
		name string
		seq  int64
		ok   bool
	}{
		{"3 · 木木1", "木木1", 3, true},
		{"1 · demo@example.com", "demo@example.com", 1, true},
		{"光·遇", "", 0, false},
		{"", "", 0, false},
		{"12 · ", "", 0, false},
	}
	for _, c := range cases {
		name, seq, ok := parseClientTitle(c.in)
		if ok != c.ok || name != c.name || seq != c.seq {
			t.Fatalf("parseClientTitle(%q) = (%q,%d,%v) want (%q,%d,%v)", c.in, name, seq, ok, c.name, c.seq, c.ok)
		}
	}
}
