package wire

import "testing"

func TestFormatSslipHost(t *testing.T) {
	tests := []struct {
		ip   string
		want string
	}{
		{"1.2.3.4", "1.2.3.4.sslip.io"},
		{"10.0.0.1", "10.0.0.1.sslip.io"},
		{"2001:db8::1", "2001-db8--1.sslip.io"},
		{"::1", "--1.sslip.io"},
		{"invalid", "invalid.sslip.io"},
	}
	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			got := FormatSslipHost(tt.ip)
			if got != tt.want {
				t.Errorf("FormatSslipHost(%q) = %q, want %q", tt.ip, got, tt.want)
			}
		})
	}
}

func TestProbePublicIPWith_InvalidURL(t *testing.T) {
	// Should return "" for an unreachable URL.
	got := ProbePublicIPWith("http://192.0.2.1:1/ip", nil)
	if got != "" {
		t.Errorf("ProbePublicIPWith(bogus URL) = %q, want empty", got)
	}
}
