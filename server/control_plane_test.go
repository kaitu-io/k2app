package server

import (
	"bufio"
	"io"
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
