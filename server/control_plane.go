package server

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"
)

// matchControlPlaneRoute returns the Center origin for an ECH-decrypted inner
// SNI, if it is an exact match in control_plane_routes. Exact-match only (no
// wildcard) — the control-plane allowlist must be tight.
func (s *Server) matchControlPlaneRoute(sni string) (string, bool) {
	if sni == "" || len(s.cfg.ControlPlaneRoutes) == 0 {
		return "", false
	}
	origin, ok := s.cfg.ControlPlaneRoutes[sni]
	return origin, ok
}

// reverseProxyHTTP serves an already-TLS-terminated client connection as an
// HTTP reverse proxy to origin (Center, host:port). The client speaks plain
// HTTP over the node-terminated TLS; the node re-originates HTTPS to origin and
// validates origin's real certificate via controlPlaneTransport. Per-request
// audit log is emitted (no body/token/headers). 做法一: the node sees plaintext
// (accepted, spec D2). Returns when the client connection closes.
func (s *Server) reverseProxyHTTP(conn net.Conn, origin string) {
	target := &url.URL{Scheme: "https", Host: origin}
	// Host header must be the bare hostname (no :443) so CloudFront/ALB
	// host-header routing matches; dialing still uses target.Host (host:port).
	hostHeader := origin
	if h, _, err := net.SplitHostPort(origin); err == nil {
		hostHeader = h
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = s.controlPlaneTransport
	baseDirector := proxy.Director
	proxy.Director = func(r *http.Request) {
		baseDirector(r)
		r.Host = hostHeader // Host header = bare Center hostname (SNI from target.Host)
	}
	proxy.ModifyResponse = func(resp *http.Response) error {
		slog.Info("DIAG: control-plane-relay",
			"origin", origin,
			"method", resp.Request.Method,
			"path", resp.Request.URL.Path,
			"status", resp.StatusCode,
			"bytes", resp.ContentLength) // -1 if unknown/chunked
		return nil
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		slog.Warn("DIAG: control-plane-relay-fail", "origin", origin, "path", r.URL.Path, "err", err)
		w.WriteHeader(http.StatusBadGateway)
	}
	ln := newSingleConnListener(conn)
	srv := &http.Server{
		Handler:      proxy,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
	}
	_ = srv.Serve(ln) // returns when the single conn closes
}

// validateControlPlaneRoutes rejects malformed or unsafe control-plane routes.
// Origins must be host:port. IP-literal origins that are loopback, private,
// link-local, or unspecified are rejected to prevent a poisoned config from
// turning the node into an open proxy to internal services.
func validateControlPlaneRoutes(routes map[string]string) error {
	for sni, origin := range routes {
		if sni == "" || origin == "" {
			return fmt.Errorf("control_plane_routes: empty sni or origin (sni=%q origin=%q)", sni, origin)
		}
		host, port, err := net.SplitHostPort(origin)
		if err != nil {
			return fmt.Errorf("control_plane_routes[%s]: origin must be host:port: %w", sni, err)
		}
		if host == "" || port == "" {
			return fmt.Errorf("control_plane_routes[%s]: origin missing host or port: %q", sni, origin)
		}
		if ip := net.ParseIP(host); ip != nil {
			if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsLinkLocalMulticast() {
				return fmt.Errorf("control_plane_routes[%s]: origin IP %s not allowed (loopback/private/link-local)", sni, host)
			}
		}
	}
	return nil
}
