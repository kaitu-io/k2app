package wire

import (
	"context"
	tls "github.com/metacubex/utls"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kaitu-io/kuic"
)

// TestIntegration_QUICEnforce_ForgedTokenRejected is the end-to-end regression
// for the P0: a normal client (Mode="") with a forged token must NOT be able to
// proxy through a node running enforce=on, while a client with a valid token
// must. Before this fix the QUIC path never authenticated normal clients at all
// — a forged udid/token proxied straight through (verified against a live node).
func TestIntegration_QUICEnforce_ForgedTokenRejected(t *testing.T) {
	cert := generateTestCert(t)

	serverUDP, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer serverUDP.Close()
	serverTransport := &quic.Transport{Conn: serverUDP}
	defer serverTransport.Close()

	ln, err := serverTransport.Listen(&tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"h3"},
	}, DefaultQUICConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	server := NewQUICServer()
	server.SetValidator(func(token, udid, mode string) AuthVerdict {
		if udid == "good-device" && token == "valid-token" {
			return AuthVerdict{OK: true}
		}
		return AuthVerdict{Code: 401}
	})
	server.SetEnforce(true)
	server.adm.authWait = 300 * time.Millisecond
	server.Listen(ln)
	defer server.Close()

	// Consume authorized TCP proxy streams: reply "established" then echo. This
	// stands in for the server-package layer that dials the target and writes
	// the stream response. Only reached when proxyAuthorized() admits the stream.
	go func() {
		for {
			select {
			case <-server.done:
				return
			case res := <-server.tcpCh:
				if res.err != nil {
					return
				}
				c := res.conn
				WriteStreamResponse(c, nil)
				go func() { io.Copy(c, c); c.Close() }()
			}
		}
	}()

	serverAddr := serverUDP.LocalAddr().(*net.UDPAddr)
	dialTCP := func(udid, token string) error {
		client := NewQUICClient(Config{
			Host:  "127.0.0.1",
			Port:  serverAddr.Port,
			UDID:  udid,
			Token: token,
		}, nil)
		client.tlsConfig = buildInsecureTLSConfig()
		defer client.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		conn, err := client.DialTCP(ctx, "93.184.216.34:80", nil)
		if conn != nil {
			conn.Close()
		}
		return err
	}

	t.Run("forged token is rejected under enforce", func(t *testing.T) {
		if err := dialTCP("attacker-device", "forged-token"); err == nil {
			t.Fatal("SECURITY REGRESSION: forged token proxied through an enforce=on node")
		}
	})

	t.Run("valid token is admitted under enforce", func(t *testing.T) {
		if err := dialTCP("good-device", "valid-token"); err != nil {
			t.Fatalf("valid client wrongly rejected under enforce: %v", err)
		}
	})
}

// TestIntegration_QUICEnforce_PermissiveServesUnauthenticated proves the rollout
// guarantee: with enforce=off a client whose auth fails is still served (the
// metadata is used only for attribution), so upgrading the fix cannot cut off
// legacy clients before the flag is flipped.
func TestIntegration_QUICEnforce_PermissiveServesUnauthenticated(t *testing.T) {
	cert := generateTestCert(t)

	serverUDP, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer serverUDP.Close()
	serverTransport := &quic.Transport{Conn: serverUDP}
	defer serverTransport.Close()

	ln, err := serverTransport.Listen(&tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"h3"},
	}, DefaultQUICConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	server := NewQUICServer()
	server.SetValidator(func(token, udid, mode string) AuthVerdict { return AuthVerdict{Code: 401} }) // reject everyone
	server.SetEnforce(false)                                                                          // permissive
	server.Listen(ln)
	defer server.Close()

	go func() {
		for {
			select {
			case <-server.done:
				return
			case res := <-server.tcpCh:
				if res.err != nil {
					return
				}
				c := res.conn
				WriteStreamResponse(c, nil)
				go func() { io.Copy(c, c); c.Close() }()
			}
		}
	}()

	serverAddr := serverUDP.LocalAddr().(*net.UDPAddr)
	client := NewQUICClient(Config{
		Host: "127.0.0.1", Port: serverAddr.Port, UDID: "legacy", Token: "whatever",
	}, nil)
	client.tlsConfig = buildInsecureTLSConfig()
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := client.DialTCP(ctx, "93.184.216.34:80", nil)
	if err != nil {
		t.Fatalf("permissive mode must serve unauthenticated clients, got: %v", err)
	}
	conn.Close()
}

