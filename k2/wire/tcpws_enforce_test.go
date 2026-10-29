package wire

import (
	"context"
	"net"
	"testing"
	"time"
)

// setupTCPWSEnforcePair wires a client↔server pair over net.Pipe with a
// validator and enforce flag, so the TCP-WS device-auth gate can be driven
// end-to-end. Mirrors setupTCPWSPair but exercises the enforce path.
func setupTCPWSEnforcePair(t *testing.T, cfg Config, enforce bool, validator Validator) (*TCPWSClient, *TCPWSServer, func()) {
	t.Helper()
	clientConn, serverConn := net.Pipe()

	server := NewTCPWSServer()
	server.SetValidator(validator)
	server.SetEnforce(enforce)
	server.adm.authWait = 300 * time.Millisecond
	server.HandleConn(serverConn)

	client := NewTCPWSClient(cfg, nil)
	client.testConn = clientConn

	cleanup := func() {
		client.Close()
		server.Close()
		clientConn.Close()
		serverConn.Close()
	}
	return client, server, cleanup
}

// TestTCPWSEnforce_ForgedRejected is the TCP-WS twin of the QUIC P0 regression:
// under enforce=on, a client whose metadata fails validation must NOT be able to
// open a proxy stream. Without a TCP-WS gate, an attacker forces the TCP-WS
// fallback (tp=tcpws / block UDP) to bypass QUIC enforcement entirely.
func TestTCPWSEnforce_ForgedRejected(t *testing.T) {
	cfg := Config{Host: "127.0.0.1", Port: 443, UDID: "attacker", Token: "forged"}
	client, _, cleanup := setupTCPWSEnforcePair(t, cfg, true,
		func(token, udid, mode string) AuthVerdict {
			if udid == "good" && token == "valid" {
				return AuthVerdict{OK: true}
			}
			return AuthVerdict{Code: 401}
		})
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := client.DialTCP(ctx, "example.com:80", nil)
	if conn != nil {
		conn.Close()
	}
	if err == nil {
		t.Fatal("SECURITY REGRESSION: forged metadata proxied through an enforce=on TCP-WS server")
	}
}

// TestTCPWSEnforce_ValidAdmitted proves a legitimately-authenticated client is
// served under enforce=on.
func TestTCPWSEnforce_ValidAdmitted(t *testing.T) {
	cfg := Config{Host: "127.0.0.1", Port: 443, UDID: "good", Token: "valid"}
	client, server, cleanup := setupTCPWSEnforcePair(t, cfg, true,
		func(token, udid, mode string) AuthVerdict {
			if udid == "good" && token == "valid" {
				return AuthVerdict{OK: true}
			}
			return AuthVerdict{Code: 401}
		})
	defer cleanup()

	go func() {
		conn, _, err := server.AcceptTCP(context.Background())
		if err != nil {
			return
		}
		WriteStreamResponse(conn, nil)
		conn.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := client.DialTCP(ctx, "example.com:80", nil)
	if err != nil {
		t.Fatalf("valid client wrongly rejected under enforce: %v", err)
	}
	conn.Close()
}

// TestTCPWSEnforce_PermissiveServesUnauthenticated proves the rollout guarantee:
// with enforce=off a client whose metadata fails validation is still served, so
// upgrading the fix cannot cut off legacy clients before the flag is flipped.
func TestTCPWSEnforce_PermissiveServesUnauthenticated(t *testing.T) {
	cfg := Config{Host: "127.0.0.1", Port: 443, UDID: "legacy", Token: "whatever"}
	client, server, cleanup := setupTCPWSEnforcePair(t, cfg, false,
		func(token, udid, mode string) AuthVerdict { return AuthVerdict{Code: 401} }) // reject everyone
	defer cleanup()

	go func() {
		conn, _, err := server.AcceptTCP(context.Background())
		if err != nil {
			return
		}
		WriteStreamResponse(conn, nil)
		conn.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := client.DialTCP(ctx, "example.com:80", nil)
	if err != nil {
		t.Fatalf("permissive mode must serve unauthenticated clients, got: %v", err)
	}
	conn.Close()
}

// TestTCPWSCookie_SecondConnectIsWarm: cold auth issues a cookie; a second
// connection presenting it must be admitted WITHOUT another validator call.
func TestTCPWSCookie_SecondConnectIsWarm(t *testing.T) {
	dir := t.TempDir()
	signer, err := NewCookieSigner(dir)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	validator := func(token, udid, mode string) AuthVerdict {
		calls++
		return AuthVerdict{OK: true, UserID: 1, SvcExp: time.Now().Add(time.Hour).Unix()}
	}
	jar := NewCookieJar()
	cfg := Config{Host: "127.0.0.1", Port: 443, UDID: "good", Token: "valid"}

	dialOnce := func() {
		t.Helper()
		clientConn, serverConn := net.Pipe()
		server := NewTCPWSServer()
		server.SetValidator(validator)
		server.SetEnforce(true)
		server.SetCookieSigner(signer)
		server.adm.authWait = 300 * time.Millisecond
		server.HandleConn(serverConn)
		client := NewTCPWSClient(cfg, jar)
		client.testConn = clientConn
		defer func() { client.Close(); server.Close(); clientConn.Close(); serverConn.Close() }()

		go func() {
			conn, _, err := server.AcceptTCP(context.Background())
			if err != nil {
				return
			}
			WriteStreamResponse(conn, nil)
			conn.Close()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		conn, err := client.DialTCP(ctx, "example.com:80", nil)
		if err != nil {
			t.Fatalf("dial failed: %v", err)
		}
		conn.Close()
	}

	dialOnce() // cold
	if calls != 1 {
		t.Fatalf("first connect: validator calls=%d, want 1", calls)
	}
	if jar.Get(cfg.Pin) == "" {
		t.Fatal("client did not store the issued cookie")
	}
	dialOnce() // warm — same jar, fresh server process state
	if calls != 1 {
		t.Fatalf("COLD AUTH REPEATED: validator calls=%d after warm reconnect, want 1", calls)
	}
}
