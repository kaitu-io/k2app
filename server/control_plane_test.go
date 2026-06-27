package server

import (
	"bufio"
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
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/kaitu-io/k2/config"
)

func TestValidateControlPlaneRoutes(t *testing.T) {
	cases := []struct {
		name    string
		routes  map[string]string
		wantErr bool
	}{
		{"empty map ok", map[string]string{}, false},
		{"valid public origin", map[string]string{"k2.52j.me": "k2.52j.me:443"}, false},
		{"empty sni", map[string]string{"": "k2.52j.me:443"}, true},
		{"empty origin", map[string]string{"k2.52j.me": ""}, true},
		{"missing port", map[string]string{"k2.52j.me": "k2.52j.me"}, true},
		{"loopback origin rejected", map[string]string{"k2.52j.me": "127.0.0.1:443"}, true},
		{"private origin rejected", map[string]string{"k2.52j.me": "10.0.0.5:443"}, true},
		{"unspecified origin rejected", map[string]string{"k2.52j.me": "0.0.0.0:443"}, true},
		{"link-local unicast rejected", map[string]string{"k2.52j.me": "169.254.0.1:443"}, true},
		{"link-local multicast rejected", map[string]string{"k2.52j.me": "224.0.0.1:443"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateControlPlaneRoutes(c.routes)
			if (err != nil) != c.wantErr {
				t.Fatalf("validateControlPlaneRoutes(%v) err=%v, wantErr=%v", c.routes, err, c.wantErr)
			}
		})
	}
}

func TestMatchControlPlaneRoute(t *testing.T) {
	s := &Server{cfg: &config.ServerConfig{
		ControlPlaneRoutes: map[string]string{"k2.52j.me": "k2.52j.me:443"},
	}}
	if origin, ok := s.matchControlPlaneRoute("k2.52j.me"); !ok || origin != "k2.52j.me:443" {
		t.Fatalf("exact match: got (%q,%v), want (k2.52j.me:443,true)", origin, ok)
	}
	if _, ok := s.matchControlPlaneRoute("evil.example.com"); ok {
		t.Fatal("non-listed inner SNI must not match")
	}
	if _, ok := s.matchControlPlaneRoute(""); ok {
		t.Fatal("empty inner SNI must not match")
	}
	empty := &Server{cfg: &config.ServerConfig{}}
	if _, ok := empty.matchControlPlaneRoute("k2.52j.me"); ok {
		t.Fatal("nil ControlPlaneRoutes must not match")
	}
}

func TestReverseProxyHTTP(t *testing.T) {
	// Stub "Center": echoes path + a marker so we can assert end-to-end relay.
	center := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-From-Center", "1")
		io.WriteString(w, "centerhit:"+r.URL.Path)
	}))
	defer center.Close()
	originURL, _ := url.Parse(center.URL) // https://127.0.0.1:PORT
	origin := originURL.Host

	s := &Server{
		cfg:                   &config.ServerConfig{},
		controlPlaneTransport: center.Client().Transport, // trusts the stub's cert
	}
	// goleak: drain keep-alive idle conns this transport opened to the stub.
	t.Cleanup(func() {
		if tr, ok := s.controlPlaneTransport.(*http.Transport); ok {
			tr.CloseIdleConnections()
		}
	})

	// net.Pipe: clientSide is what the (already-TLS-terminated) client speaks
	// plain HTTP over; serverSide is handed to reverseProxyHTTP.
	clientSide, serverSide := net.Pipe()
	done := make(chan struct{})
	go func() { s.reverseProxyHTTP(serverSide, origin); close(done) }() // goleak: joined below

	// Speak a single plain-HTTP request over the pipe.
	req, _ := http.NewRequest("GET", "http://"+origin+"/api/app/config", nil)
	if err := req.Write(clientSide); err != nil {
		t.Fatalf("write req: %v", err)
	}
	clientSide.SetReadDeadline(time.Now().Add(5 * time.Second))
	resp, err := http.ReadResponse(bufio.NewReader(clientSide), req)
	if err != nil {
		t.Fatalf("read resp: %v", err)
	}
	if resp.Header.Get("X-From-Center") != "1" {
		t.Fatalf("response did not come from Center stub: headers=%v", resp.Header)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "centerhit:/api/app/config" {
		t.Fatalf("body=%q, want centerhit:/api/app/config", string(body))
	}
	_ = clientSide.Close() // closing the pipe end makes reverseProxyHTTP return
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("reverseProxyHTTP did not return after client close (goroutine leak)")
	}
}

// selfSignedTLSConfig builds a minimal server tls.Config with one EC cert.
func selfSignedTLSConfig(t *testing.T) *tls.Config {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "k2s-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("cert: %v", err)
	}
	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
}

