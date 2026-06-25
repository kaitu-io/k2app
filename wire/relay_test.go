package wire

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRoundTripOverConn(t *testing.T) {
	// Fake "node+Center": a plain HTTP server on one end of a pipe.
	clientConn, serverConn := net.Pipe()
	ln := newPipeListener(serverConn)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Echo-Host", r.Host)
		io.WriteString(w, "hit:"+r.Method+":"+r.URL.Path)
	})}
	done := make(chan struct{})
	go func() { _ = srv.Serve(ln); close(done) }()

	resp, err := roundTripOverConn(clientConn, &RelayRequest{
		CenterHost: "k2.52j.me",
		Method:     "GET",
		Path:       "/api/tunnels/k2v4",
		Headers:    map[string]string{"Authorization": "Bearer x"},
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("roundTrip: %v", err)
	}
	if resp.Status != 200 || resp.Body != "hit:GET:/api/tunnels/k2v4" {
		t.Fatalf("status=%d body=%q", resp.Status, resp.Body)
	}
	if got := resp.Headers["X-Echo-Host"]; len(got) == 0 || got[0] != "k2.52j.me" {
		t.Fatalf("Host header not propagated: %v", resp.Headers)
	}
	_ = clientConn.Close()
	_ = ln.Close() // unblocks pipeListener.Accept so srv.Serve returns
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not stop (goroutine leak)")
	}
}

// newPipeListener serves exactly one net.Conn through http.Server.Serve.
func newPipeListener(c net.Conn) net.Listener { return &pipeListener{c: c, done: make(chan struct{})} }

type pipeListener struct {
	c         net.Conn
	acceptOnce sync.Once
	closeOnce  sync.Once
	done       chan struct{}
}

func (l *pipeListener) Accept() (net.Conn, error) {
	var first bool
	l.acceptOnce.Do(func() { first = true })
	if first {
		return l.c, nil
	}
	<-l.done
	return nil, net.ErrClosed
}
func (l *pipeListener) Close() error   { l.closeOnce.Do(func() { close(l.done) }); return nil }
func (l *pipeListener) Addr() net.Addr { return l.c.LocalAddr() }

func TestCaptureResponse(t *testing.T) {
	resp := &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
	}
	got, err := captureResponse(resp)
	if err != nil {
		t.Fatalf("captureResponse: %v", err)
	}
	if got.Status != 200 {
		t.Fatalf("status=%d, want 200", got.Status)
	}
	if got.Body != `{"ok":true}` {
		t.Fatalf("body=%q", got.Body)
	}
	if got.Headers["Content-Type"][0] != "application/json" {
		t.Fatalf("headers=%v", got.Headers)
	}
}

// selfSignedServerTLS builds a server tls.Config with one EC cert (no ECH —
// the override path skips BuildClientTLSConfig, exercising dial+handshake+HTTP).
func selfSignedServerTLS(t *testing.T) *tls.Config {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "node-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
}

func TestRelayFetch_OverrideDial_EndToEnd(t *testing.T) {
	// Real TCP "node" that terminates TLS and serves HTTP (stands in for a
	// Phase-1 node whose reverseProxyHTTP already relayed to Center).
	lc, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer lc.Close()
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, "centerhit:"+r.URL.Path)
		}),
		// Disable HTTP/2: our relay client sends HTTP/1.1 over the uTLS conn.
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
	}
	// Set TLSConfig BEFORE starting the goroutine to avoid the data race where
	// ServeTLS reads srv.TLSConfig concurrently with the main goroutine writing it.
	srv.TLSConfig = selfSignedServerTLS(t)
	srvDone := make(chan struct{})
	go func() { _ = srv.ServeTLS(lc, "", ""); close(srvDone) }()
	t.Cleanup(func() { _ = srv.Close(); <-srvDone })

	host, portStr, _ := net.SplitHostPort(lc.Addr().String())
	port, _ := strconv.Atoi(portStr)

	resp, err := relayFetchWithOverride(context.Background(), &RelayRequest{
		IP: host, Port: port, CenterHost: "k2.52j.me",
		Method: "GET", Path: "/api/tunnels/k2v4",
	}, &tls.Config{InsecureSkipVerify: true, ServerName: "k2.52j.me"})
	if err != nil {
		t.Fatalf("relayFetch: %v", err)
	}
	if resp.Status != 200 || resp.Body != "centerhit:/api/tunnels/k2v4" {
		t.Fatalf("status=%d body=%q", resp.Status, resp.Body)
	}
}

// Production path requires a non-empty Pin — default-CA through untrusted node is forbidden.
func TestRelayFetch_RequiresPin(t *testing.T) {
	_, err := RelayFetch(context.Background(), &RelayRequest{
		IP: "127.0.0.1", Port: 1, CenterHost: "k2.52j.me", Method: "GET", Path: "/",
	}) // Pin empty
	if err == nil || !strings.Contains(err.Error(), "pin") {
		t.Fatalf("RelayFetch with empty Pin must error about the pin, got %v", err)
	}
}

// Production path requires ECH → BuildClientTLSConfig errors without it.
// Pin is set so the new pin guard is passed and the ECH check is reached.
func TestRelayFetch_RequiresECH(t *testing.T) {
	_, err := RelayFetch(context.Background(), &RelayRequest{
		IP: "127.0.0.1", Port: 1, CenterHost: "k2.52j.me", Method: "GET", Path: "/",
		Pin: "sha256:0000000000000000000000000000000000000000000000000000000000000000",
	})
	if err == nil {
		t.Fatal("RelayFetch without ECH must error (BuildClientTLSConfig requires ECH)")
	}
}

func TestRelayFetchJSON_BadInput(t *testing.T) {
	out := RelayFetchJSON("not json")
	if !strings.Contains(out, `"code"`) || strings.Contains(out, `"code":0`) {
		t.Fatalf("bad input must return non-zero code envelope, got %q", out)
	}
}

func TestRelayFetchJSON_MissingFields(t *testing.T) {
	out := RelayFetchJSON(`{"method":"GET"}`) // no ip/centerHost → dial/build fails
	if !strings.Contains(out, `"code"`) || strings.Contains(out, `"code":0`) {
		t.Fatalf("missing fields must return error envelope, got %q", out)
	}
}
