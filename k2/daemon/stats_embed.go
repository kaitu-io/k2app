package daemon

import (
	_ "embed"
	"net/http"
)

//go:embed stats.html
var statsHTML []byte

func (d *Daemon) handleStatsPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(statsHTML)
}
