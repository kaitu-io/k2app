package wire

import (
	"context"
	"net"
	"testing"
)

// Compile-time interface satisfaction checks
var _ Dialer = (*mockDialer)(nil)
var _ Listener = (*mockListener)(nil)
var _ UDPHandler = (*mockUDPHandler)(nil)

type mockDialer struct{}

func (m *mockDialer) DialTCP(ctx context.Context, addr string, earlyData []byte) (net.Conn, error) {
	return nil, nil
}
func (m *mockDialer) DialUDP(ctx context.Context, addr string) (net.PacketConn, error) {
	return nil, nil
}
func (m *mockDialer) DialStreamUDP(ctx context.Context, addr string) (net.PacketConn, error) {
	return nil, nil
}
func (m *mockDialer) Reset()       {}
func (m *mockDialer) Close() error { return nil }

type mockListener struct{}

func (m *mockListener) AcceptTCP(ctx context.Context) (net.Conn, string, error) {
	return nil, "", nil
}
func (m *mockListener) ServeUDP(ctx context.Context, handler UDPHandler) error { return nil }
func (m *mockListener) Close() error                                           { return nil }

type mockUDPHandler struct{}

func (m *mockUDPHandler) HandleUDP(conn net.PacketConn, addr string) {}

func TestParseURL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    Config
		wantErr bool
	}{
		{
			name: "full URL with all params",
			raw:  "k2v5://device123:eyJtoken@1.2.3.4:443?ech=AEX05&fp=chrome",
			want: Config{
				UDID:        "device123",
				Token:       "eyJtoken",
				Host:        "1.2.3.4",
				Port:        443,
				ECH:         "AEX05",
				Fingerprint: "chrome",
			},
		},
		{
			// Un-upgraded servers still emit hop=; it must parse clean and be
			// dropped, not error out and not resurrect a dial-port override.
			name: "legacy hop param is ignored",
			raw:  "k2v5://device123:eyJtoken@1.2.3.4:443?ech=AEX05&fp=chrome&hop=10000-20000",
			want: Config{
				UDID:        "device123",
				Token:       "eyJtoken",
				Host:        "1.2.3.4",
				Port:        443,
				ECH:         "AEX05",
				Fingerprint: "chrome",
			},
		},
		{
			name: "minimal URL with defaults",
			raw:  "k2v5://dev:tok@1.2.3.4:443",
			want: Config{
				UDID:        "dev",
				Token:        "tok",
				Host:        "1.2.3.4",
				Port:        443,
				Fingerprint: "chrome",
			},
		},
		{
			name: "IPv6 host",
			raw:  "k2v5://dev:tok@[2001:db8::1]:443",
			want: Config{
				UDID:        "dev",
				Token:        "tok",
				Host:        "2001:db8::1",
				Port:        443,
				Fingerprint: "chrome",
			},
		},
		{
			name:    "wrong scheme",
			raw:     "https://1.2.3.4:443",
			wantErr: true,
		},
		{
			name:    "missing auth",
			raw:     "k2v5://1.2.3.4:443",
			wantErr: true,
		},
		{
			// hop= is no longer parsed, so even a malformed one must not
			// reject an otherwise valid URL from an un-upgraded server.
			name: "malformed legacy hop param does not reject the URL",
			raw:  "k2v5://dev:tok@1.2.3.4:443?hop=abc",
			want: Config{
				UDID:        "dev",
				Token:       "tok",
				Host:        "1.2.3.4",
				Port:        443,
				Fingerprint: "chrome",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseURL(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseURL(%q) expected error, got nil", tt.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseURL(%q) unexpected error: %v", tt.raw, err)
			}
			if got != tt.want {
				t.Errorf("ParseURL(%q)\n  got  %+v\n  want %+v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestErrors(t *testing.T) {
	if ErrNotConnected.Error() == "" {
		t.Error("ErrNotConnected should have a message")
	}
	if ErrUDPNotSupported.Error() == "" {
		t.Error("ErrUDPNotSupported should have a message")
	}
}

func TestParsePins(t *testing.T) {
	// Generate a real cert pair to get valid pin hashes
	pair, err := GenerateCertPair()
	if err != nil {
		t.Fatalf("GenerateCertPair: %v", err)
	}
	pinStr := pair.FormatPins()

	pins, err := ParsePins(pinStr)
	if err != nil {
		t.Fatalf("ParsePins(%q) error: %v", pinStr, err)
	}
	if len(pins) != 2 {
		t.Fatalf("ParsePins returned %d hashes, want 2", len(pins))
	}
	if pins[0] != pair.RSAPinHash {
		t.Errorf("pins[0] = %x, want %x", pins[0], pair.RSAPinHash)
	}
	if pins[1] != pair.ECPinHash {
		t.Errorf("pins[1] = %x, want %x", pins[1], pair.ECPinHash)
	}
}

func TestParsePins_Invalid(t *testing.T) {
	tests := []struct {
		name string
		pin  string
	}{
		{"no prefix", "AAAA,BBBB"},
		{"wrong prefix", "md5:AAAA,md5:BBBB"},
		{"bad base64", "sha256:!!!invalid!!!"},
		{"wrong hash length", "sha256:AAAA"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParsePins(tt.pin)
			if err == nil {
				t.Errorf("ParsePins(%q) should have returned error", tt.pin)
			}
		})
	}
}

func TestParsePins_Empty(t *testing.T) {
	pins, err := ParsePins("")
	if err != nil {
		t.Fatalf("ParsePins(\"\") error: %v", err)
	}
	if pins != nil {
		t.Errorf("ParsePins(\"\") = %v, want nil", pins)
	}
}

func TestParseURL_IPv4Param(t *testing.T) {
	raw := "k2v5://dev:tok@camouflage.example.com:443?ip=45.78.4.222"
	cfg, err := ParseURL(raw)
	if err != nil {
		t.Fatalf("ParseURL error: %v", err)
	}
	if cfg.Host != "camouflage.example.com" {
		t.Errorf("Host = %q, want %q", cfg.Host, "camouflage.example.com")
	}
	if cfg.IPv4 != "45.78.4.222" {
		t.Errorf("IPv4 = %q, want %q", cfg.IPv4, "45.78.4.222")
	}
}

func TestParseURL_IPv6RawParam(t *testing.T) {
	raw := "k2v5://dev:tok@1.2.3.4:443?ip6=2001:db8::1"
	cfg, err := ParseURL(raw)
	if err != nil {
		t.Fatalf("ParseURL error: %v", err)
	}
	if cfg.IPv6 != "2001:db8::1" {
		t.Errorf("IPv6 = %q, want %q", cfg.IPv6, "2001:db8::1")
	}
}

func TestParseURL_IPv6SslipBackwardCompat(t *testing.T) {
	raw := "k2v5://dev:tok@1.2.3.4:443?ip6=2001-db8--1.sslip.io"
	cfg, err := ParseURL(raw)
	if err != nil {
		t.Fatalf("ParseURL error: %v", err)
	}
	if cfg.IPv6 != "2001:db8::1" {
		t.Errorf("IPv6 = %q, want %q (sslip.io should be normalized to raw)", cfg.IPv6, "2001:db8::1")
	}
}

func TestParseURL_BothIPParams(t *testing.T) {
	raw := "k2v5://dev:tok@camouflage.example.com:443?ip=45.78.4.222&ip6=2001:db8::1&ech=ABC"
	cfg, err := ParseURL(raw)
	if err != nil {
		t.Fatalf("ParseURL error: %v", err)
	}
	if cfg.Host != "camouflage.example.com" {
		t.Errorf("Host = %q, want %q", cfg.Host, "camouflage.example.com")
	}
	if cfg.IPv4 != "45.78.4.222" {
		t.Errorf("IPv4 = %q, want %q", cfg.IPv4, "45.78.4.222")
	}
	if cfg.IPv6 != "2001:db8::1" {
		t.Errorf("IPv6 = %q, want %q", cfg.IPv6, "2001:db8::1")
	}
	if cfg.ECH != "ABC" {
		t.Errorf("ECH = %q, want %q", cfg.ECH, "ABC")
	}
}

func TestParseURL_NoIPParams(t *testing.T) {
	raw := "k2v5://dev:tok@1.2.3.4:443"
	cfg, err := ParseURL(raw)
	if err != nil {
		t.Fatalf("ParseURL error: %v", err)
	}
	if cfg.IPv4 != "" {
		t.Errorf("IPv4 = %q, want empty", cfg.IPv4)
	}
	if cfg.IPv6 != "" {
		t.Errorf("IPv6 = %q, want empty", cfg.IPv6)
	}
}

func TestNormalizeSslipIPv6(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"2001-db8--1.sslip.io", "2001:db8::1"},
		{"2001:db8::1", "2001:db8::1"},
		{"fe80--1.sslip.io", "fe80::1"},
		{"45.78.4.222", "45.78.4.222"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeSslipIPv6(tt.input)
			if got != tt.want {
				t.Errorf("normalizeSslipIPv6(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseURL_Pin(t *testing.T) {
	raw := "k2v5://dev:tok@1.2.3.4:443?pin=sha256:AAAA,sha256:BBBB"
	cfg, err := ParseURL(raw)
	if err != nil {
		t.Fatalf("ParseURL error: %v", err)
	}
	if cfg.Pin != "sha256:AAAA,sha256:BBBB" {
		t.Errorf("Pin = %q, want %q", cfg.Pin, "sha256:AAAA,sha256:BBBB")
	}
}
