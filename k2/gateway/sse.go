//go:build linux

package gateway

import (
	"fmt"
	"net/http"
)

func (g *Gateway) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := make(chan []byte, 8)
	g.sseMu.Lock()
	g.sseClients[ch] = struct{}{}
	g.sseMu.Unlock()

	defer func() {
		g.sseMu.Lock()
		delete(g.sseClients, ch)
		g.sseMu.Unlock()
	}()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case data := <-ch:
			fmt.Fprintf(w, "%s", data)
			flusher.Flush()
		}
	}
}

func (g *Gateway) broadcastSSE(event string, data []byte) {
	msg := []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", event, data))
	g.sseMu.RLock()
	defer g.sseMu.RUnlock()
	for ch := range g.sseClients {
		select {
		case ch <- msg:
		default:
		}
	}
}
