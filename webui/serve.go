//go:build linux

package webui

import (
	"bytes"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// NewWebappHandler returns an http.Handler that serves the embedded SPA
// with an injected `window.__K2_GATEWAY__` global exposing
// version/commit/arch. Returns nil when no webapp is embedded (nowebapp
// build tag). Kept for callers without a web OTA override (gateway).
func NewWebappHandler(version, commit, arch string) http.Handler {
	return NewWebappHandlerWithOverride(version, commit, arch, "")
}

// NewWebappHandlerWithOverride is NewWebappHandler plus an optional disk
// override directory (a web-ota "current" bundle). The override is
// consulted per request — an atomic swap of overrideDir takes effect on
// the next page load without reconstructing the handler:
//
//   - HTML requests ("/" and extension-less SPA paths) serve
//     overrideDir/index.html (with the same __K2_GATEWAY__ injection)
//     when it exists, else the embedded index.
//   - Asset requests serve the file from overrideDir when present, else
//     fall back to the embedded FS.
//   - `?ui=embedded` forces the embedded index for that document request
//     (escape hatch for a broken OTA bundle).
//
// overrideDir == "" disables the override. Returns nil when no webapp is
// embedded (nowebapp): without an embedded fallback there is no escape
// hatch, so the whole web OTA surface is a no-op on headless builds.
func NewWebappHandlerWithOverride(version, commit, arch, overrideDir string) http.Handler {
	if webappFS == nil {
		return nil
	}
	sub, err := fs.Sub(webappFS, "dist")
	if err != nil {
		return nil
	}
	embedServer := http.FileServer(http.FS(sub))

	injection := fmt.Sprintf(
		`<script>window.__K2_GATEWAY__={version:%q,commit:%q,arch:%q}</script>`,
		version, commit, arch,
	)

	indexData, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		return nil
	}
	embeddedIndex := injectGateway(indexData, injection)

	var overrideServer http.Handler
	if overrideDir != "" {
		overrideServer = http.FileServer(http.Dir(overrideDir))
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forceEmbedded := r.URL.Query().Get("ui") == "embedded"

		if r.URL.Path == "/" || !hasFileExtension(r.URL.Path) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			if !forceEmbedded {
				if data := readOverrideIndex(overrideDir); data != nil {
					w.Write(injectGateway(data, injection))
					return
				}
			}
			w.Write(embeddedIndex)
			return
		}

		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		if !forceEmbedded && overrideServer != nil && overrideHasFile(overrideDir, r.URL.Path) {
			overrideServer.ServeHTTP(w, r)
			return
		}
		embedServer.ServeHTTP(w, r)
	})
}

// injectGateway inserts the __K2_GATEWAY__ script right after <head>.
func injectGateway(index []byte, injection string) []byte {
	return bytes.Replace(index, []byte("<head>"), []byte("<head>"+injection), 1)
}

// readOverrideIndex returns the override bundle's index.html, or nil when
// the override is unset or has no readable index. Read per request so a
// web OTA swap is visible on the next page load.
func readOverrideIndex(overrideDir string) []byte {
	if overrideDir == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(overrideDir, "index.html"))
	if err != nil {
		return nil
	}
	return data
}

// overrideHasFile reports whether urlPath resolves to a regular file
// inside overrideDir. Traversal is neutralized by rooting path.Clean at
// "/"; http.Dir performs its own containment checks when serving.
func overrideHasFile(overrideDir, urlPath string) bool {
	clean := path.Clean("/" + urlPath)
	fi, err := os.Stat(filepath.Join(overrideDir, filepath.FromSlash(clean)))
	return err == nil && fi.Mode().IsRegular()
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
