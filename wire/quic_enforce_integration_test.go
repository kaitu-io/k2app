package wire

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"testing"
	"time"

	"github.com/apernet/quic-go"
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
	server.SetValidator(func(jwt, udid, mode string) bool {
		return udid == "good-device" && jwt == "valid-token"
	})
	server.SetEnforce(true)
	server.authWait = 300 * time.Millisecond
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
		})
		client.tlsConfig = &tls.Config{InsecureSkipVerify: true, NextProtos: []string{"h3"}}
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
	server.SetValidator(func(jwt, udid, mode string) bool { return false }) // reject everyone
	server.SetEnforce(false)                                                // permissive
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
	})
	client.tlsConfig = &tls.Config{InsecureSkipVerify: true, NextProtos: []string{"h3"}}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := client.DialTCP(ctx, "93.184.216.34:80", nil)
	if err != nil {
		t.Fatalf("permissive mode must serve unauthenticated clients, got: %v", err)
	}
	conn.Close()
}