func TestHandleK2V5_ControlPlaneBranch(t *testing.T) {
	center := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "centerhit:"+r.URL.Path)
	}))
	defer center.Close()
	originURL, _ := url.Parse(center.URL)

	s := &Server{
		cfg: &config.ServerConfig{
			ControlPlaneRoutes: map[string]string{"cp.test": originURL.Host},
		},
		controlPlaneTransport: center.Client().Transport,
		proxySem:              make(chan struct{}, 8),
		TLSConfig:             selfSignedTLSConfig(t),
	}
	t.Cleanup(func() {
		if tr, ok := s.controlPlaneTransport.(*http.Transport); ok {
			tr.CloseIdleConnections() // goleak: drain node→Center idle conns
		}
	})

	clientRaw, serverRaw := net.Pipe()
	done := make(chan struct{})
	go func() { s.handleK2V5(serverRaw, "cloudflare-ech.com"); close(done) }() // goleak: joined below

	// Client side: TLS with inner-routing SNI = cp.test, then plain HTTP.
	tlsClient := tls.Client(clientRaw, &tls.Config{ServerName: "cp.test", InsecureSkipVerify: true})
	if err := tlsClient.Handshake(); err != nil {
		t.Fatalf("client handshake: %v", err)
	}
	req, _ := http.NewRequest("GET", "https://cp.test/api/tunnels/k2v4", nil)
	if err := req.Write(tlsClient); err != nil {
		t.Fatalf("write: %v", err)
	}
	tlsClient.SetReadDeadline(time.Now().Add(5 * time.Second))
	resp, err := http.ReadResponse(bufio.NewReader(tlsClient), req)
	if err != nil {
		t.Fatalf("read resp: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "centerhit:/api/tunnels/k2v4" {
		t.Fatalf("body=%q, want centerhit:/api/tunnels/k2v4", string(body))
	}
	tlsClient.Close() // makes handleK2V5's relay return
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handleK2V5 did not return after client close (goroutine leak)")
	}
}

func TestNew_RejectsUnsafeControlPlaneRoute(t *testing.T) {
	_, err := New(&config.ServerConfig{
		ControlPlaneRoutes: map[string]string{"k2.52j.me": "169.254.169.254:80"},
	})
	if err == nil {
		t.Fatal("New must reject a private/link-local control-plane origin (open-proxy gate)")
	}
}

func TestHandleK2V5_NonMatchingInnerSNIFallsThrough(t *testing.T) {
	hit := make(chan struct{}, 1)
	center := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit <- struct{}{}
	}))
	defer center.Close()
	originURL, _ := url.Parse(center.URL)

	// tcpwsSrv is intentionally nil: the fall-through path (s.tcpwsSrv.HandleConn)
	// does not deref the receiver before spawning its own recover()-guarded
	// goroutine, so a nil tcpwsSrv cannot crash the process here. We only assert
	// Center is never hit. (Verified against wire/tcpws.go:627 HandleConn.)
	s := &Server{
		cfg: &config.ServerConfig{
			ControlPlaneRoutes: map[string]string{"cp.test": originURL.Host},
		},
		controlPlaneTransport: center.Client().Transport,
		proxySem:              make(chan struct{}, 8),
		TLSConfig:             selfSignedTLSConfig(t),
	}
	t.Cleanup(func() {
		if tr, ok := s.controlPlaneTransport.(*http.Transport); ok {
			tr.CloseIdleConnections()
		}
	})

	clientRaw, serverRaw := net.Pipe()
	done := make(chan struct{})
	go func() { s.handleK2V5(serverRaw, "cloudflare-ech.com"); close(done) }()

	// Inner SNI NOT in control_plane_routes → must NOT relay to Center.
	tlsClient := tls.Client(clientRaw, &tls.Config{ServerName: "not-control.test", InsecureSkipVerify: true})
	_ = tlsClient.Handshake()
	// Send a tiny smux-ish blob; we only assert Center was never hit.
	tlsClient.SetWriteDeadline(time.Now().Add(time.Second))
	_, _ = tlsClient.Write([]byte{0x00, 0x01, 0x02, 0x03})
	tlsClient.Close()

	select {
	case <-hit:
		t.Fatal("non-matching inner SNI was wrongly relayed to Center")
	case <-time.After(500 * time.Millisecond):
		// good: fell through to existing k2v5 dispatch, Center untouched
	}
	select { // goleak: handleK2V5 returns once the closed pipe propagates
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handleK2V5 did not return after client close (goroutine leak)")
	}
}

// New must bake in the single permanent control-plane route when no explicit
// control_plane_routes config is present, so a node needs zero per-node config.
func TestNew_DefaultsControlPlaneRouteWhenUnset(t *testing.T) {
	s, err := New(&config.ServerConfig{}) // no CertDir → no provisioning; no routes
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	origin, ok := s.matchControlPlaneRoute("k2.52j.me")
	if !ok || origin != "k2.52j.me:443" {
		t.Fatalf("built-in default route: got (%q,%v), want (k2.52j.me:443,true)", origin, ok)
	}
}

// An explicit control_plane_routes config replaces (does not merge with) the
// built-in default.
func TestNew_ExplicitControlPlaneRouteOverridesDefault(t *testing.T) {
	s, err := New(&config.ServerConfig{
		ControlPlaneRoutes: map[string]string{"cp.test": "cp.test:443"},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if origin, ok := s.matchControlPlaneRoute("cp.test"); !ok || origin != "cp.test:443" {
		t.Fatalf("explicit route: got (%q,%v), want (cp.test:443,true)", origin, ok)
	}
	if _, ok := s.matchControlPlaneRoute("k2.52j.me"); ok {
		t.Fatal("explicit config must replace, not merge with, the built-in default")
	}
}
