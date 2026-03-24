//go:build linux

package gateway

import "testing"

func TestExtractServerHost(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"k2v5://example.com:443/path?token=x", "example.com"},
		{"k2v5://user:pass@example.com:443/path", "example.com"},
		{"k2v5://1.2.3.4:443/path", "1.2.3.4"},
		{"k2v5://[::1]:443/path", "::1"},
	}
	for _, tt := range tests {
		got := extractServerHost(tt.url)
		if got != tt.want {
			t.Errorf("extractServerHost(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}
