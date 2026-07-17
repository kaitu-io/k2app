//go:build linux

package gateway

import "net/http"

// rootNoticeHTML replaces the retired embedded SPA: a single brand-neutral
// static page so legacy panel bookmarks land on guidance instead of a 404.
// Constitution: no brand words here — k2r ships under multiple brands.
const rootNoticeHTML = `<!doctype html><html><head><meta charset="utf-8">` +
	`<meta name="viewport" content="width=device-width,initial-scale=1">` +
	`<title>Router</title></head>` +
	`<body style="font-family:system-ui,sans-serif;display:flex;min-height:90vh;align-items:center;justify-content:center;text-align:center">` +
	`<div><h2>This router is managed by the companion app.</h2>` +
	`<p>&#x8BF7;&#x4F7F;&#x7528;&#x914D;&#x5957;&#x5E94;&#x7528;&#x7BA1;&#x7406;&#x6B64;&#x8DEF;&#x7531;&#x5668;&#x3002;</p></div></body></html>`

// handleRootNotice serves the notice page on exactly "/" and 404s elsewhere
// (it is the mux fallback pattern, so it sees every unmatched path).
func handleRootNotice(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(rootNoticeHTML))
}
