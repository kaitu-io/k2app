package server

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"time"

	"github.com/kaitu-io/k2/safego"
)

// pprofListenAddr is fixed to loopback on purpose. The profile, cmdline,
// symbol and trace endpoints expose heap addresses and internal state, so the
// listener must never be reachable from the network: inside the k2s container
// only `docker exec` on the same node can reach it. There is no knob to change
// the host.
const pprofListenAddr = "127.0.0.1:6060"

// pprofEnabled reports whether the opt-in profiler is requested. Strictly "1",
// the same switch the client daemon uses (daemon/daemon.go).
func pprofEnabled() bool { return os.Getenv("K2_PPROF") == "1" }

// newPProfMux wires the net/http/pprof handlers onto a private mux. Each route
// is registered by name instead of importing pprof for its side effect, which
// would hang them on http.DefaultServeMux.
func newPProfMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return mux
}

// startPProf serves pprof on addr until ctx is cancelled and returns the bound
// address, or "" when it did not start. addr must be a loopback IP; anything
// else is refused. Failure is never fatal: a diagnostics aid must not be able
// to take the tunnel down, so a refused or busy address only logs a warning.
//
// No server WriteTimeout is set on purpose: /debug/pprof/profile?seconds=N
// holds the response open for N seconds.
func startPProf(ctx context.Context, addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		slog.Warn("server: pprof disabled, bad address", "addr", addr, "err", err)
		return ""
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		slog.Warn("server: pprof refused, address is not loopback", "addr", addr)
		return ""
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		slog.Warn("server: pprof disabled, listen failed", "addr", addr, "err", err)
		return ""
	}

	srv := &http.Server{Handler: newPProfMux(), ReadHeaderTimeout: 10 * time.Second}
	safego.Go(func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Warn("server: pprof serve ended", "err", err)
		}
	})
	safego.Go(func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	})

	bound := ln.Addr().String()
	slog.Warn("server: pprof listening (K2_PPROF=1)", "addr", bound)
	return bound
}
