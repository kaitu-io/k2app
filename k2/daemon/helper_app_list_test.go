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
		// /proc/PID/exe never returns trailing-slash paths in practice
		// (kernel always resolves to a file), but for the synthetic case
		// filepath.Clean strips the trailing slash → basename is "bin".
		{"/bin/", "bin"},
		{"/", ""},
		{"/usr/bin/../bin/zsh", "zsh"},
		// Spaces / parens in path basename — Electron / Slack / Code helper paths
		{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome Helper", "Google Chrome Helper"},
		{"/Applications/Visual Studio Code.app/Contents/Frameworks/Code Helper (GPU).app/Contents/MacOS/Code Helper (GPU)", "Code Helper (GPU)"},
		// Deleted-binary suffix appears when apt/dpkg/snap replaces a running binary.
		// MUST be stripped so the UI and the daemon's process searcher agree on the
		// post-upgrade attribution name.
		{"/usr/bin/chrome (deleted)", "chrome"},
		{"/opt/foo/Google Chrome Helper (deleted)", "Google Chrome Helper"},
		// Edge: just " (deleted)" with empty path → empty result, not "(deleted)".
		{" (deleted)", ""},
		// UTF-8 multi-byte basename round-trip
		{"/Applications/微信", "微信"},
		{"/opt/百度网盘/百度网盘", "百度网盘"},
	}
	for _, c := range cases {
		got := parseProcExeBasename(c.link)
		if got != c.want {
			t.Errorf("parseProcExeBasename(%q) = %q, want %q", c.link, got, c.want)
		}
	}
}
