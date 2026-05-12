//go:build linux

package daemon

import "testing"

func TestParseProcExeBasename(t *testing.T) {
	cases := []struct {
		link string
		want string
	}{
		{"/usr/bin/curl", "curl"},
		{"/usr/local/bin/firefox", "firefox"},
		{"", ""},
		{"/bin/", ""},
		{"/", ""},
		{"/usr/bin/../bin/zsh", "zsh"},
	}
	for _, c := range cases {
		got := parseProcExeBasename(c.link)
		if got != c.want {
			t.Errorf("parseProcExeBasename(%q) = %q, want %q", c.link, got, c.want)
		}
	}
}
