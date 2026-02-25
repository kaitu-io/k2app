package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// handleEvents serves the SSE endpoint for real-time status push.
// Clients receive a full status snapshot on connect and on every state change.
//
//	GET /api/events
//	Content-Type: text/event-stream
//
//	event: status
//	data: {"state":"connected","connected_at":"...","uptime_seconds":0}
func (d *Daemon) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan string, 8)

	// Subscribe FIRST, then send snapshot — any event between subscribe and
	// snapshot results in duplicate delivery, which is harmless (idempotent).
	d.sseMu.Lock()
	d.sseClients[ch] = struct{}{}
	d.sseMu.Unlock()

	defer func() {
		d.sseMu.Lock()
		delete(d.sseClients, ch)
		d.sseMu.Unlock()
	}()

	// Send initial status snapshot.
	d.mu.RLock()
	s := d.lastStatus
	d.mu.RUnlock()
	data, _ := json.Marshal(s)
	fmt.Fprintf(w, "event: status\ndata: %s\n\n", data)
	flusher.Flush()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case msg := <-ch:
			fmt.Fprint(w, msg)
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// broadcastStatus sends the current lastStatus to all connected SSE clients.
func (d *Daemon) broadcastStatus() {
	d.mu.RLock()
	s := d.lastStatus
	d.mu.RUnlock()

	data, _ := json.Marshal(s)
	msg := fmt.Sprintf("event: status\ndata: %s\n\n", data)

	d.sseMu.RLock()
	for ch := range d.sseClients {
		select {
		case ch <- msg:
		default:
			// Slow client, drop event — next event delivers full snapshot.
		}
	}
	d.sseMu.RUnlock()
}
