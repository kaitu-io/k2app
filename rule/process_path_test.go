package rule

import "testing"

func TestNormalizeProcessDir(t *testing.T) {
	cases := []struct {
		goos, in, want string
	}{
		{"darwin", "/Applications/Doubao.app", "/Applications/Doubao.app/"},
		{"darwin", "/Applications/Doubao.app/", "/Applications/Doubao.app/"},
		{"darwin", "/Applications/Doubao.app///", "/Applications/Doubao.app/"},
		{"darwin", " /Applications/A B.app ", "/Applications/A B.app/"},
		{"darwin", "/Applications/CaseKept.app", "/Applications/CaseKept.app/"},
		// Never a prefix: these would capture every process.
		{"darwin", "", ""},
		{"darwin", "/", ""},
		{"darwin", "Applications/Doubao.app", ""},
		{"linux", "/opt/app", "/opt/app/"},
		{"windows", `C:\Program Files\WeChat`, `c:\program files\wechat\`},
		{"windows", `C:/Program Files/WeChat/`, `c:\program files\wechat\`},
		{"windows", `\\server\share\app`, `\\server\share\app\`},
		{"windows", `C:\`, ""},
		{"windows", `C:`, ""},
		{"windows", `Program Files\WeChat`, ""},
		{"windows", "", ""},
	}
	for _, c := range cases {
		if got := NormalizeProcessDir(c.goos, c.in); got != c.want {
			t.Errorf("NormalizeProcessDir(%s, %q) = %q, want %q", c.goos, c.in, got, c.want)
		}
	}
}

// A process matches an apps route by living under one of its directories OR
// by its name — and the directory test is a path-COMPONENT test.
func TestMatchMeta_ProcessPathPrefix(t *testing.T) {
	r := &RouteEntry{
		ProcessNames:        []string{"OutsideHelper"},
		ProcessPathPrefixes: []string{NormalizeProcessDir("darwin", "/Applications/Doubao.app")},
	}
	cases := []struct {
		name string
		meta ConnMeta
		want bool
	}{
		{"main executable", ConnMeta{ProcessName: "Doubao", ProcessPath: "/Applications/Doubao.app/Contents/MacOS/Doubao"}, true},
		{"deeply nested helper with a name no list carries",
			ConnMeta{ProcessName: "chrome_crashpad_handler", ProcessPath: "/Applications/Doubao.app/Contents/Helpers/Doubao Browser.app/Contents/Frameworks/F.framework/Versions/1/Helpers/chrome_crashpad_handler"}, true},
		{"same basename in ANOTHER app must not match",
			ConnMeta{ProcessName: "chrome_crashpad_handler", ProcessPath: "/Applications/Google Chrome.app/Contents/Frameworks/F.framework/Helpers/chrome_crashpad_handler"}, false},
		{"sibling bundle sharing the string prefix",
			ConnMeta{ProcessName: "x", ProcessPath: "/Applications/Doubao.app2/Contents/MacOS/x"}, false},
		{"name hit with a path elsewhere (process outside the bundle)",
			ConnMeta{ProcessName: "OutsideHelper", ProcessPath: "/Library/Application Support/Doubao/OutsideHelper"}, true},
		{"name hit when the searcher reports no path",
			ConnMeta{ProcessName: "OutsideHelper"}, true},
		{"path hit when the name is unreadable",
			ConnMeta{ProcessPath: "/Applications/Doubao.app/Contents/MacOS/Doubao"}, true},
		{"unattributed connection", ConnMeta{}, false},
	}
	for _, c := range cases {
		if got := matchMeta(r, c.meta); got != c.want {
			t.Errorf("%s: matchMeta = %v, want %v", c.name, got, c.want)
		}
	}
}

// A paths-only route is a process route end to end: it routes connections,
// flips the DNS view, and keeps process attribution switched on.
func TestEngine_ProcessPathOnlyRoute(t *testing.T) {
	const direct, proxy = Target(0), Target(2)
	eng := NewEngine([]RouteEntry{{
		Target:              direct,
		ProcessPathPrefixes: []string{"/Applications/Doubao.app/"},
	}}, proxy, nil, false)

	if !eng.HasProcessCriteria() {
		t.Fatal("a path-prefix route must keep process attribution on — without it ProcessPath is never filled and the route can never match")
	}
	in := ConnMeta{ProcessName: "Doubao Browser Helper", ProcessPath: "/Applications/Doubao.app/Contents/Helpers/x/Doubao Browser Helper", Network: "tcp", Port: 443}
	out := ConnMeta{ProcessName: "Safari", ProcessPath: "/Applications/Safari.app/Contents/MacOS/Safari", Network: "tcp", Port: 443}
	if got := eng.MatchConn("203.0.113.9", in); got != direct {
		t.Errorf("process inside the bundle routed to %v, want direct", got)
	}
	if got := eng.MatchConn("203.0.113.9", out); got != proxy {
		t.Errorf("process outside the bundle routed to %v, want fallback", got)
	}
	// DNS view override: identity-only meta, path alone is enough.
	if tgt, ok := eng.MatchDNSMeta(ConnMeta{ProcessPath: in.ProcessPath}); !ok || tgt != direct {
		t.Errorf("MatchDNSMeta(path only) = (%v, %v), want (direct, true)", tgt, ok)
	}
	if _, ok := eng.MatchDNSMeta(ConnMeta{ProcessName: out.ProcessName, ProcessPath: out.ProcessPath}); ok {
		t.Error("MatchDNSMeta matched a process outside the bundle")
	}
}
