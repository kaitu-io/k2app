package daemon

import (
	"net/http"
	"net/http/pprof"
)

// registerPProf attaches the standard net/http/pprof handlers onto the supplied
// mux. We deliberately wire each route by name instead of importing with a blank
// identifier and relying on http.DefaultServeMux — the daemon's mux is isolated,
// so registration must be explicit.
func registerPProf(mux *http.ServeMux) {
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
}
