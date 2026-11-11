package server

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestPProfEnabled_StrictlyOne(t *testing.T) {
	for _, tc := range []struct {
		val  string
		want bool
	}{
		{"", false},
		{"0", false},
		{"true", false}, // only "1" counts, same as the client daemon
		{"1", true},
	} {
		t.Setenv("K2_PPROF", tc.val)
		if got := pprofEnabled(); got != tc.want {
			t.Errorf("K2_PPROF=%q: pprofEnabled()=%v, want %v", tc.val, got, tc.want)
		}
	}
}

func TestPProfListenAddr_IsLoopback(t *testing.T) {
	// The production constant is the security boundary: pin it.
	host, _, err := net.SplitHostPort(pprofListenAddr)
	if err != nil {
		t.Fatal(err)
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		t.Fatalf("pprofListenAddr %q is not a loopback IP", pprofListenAddr)
	}
}

func TestStartPProf_RefusesNonLoopback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, addr := range []string{"0.0.0.0:0", ":0", "192.0.2.1:6060", "localhost-not-an-ip:6060", "no-port"} {
		if got := startPProf(ctx, addr); got != "" {
			t.Errorf("startPProf(%q) bound %q, want refusal", addr, got)
		}
	}
}

func TestStartPProf_ServesOnLoopbackAndStopsWithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	bound := startPProf(ctx, "127.0.0.1:0")
	if bound == "" {
		t.Fatal("startPProf returned no address for a loopback listener")
	}
	host, _, _ := net.SplitHostPort(bound)
	if !net.ParseIP(host).IsLoopback() {
		t.Fatalf("bound to non-loopback %q", bound)
	}

	for _, path := range []string{"/debug/pprof/", "/debug/pprof/cmdline", "/debug/pprof/goroutine?debug=1"} {
		resp, err := http.Get("http://" + bound + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("GET %s: status %d, want 200", path, resp.StatusCode)
		}
	}

	// A short CPU profile must complete: this is what we actually collect, and
	// it fails if a server WriteTimeout is ever added.
	resp, err := http.Get("http://" + bound + "/debug/pprof/profile?seconds=1")
	if err != nil {
		t.Fatalf("GET profile: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || len(body) == 0 {
		t.Fatalf("profile: status %d, %d bytes", resp.StatusCode, len(body))
	}

	cancel()
	deadline := time.Now().Add(3 * time.Second)
	for {
		c, err := net.DialTimeout("tcp", bound, 200*time.Millisecond)
		if err != nil {
			return // listener closed
		}
		c.Close()
		if time.Now().After(deadline) {
			t.Fatal("pprof listener still accepting after ctx cancel")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestStartPProf_BusyAddressIsNotFatal(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if got := startPProf(ctx, ln.Addr().String()); got != "" {
		t.Fatalf("expected no bind on a busy address, got %q", got)
	}
}
