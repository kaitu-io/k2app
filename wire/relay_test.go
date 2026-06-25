package wire

import (
	"io"
	"net"
	"net/http"
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
