package wire

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	tls "github.com/metacubex/utls"
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
	return &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
		// Disable HTTP/2: the relay client speaks HTTP/1.1 over the uTLS conn.
		// Negotiating it here rather than via http.Server.TLSNextProto, which
		// net/http hard-types to *crypto/tls.Conn.
		NextProtos: []string{"http/1.1"},
	}
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
	}
	srvDone := make(chan struct{})
	go func() { _ = srv.Serve(tls.NewListener(lc, selfSignedServerTLS(t))); close(srvDone) }()
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

// --- antiblock relay contract tests -----------------------------------------
// Crystallize the AU smoke (ECH→node→Center 200) into in-process CI regressions
// for the contracts the webapp transport layer (resolve-and-fetch.ts) relies on
// but that the 200-only OverrideDial e2e never exercised. All reuse the
// self-signed override path (dial+handshake+HTTP/1.1, ECH/pin skipped).

// startTLSNode serves handler over a self-signed TLS listener and returns its
// dial host/port. Cleanup registered on t. Mirrors the harness in
// TestRelayFetch_OverrideDial_EndToEnd (HTTP/1.1 only — no h2).
func startTLSNode(t *testing.T, handler http.HandlerFunc) (string, int) {
	t.Helper()
	lc, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: handler}
	srvDone := make(chan struct{})
	go func() { _ = srv.Serve(tls.NewListener(lc, selfSignedServerTLS(t))); close(srvDone) }()
	t.Cleanup(func() { _ = srv.Close(); <-srvDone })
	host, portStr, _ := net.SplitHostPort(lc.Addr().String())
	port, _ := strconv.Atoi(portStr)
	return host, port
}

// nodeOverrideTLS is the client-side test override (self-signed node, skips ECH/pin).
func nodeOverrideTLS() *tls.Config {
	return &tls.Config{InsecureSkipVerify: true, ServerName: "k2.52j.me"}
}

// A non-2xx response from Center MUST come back as a normal RelayResponse (no
// error) so the webapp returns the status verbatim and cloud-api owns the
// 401→refresh decision. If RelayFetch ever treated non-2xx as a transport
// failure, 401-over-relay would trigger node-failover instead of token refresh.
func TestRelayFetch_NonOKStatusPassthrough(t *testing.T) {
	for _, code := range []int{401, 500} {
		host, port := startTLSNode(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(code)
			io.WriteString(w, "body-for-"+strconv.Itoa(code))
		})
		resp, err := relayFetchWithOverride(context.Background(), &RelayRequest{
			IP: host, Port: port, CenterHost: "k2.52j.me", Method: "GET", Path: "/api/x",
		}, nodeOverrideTLS())
		if err != nil {
			t.Fatalf("status %d: relayFetch errored (must pass non-2xx through, not error): %v", code, err)
		}
		if resp.Status != code {
			t.Fatalf("status passthrough: got %d want %d", resp.Status, code)
		}
		if resp.Body != "body-for-"+strconv.Itoa(code) {
			t.Fatalf("status %d: body=%q", code, resp.Body)
		}
	}
}

// RelayResponse.Headers is map[string][]string specifically so multi-valued
// headers (Set-Cookie) survive losslessly. Guard against a "simplification" to
// map[string]string that would silently drop cookies.
func TestRelayFetch_MultiValuedHeadersLossless(t *testing.T) {
	host, port := startTLSNode(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "a=1")
		w.Header().Add("Set-Cookie", "b=2")
		w.WriteHeader(200)
	})
	resp, err := relayFetchWithOverride(context.Background(), &RelayRequest{
		IP: host, Port: port, CenterHost: "k2.52j.me", Method: "GET", Path: "/",
	}, nodeOverrideTLS())
	if err != nil {
		t.Fatalf("relayFetch: %v", err)
	}
	got := resp.Headers["Set-Cookie"]
	if len(got) != 2 || got[0] != "a=1" || got[1] != "b=2" {
		t.Fatalf("Set-Cookie not lossless: %v", got)
	}
}

// Token refresh relays as a POST with a body + Authorization header. Verify
// method, body, and custom request headers reach the node verbatim (the 200-only
// e2e used GET with no body). Everything is observed via the response to stay
// race-free under -race.
func TestRelayFetch_PostBodyAndHeadersPropagated(t *testing.T) {
	host, port := startTLSNode(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		io.WriteString(w, r.Method+"|"+r.Header.Get("Authorization")+"|"+string(b))
	})
	resp, err := relayFetchWithOverride(context.Background(), &RelayRequest{
		IP: host, Port: port, CenterHost: "k2.52j.me",
		Method:  "POST",
		Path:    "/api/token/refresh",
		Headers: map[string]string{"Authorization": "Bearer refresh-tok"},
		Body:    `{"refresh":"r"}`,
	}, nodeOverrideTLS())
	if err != nil {
		t.Fatalf("relayFetch: %v", err)
	}
	if want := `POST|Bearer refresh-tok|{"refresh":"r"}`; resp.Body != want {
		t.Fatalf("propagation: body=%q want %q", resp.Body, want)
	}
}

// A node that completes the TLS handshake but dies mid-round-trip (closes
// without a valid HTTP response) must surface as an error — the webapp records a
// node failure and fails over. All prior failure tests fail pre-dial.
func TestRelayFetch_TransportFailsAfterTLS(t *testing.T) {
	host, port := startTLSNode(t, func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Errorf("ResponseWriter is not a Hijacker")
			return
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			return
		}
		_ = conn.Close() // close without writing any response → client ReadResponse fails
	})
	_, err := relayFetchWithOverride(context.Background(), &RelayRequest{
		IP: host, Port: port, CenterHost: "k2.52j.me", Method: "GET", Path: "/",
	}, nodeOverrideTLS())
	if err == nil {
		t.Fatal("node that dies mid-round-trip must surface as an error (webapp records node failure)")
	}
}
