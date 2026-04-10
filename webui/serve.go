//go:build linux

package webui

import (
	"bytes"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
)

// NewWebappHandler returns an http.Handler that serves the embedded SPA with
// an injected `window.__K2_GATEWAY__` global exposing version/commit/arch.
// Returns nil when no webapp is embedded (nowebapp build tag).
//
// The handler serves the injected index.html for "/" and any extension-less
// path (SPA fallback), and the raw file from the embedded FS for paths with
// a file extension.
func NewWebappHandler(version, commit, arch string) http.Handler {
	if webappFS == nil {
		return nil
	}
	sub, err := fs.Sub(webappFS, "dist")
	if err != nil {
		return nil
	}
	fileServer := http.FileServer(http.FS(sub))

	injection := fmt.Sprintf(
		`<script>window.__K2_GATEWAY__={version:%q,commit:%q,arch:%q}</script>`,
		version, commit, arch,
	)

	indexData, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		return nil
	}
	patched := bytes.Replace(indexData, []byte("<head>"), []byte("<head>"+injection), 1)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/" || !hasFileExtension(path) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			w.Write(patched)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		fileServer.ServeHTTP(w, r)
	})
}

// hasFileExtension reports whether the last path segment contains a dot,
// which we interpret as "has a file extension" for SPA routing.
func hasFileExtension(path string) bool {
	lastSlash := strings.LastIndex(path, "/")
	if lastSlash >= 0 {
		path = path[lastSlash:]
	}
	return strings.Contains(path, ".")
}
