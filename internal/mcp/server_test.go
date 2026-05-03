package mcp

import "testing"

func TestIsInboxMessage(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"2026-05-03T12-00-00Z-from-evan.md", true},
		{"2026-05-03T12-00-00Z-from-some_agent.md", true},
		{".hidden", false},
		{".DS_Store", false},
		{"87-feature-teardown.md.prev", false},
		{"87-feature-teardown.md.read", false},
		{"random-note.md", false},
		{"2026-05-03T12-00-00Z-from-evan.txt", false},
		{"README", false},
		{"", false},
	}
	for _, c := range cases {
		got := isInboxMessage(c.name)
		if got != c.want {
			t.Errorf("isInboxMessage(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestParseInboxName(t *testing.T) {
	cases := []struct {
		filename string
		ts       string
		sender   string
	}{
		{"2026-05-03T12-00-00Z-from-evan.md", "2026-05-03T12-00-00Z", "evan"},
		{"2026-05-03T12-00-00Z-from-some_agent.md", "2026-05-03T12-00-00Z", "some_agent"},
		{"no-marker.md", "", ""},
		{"87-feature-teardown.md.prev", "", ""},
	}
	for _, c := range cases {
		ts, sender := parseInboxName(c.filename)
		if ts != c.ts || sender != c.sender {
			t.Errorf("parseInboxName(%q) = (%q, %q), want (%q, %q)",
				c.filename, ts, sender, c.ts, c.sender)
		}
	}
}
