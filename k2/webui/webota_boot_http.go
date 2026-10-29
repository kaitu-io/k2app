//go:build linux

package webui

import "net/http"

// NewUIBootOkHandler returns the /api/ui-boot-ok handler: the webapp's
// confirmation that it actually RENDERED, which clears the web OTA boot
// marker.
//
// The webapp must call this only after ReactDOM has rendered — never from
// bridge/platform init. Confirming during init is what made this whole
// mechanism a no-op on the mobile shells: a bundle that loaded its JS and
// then died before painting still reported success, so the rollback never
// fired and the user was served the same white screen on every load.
//
// POST only. A GET would let a link preview, a crawler, or a browser
// prefetch clear the marker on behalf of a page nobody ever rendered.
func NewUIBootOkHandler(o *WebOTA) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		o.ConfirmBootOK()
		writeJSON(w, response{Code: 0, Message: "ok"})
	}
}
