package server

import (
	"context"
	"errors"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/kaitu-io/k2/config"
	"github.com/kaitu-io/k2/wire"
	"github.com/kaitu-io/kuic"
	tls "github.com/metacubex/utls"
)

// freeTCPUDPPort returns a port free on both TCP and UDP loopback, since Run
// binds one cfg.Listen address for both.
func freeTCPUDPPort(t *testing.T) int {
	t.Helper()
	for range 20 {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := ln.Addr().(*net.TCPAddr).Port
		ln.Close()
		u, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
		if err == nil {
			u.Close()
			return port
		}
	}
	t.Fatal("no port free on both TCP and UDP")
	return 0
}

// TestRun_ShutdownClosesClientsAndReturnsPromptly: on SIGTERM (ctx cancel) a
// connected QUIC client must hear CONNECTION_CLOSE 0x04 straight away, and Run
// must return on its own well inside docker's `-t 2` stop timeout. Before this,
// tcpListenLoop sat in Accept on a listener only closed after wg.Wait, so every
// stop ended in SIGKILL and clients were told nothing.
func TestRun_ShutdownClosesClientsAndReturnsPromptly(t *testing.T) {
	port := freeTCPUDPPort(t)
	srv, err := New(&config.ServerConfig{
		Listen:  "127.0.0.1:" + strconv.Itoa(port),
		CertDir: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := make(chan error, 1)
	go func() { runDone <- srv.Run(ctx) }()

	var conn *quic.Conn
	deadline := time.Now().Add(5 * time.Second)
	for {
		dctx, dcancel := context.WithTimeout(context.Background(), time.Second)
		conn, err = quic.DialAddr(dctx, "127.0.0.1:"+strconv.Itoa(port), &tls.Config{
			InsecureSkipVerify: true,
			NextProtos:         []string{"h3"},
		}, wire.DefaultQUICConfig())
		dcancel()
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("dial k2s: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	defer conn.CloseWithError(0, "")
	time.Sleep(200 * time.Millisecond) // let handleConn register the session

	start := time.Now()
	cancel()

	select {
	case <-conn.Context().Done():
	case <-time.After(2 * time.Second):
		t.Fatal("client was not told the server is shutting down")
	}
	var appErr *quic.ApplicationError
	if !errors.As(context.Cause(conn.Context()), &appErr) || !appErr.Remote || appErr.ErrorCode != 0x04 {
		t.Fatalf("close cause = %v, want remote application close 0x04", context.Cause(conn.Context()))
	}

	select {
	case <-runDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s of cancel; docker would SIGKILL it")
	}
	t.Logf("shutdown took %v", time.Since(start))
}
