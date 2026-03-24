package signal

import (
	"log/slog"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/kaitu-io/k2/safego"
	"github.com/sasha-s/go-deadlock"
)

type relayPair struct {
	home   *websocket.Conn
	away   *websocket.Conn
	paired chan struct{} // closed when both sides are connected
	done   chan struct{} // closed when the relay session ends
}

// Relay manages WebSocket relay pairs for k2p hole-punch fallback.
// When both home and away connect with the same UDID, messages are
// forwarded bidirectionally (binary frames only).
type Relay struct {
	mu       deadlock.Mutex
	pairs    map[string]*relayPair
	upgrader websocket.Upgrader
}

// NewRelay creates a Relay ready to accept connections.
func NewRelay() *Relay {
	return &Relay{
		pairs: make(map[string]*relayPair),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

// HandleRelay handles GET /k2p/relay?udid=...&role=home|away.
// Upgrades to WebSocket, stores the connection, and when both sides are
// present starts bidirectional binary forwarding.
//
// Only the second connector (the one that completes the pair) starts
// forwarding. The first connector waits on the paired channel, then
// blocks until the session ends via the done channel.
func (rl *Relay) HandleRelay(w http.ResponseWriter, r *http.Request) {
	udid := r.URL.Query().Get("udid")
	role := r.URL.Query().Get("role")

	if udid == "" || (role != "home" && role != "away") {
		http.Error(w, "bad request: udid and role=home|away required", http.StatusBadRequest)
		return
	}

	conn, err := rl.upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Warn("DIAG: relay-upgrade-fail", "udid", udid, "role", role, "err", err)
		return
	}

	rl.mu.Lock()
	pair, ok := rl.pairs[udid]
	if !ok {
		pair = &relayPair{
			paired: make(chan struct{}),
			done:   make(chan struct{}),
		}
		rl.pairs[udid] = pair
	}
	if role == "home" {
		pair.home = conn
	} else {
		pair.away = conn
	}
	bothReady := pair.home != nil && pair.away != nil
	if bothReady {
		close(pair.paired)
	}
	pairedCh := pair.paired
	doneCh := pair.done
	rl.mu.Unlock()

	if !bothReady {
		// First connector: wait until paired, then wait until done.
		// Do NOT read from conn here — reads will be handled by forwardBinary
		// once the pair is established.
		<-pairedCh
		<-doneCh
		return
	}

	// Second connector: retrieve both conns and start bidirectional forwarding.
	rl.mu.Lock()
	home := pair.home
	away := pair.away
	rl.mu.Unlock()

	cleanup := func() { rl.cleanup(udid) }

	safego.Go(func() {
		forwardBinary(udid, home, away, cleanup) // home→away
	})
	forwardBinary(udid, away, home, cleanup) // away→home (blocks)
}

// cleanup closes both connections and removes the pair from the map.
// Idempotent — safe to call from multiple goroutines; only the first call acts.
func (rl *Relay) cleanup(udid string) {
	rl.mu.Lock()
	p, ok := rl.pairs[udid]
	if ok {
		delete(rl.pairs, udid)
	}
	rl.mu.Unlock()

	if !ok {
		return
	}
	if p.home != nil {
		p.home.Close()
	}
	if p.away != nil {
		p.away.Close()
	}
	// Signal the first handler's wait loop that the session is over.
	select {
	case <-p.done:
	default:
		close(p.done)
	}
	slog.Debug("relay pair cleaned up", "udid", udid)
}

// forwardBinary reads binary messages from src and writes them to dst.
// It calls cleanup on the first error (disconnect from either side).
func forwardBinary(udid string, src, dst *websocket.Conn, cleanup func()) {
	for {
		msgType, data, err := src.ReadMessage()
		if err != nil {
			slog.Debug("relay src disconnected", "udid", udid, "err", err)
			cleanup()
			return
		}
		if msgType != websocket.BinaryMessage {
			continue
		}
		if err := dst.WriteMessage(websocket.BinaryMessage, data); err != nil {
			slog.Debug("relay dst write failed", "udid", udid, "err", err)
			cleanup()
			return
		}
	}
}
