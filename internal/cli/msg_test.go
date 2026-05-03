package cli

import "testing"

func TestIsInboxMessage(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"2026-05-03T12-00-00Z-from-evan.md", true},
		{".hidden", false},
		{"87-feature-teardown.md.prev", false},
		{"87-feature-teardown.md.read", false},
		{"random-note.md", false},
		{"2026-05-03T12-00-00Z-from-evan.txt", false},
	}
	for _, c := range cases {
		got := isInboxMessage(c.name)
		if got != c.want {
			t.Errorf("isInboxMessage(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}
