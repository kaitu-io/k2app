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

func (m *mockDialer) DialTCP(ctx context.Context, addr string) (net.Conn, error) {
	return nil, nil
}
func (m *mockDialer) DialUDP(ctx context.Context, addr string) (net.PacketConn, error) {
	return nil, nil
}
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
			raw:  "k2v5://device123:eyJtoken@1.2.3.4:443?sni=www.microsoft.com&ech=AEX05&fp=chrome&hop=10000-20000",
			want: Config{
				UDID:        "device123",
				Token:       "eyJtoken",
				Host:        "1.2.3.4",
				Port:        443,
				SNI:         "www.microsoft.com",
				ECH:         "AEX05",
				Fingerprint: "chrome",
				HopStart:    10000,
				HopEnd:      20000,
			},
		},
		{
			name: "minimal URL with defaults",
			raw:  "k2v5://dev:tok@1.2.3.4:443",
			want: Config{
				UDID:        "dev",
				Token:       "tok",
				Host:        "1.2.3.4",
				Port:        443,
				SNI:         "www.microsoft.com",
				Fingerprint: "chrome",
			},
		},
		{
			name: "IPv6 host",
			raw:  "k2v5://dev:tok@[2001:db8::1]:443",
			want: Config{
				UDID:        "dev",
				Token:       "tok",
				Host:        "2001:db8::1",
				Port:        443,
				SNI:         "www.microsoft.com",
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
			name:    "invalid hop range",
			raw:     "k2v5://dev:tok@1.2.3.4:443?hop=abc",
			wantErr: true,
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
