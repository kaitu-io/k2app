package wire

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kaitu-io/kuic"
	tls "github.com/metacubex/utls"
	smux "github.com/xtaci/smux/v2"
)

// startRevalidatingQUICServer is startRawQUICServer with the session registry
// enabled (EnableRevalidation before Listen, as server.New does), which is what
// CloseConnections walks.
func startRevalidatingQUICServer(t *testing.T, resetKey *quic.StatelessResetKey) (*QUICServer, *net.UDPAddr, func()) {
	t.Helper()
	cert := generateTestCert(t)
	serverUDP, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	tr := &quic.Transport{Conn: serverUDP, StatelessResetKey: resetKey}
	ln, err := tr.Listen(&tls.Config{Certificates: []tls.Certificate{cert}, NextProtos: []string{"h3"}}, DefaultQUICConfig())
	if err != nil {
		t.Fatal(err)
	}
	server := NewQUICServer()
	server.SetValidator(vOK())
	server.SetEnforce(true)
	server.EnableRevalidation(vOK())
	server.Listen(ln)
	return server, serverUDP.LocalAddr().(*net.UDPAddr), func() {
		server.Close()
		ln.Close()
		tr.Close()
		serverUDP.Close()
	}
}

// waitRegistered waits until the server's handleConn has registered n sessions.
func waitRegistered(t *testing.T, a *authSessions, n int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.Lock()
		got := len(a.set)
		a.mu.Unlock()
		if got >= n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("server never registered %d session(s)", n)
}

// TestQUICServer_CloseConnectionsSendsShutdownCode: on shutdown every client
// must hear CONNECTION_CLOSE 0x04 right away. Without it a client only notices
// the dead server after its echo probes fail or MaxIdleTimeout (30s) expires.
func TestQUICServer_CloseConnectionsSendsShutdownCode(t *testing.T) {
	server, addr, cleanup := startRevalidatingQUICServer(t, nil)
	defer cleanup()
	conn, connCleanup := dialRawQUIC(t, addr)
	defer connCleanup()
	waitRegistered(t, server.sessions, 1)

	if n := server.CloseConnections(); n != 1 {
		t.Fatalf("CloseConnections closed %d, want 1", n)
	}
	select {
	case <-conn.Context().Done():
	case <-time.After(2 * time.Second):
		t.Fatal("client did not see the connection close")
	}
	var appErr *quic.ApplicationError
	if !errors.As(context.Cause(conn.Context()), &appErr) {
		t.Fatalf("close cause = %v, want *quic.ApplicationError", context.Cause(conn.Context()))
	}
	if !appErr.Remote || appErr.ErrorCode != quicCloseShutdown {
		t.Fatalf("close = remote %v code %#x, want remote code %#x", appErr.Remote, appErr.ErrorCode, quicCloseShutdown)
	}
}

// TestTCPWSServer_CloseConnectionsClosesSession: the TCP-WS twin — the
// client's smux session must close at once.
func TestTCPWSServer_CloseConnectionsClosesSession(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	server := NewTCPWSServer()
	server.SetValidator(vOK())
	server.SetEnforce(true)
	server.EnableRevalidation(vOK())
	defer server.Close()
	server.HandleConn(serverConn)
	sess, err := smux.Client(clientConn, DefaultSmuxConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	waitRegistered(t, server.sessions, 1)

	if n := server.CloseConnections(); n != 1 {
		t.Fatalf("CloseConnections closed %d, want 1", n)
	}
	deadline := time.Now().Add(2 * time.Second)
	for !sess.IsClosed() {
		if time.Now().After(deadline) {
			t.Fatal("client smux session did not close")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestStatelessReset_RestartedTransportResetsOldConnection: the kill -9 path.
// The first server vanishes without a word; a new transport on the same port
// with the SAME persisted key must make the client drop the connection
// promptly, not after MaxIdleTimeout.
func TestStatelessReset_RestartedTransportResetsOldConnection(t *testing.T) {
	key, err := EnsureStatelessResetKey(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cert := generateTestCert(t)
	tlsConf := &tls.Config{Certificates: []tls.Certificate{cert}, NextProtos: []string{"h3"}}

	udp1, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	addr := udp1.LocalAddr().(*net.UDPAddr)
	tr1 := &quic.Transport{Conn: udp1, StatelessResetKey: key}
	ln1, err := tr1.Listen(tlsConf, DefaultQUICConfig())
	if err != nil {
		t.Fatal(err)
	}
	go func() { ln1.Accept(context.Background()) }()

	conn, connCleanup := dialRawQUIC(t, addr)
	defer connCleanup()

	// "SIGKILL": drop the socket; no CONNECTION_CLOSE is sent.
	udp1.Close()
	defer tr1.Close()

	udp2, err := net.ListenUDP("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	tr2 := &quic.Transport{Conn: udp2, StatelessResetKey: key}
	defer tr2.Close()
	ln2, err := tr2.Listen(tlsConf, DefaultQUICConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer ln2.Close()

	// Any client packet now reaches tr2, which knows no such connection. An
	// opened stream sends nothing until written to.
	go func() {
		if st, err := conn.OpenStreamSync(context.Background()); err == nil {
			st.Write(make([]byte, 64))
		}
	}()

	select {
	case <-conn.Context().Done():
	case <-time.After(5 * time.Second):
		t.Fatal("client kept a dead connection; stateless reset not recognised")
	}
	var reset *quic.StatelessResetError
	if !errors.As(context.Cause(conn.Context()), &reset) {
		t.Fatalf("close cause = %v, want *quic.StatelessResetError", context.Cause(conn.Context()))
	}
}

func TestEnsureStatelessResetKey_PersistsAndReplacesMalformed(t *testing.T) {
	dir := t.TempDir()
	k1, err := EnsureStatelessResetKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	k2, err := EnsureStatelessResetKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	if *k1 != *k2 {
		t.Fatal("reset key must persist across restarts")
	}
	path := filepath.Join(dir, quicResetKeyFileName)
	if err := os.WriteFile(path, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	k3, err := EnsureStatelessResetKey(dir)
	if err != nil {
		t.Fatalf("malformed key must be replaced, not refuse boot: %v", err)
	}
	if *k3 == *k1 {
		t.Fatal("malformed key should have been regenerated")
	}
}
