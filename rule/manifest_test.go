package rule

import "testing"

func TestRemoteNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool // is a strictly newer than b?
	}{
		{"2026-06-02T00:31:05Z", "2026-06-01T00:31:05Z", true},
		{"2026-06-02T00:31:05Z", "2026-06-02T00:31:05Z", false},
		{"2026-06-01T00:31:05Z", "2026-06-02T00:31:05Z", false},
		{"2026-06-02T00:31:05Z", "", true},            // any vs unknown local
		{"garbage", "2026-06-02T00:31:05Z", false},    // unparseable a → treated oldest
		{"2026-06-02", "2026-06-02T00:31:05Z", false}, // legacy date < timestamped
	}
	for _, c := range cases {
		if got := remoteNewer(c.a, c.b); got != c.want {
			t.Errorf("remoteNewer(%q,%q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