// TestIntegration_QUICCookie_SecondConnectIsWarm is the QUIC twin of
// TestTCPWSCookie_SecondConnectIsWarm: cold auth issues a cookie; a second
// client sharing the jar must be admitted WITHOUT another validator call.
// Two clients (not one reused connection) — this is the RaceTransport shape:
// independent dialers sharing one per-outbound jar.
func TestIntegration_QUICCookie_SecondConnectIsWarm(t *testing.T) {
	cert := generateTestCert(t)

	serverUDP, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer serverUDP.Close()
	serverTransport := &quic.Transport{Conn: serverUDP}
	defer serverTransport.Close()

	ln, err := serverTransport.Listen(&tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"h3"},
	}, DefaultQUICConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	signer, err := NewCookieSigner(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	server := NewQUICServer()
	server.SetValidator(func(token, udid, mode string) AuthVerdict {
		calls++
		return AuthVerdict{OK: true, UserID: 1, SvcExp: time.Now().Add(time.Hour).Unix()}
	})
	server.SetEnforce(true)
	server.SetCookieSigner(signer)
	server.adm.authWait = 300 * time.Millisecond
	server.Listen(ln)
	defer server.Close()

	go func() {
		for {
			select {
			case <-server.done:
				return
			case res := <-server.tcpCh:
				if res.err != nil {
					return
				}
				c := res.conn
				WriteStreamResponse(c, nil)
				go func() { io.Copy(c, c); c.Close() }()
			}
		}
	}()

	jar := NewCookieJar()
	serverAddr := serverUDP.LocalAddr().(*net.UDPAddr)
	cfg := Config{Host: "127.0.0.1", Port: serverAddr.Port, UDID: "good", Token: "valid"}

	dial := func() {
		t.Helper()
		client := NewQUICClient(cfg, jar)
		client.tlsConfig = buildInsecureTLSConfig()
		defer client.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		conn, err := client.DialTCP(ctx, "93.184.216.34:80", nil)
		if err != nil {
			t.Fatalf("dial failed: %v", err)
		}
		conn.Close()
	}

	dial() // cold
	if calls != 1 {
		t.Fatalf("first connect: validator calls=%d, want 1", calls)
	}
	if jar.Get(cfg.Pin) == "" {
		t.Fatal("client did not store the issued cookie")
	}
	dial() // warm — different client, same jar
	if calls != 1 {
		t.Fatalf("COLD AUTH REPEATED: validator calls=%d after warm reconnect, want 1", calls)
	}
}

// TestIntegration_QUICSweep_KillSeversConn is the QUIC twin of
// TestTCPWSServer_SweepKillSeversSession. It exercises the REAL QUIC wiring
// that the fixture-based unit sweep tests never touch: handleConn's kill
// closure (conn.CloseWithError(0x03,…)) and handleMetadataStream's markAuthed
// publish. Reuses this file's real-listener harness.
func TestIntegration_QUICSweep_KillSeversConn(t *testing.T) {
	cert := generateTestCert(t)

	serverUDP, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer serverUDP.Close()
	serverTransport := &quic.Transport{Conn: serverUDP}
	defer serverTransport.Close()

	ln, err := serverTransport.Listen(&tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"h3"},
	}, DefaultQUICConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	verdict := atomic.Value{}
	verdict.Store(AuthVerdict{OK: true, UserID: 1, SvcExp: time.Now().Add(time.Hour).Unix()})
	validator := func(token, udid, mode string) AuthVerdict { return verdict.Load().(AuthVerdict) }

	server := NewQUICServer()
	server.SetValidator(validator)
	server.SetEnforce(true)
	server.EnableRevalidation(validator)
	server.adm.authWait = 300 * time.Millisecond
	server.Listen(ln)
	defer server.Close()

	go func() {
		for {
			select {
			case <-server.done:
				return
			case res := <-server.tcpCh:
				if res.err != nil {
					return
				}
				c := res.conn
				WriteStreamResponse(c, nil)
				go io.Copy(io.Discard, c)
			}
		}
	}()

	serverAddr := serverUDP.LocalAddr().(*net.UDPAddr)
	client := NewQUICClient(Config{Host: "127.0.0.1", Port: serverAddr.Port, UDID: "good", Token: "valid"}, nil)
	client.tlsConfig = buildInsecureTLSConfig()
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := client.DialTCP(ctx, "93.184.216.34:80", nil)
	if err != nil {
		t.Fatalf("initial dial: %v", err)
	}
	defer conn.Close()

	// Flip Center to "revoked", age the session past the window, sweep. The
	// kill closure must call conn.CloseWithError(0x03) on the live QUIC conn.
	verdict.Store(AuthVerdict{OK: false, Code: 402})
	server.sessions.now = func() time.Time { return time.Now().Add(cookieTTL + time.Minute) }
	server.sessions.sweepOnce()

	// A plain read timeout is NOT proof the kill closure fired — the target
	// echo goroutine never writes back either way, so a mutated no-op kill()
	// would leave the read blocked until the deadline and still return a
	// non-nil error, silently passing a bare err==nil check. Require the
	// error to be the QUIC application-error close, not a timeout.
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1)
	start := time.Now()
	_, err = conn.Read(buf)
	if err == nil {
		t.Fatal("REVOCATION LEAK: QUIC proxy stream still alive after sweeper kill")
	}
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		t.Fatalf("REVOCATION LEAK: read only failed via deadline timeout (%v elapsed) — sweeper kill() did not call conn.CloseWithError, a stalled stream just happened to also error", time.Since(start))
	}
}
