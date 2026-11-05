package wire

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	tls "github.com/metacubex/utls"
	smux "github.com/xtaci/smux/v2"
)

// gatedTCPWSServer serves TCP-WS on ln and answers each metadata stream with
// OK only at gate (the shape of a cold auth that both transports wait on).
func gatedTCPWSServer(t *testing.T, ln net.Listener, gate time.Time) {
	t.Helper()
	cert := generateTestCert(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/k2v5/tunnel", func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		wsConn := websocket.NetConn(context.Background(), ws, websocket.MessageBinary)
		defer wsConn.Close()
		sess, err := smux.Server(wsConn, DefaultSmuxConfig())
		if err != nil {
			return
		}
		defer sess.Close()
		for {
			stream, err := sess.AcceptStream()
			if err != nil {
				return
			}
			go func() {
				defer stream.Close()
				br := bufio.NewReader(stream)
				st, _, err := ReadStreamHeader(br)
				if err != nil {
					return
				}
				switch st {
				case StreamTypeMeta:
					if _, err := ReadMetadataFrame(br); err != nil {
						return
					}
					time.Sleep(time.Until(gate))
					_ = WriteMetadataResponse(stream, "", false)
				case StreamTypeTCP:
					_ = WriteStreamResponse(stream, nil)
				}
			}()
		}
	})
	srv := &http.Server{Handler: mux}
	go srv.Serve(tls.NewListener(ln, &tls.Config{Certificates: []tls.Certificate{cert}}))
	t.Cleanup(func() { srv.Close() })
}

// N-1 (re-review 4): TCP-WS wins while QUIC is still in flight — the race
// must keep that QUIC dialer as QUICBackup (installed as primary with tcpws
// active, "quic-race-lost") and must not close it. With the drain count one
// short, the drain returned at once and the deferred cleanup closed the
// QUIC dialer. Deterministic (R1 review M-5): QUIC's metadata verdict is
// held until the collector has picked the tcpws winner (testHookRaceWinner),
// and the drain grace is widened so its result always lands inside it.
func TestRaceTransport_TCPWSWinsQUICBackupKept(t *testing.T) {
	tcpwsWon := make(chan struct{})
	var wonOnce sync.Once
	testHookRaceWinner = func(tt string) {
		if tt == "tcpws" {
			wonOnce.Do(func() { close(tcpwsWon) })
		}
	}
	origGrace := drainQUICGrace
	drainQUICGrace = 5 * time.Second
	t.Cleanup(func() { testHookRaceWinner = nil; drainQUICGrace = origGrace })

	quicValidator := func(token, udid, mode string) AuthVerdict {
		select {
		case <-tcpwsWon: // QUIC answers only after tcpws has won
		case <-time.After(8 * time.Second):
		}
		return AuthVerdict{OK: true, UserID: 7}
	}
	var (
		addr    *net.UDPAddr
		tcpLn   net.Listener
		cleanup func()
	)
	for attempt := 0; attempt < 20 && tcpLn == nil; attempt++ {
		_, a, c := startRawQUICServer(t, quicValidator, false)
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", a.Port))
		if err != nil {
			c() // TCP port of that number taken: try another
			continue
		}
		addr, tcpLn, cleanup = a, ln, c
	}
	if tcpLn == nil {
		t.Skip("no port free on both UDP and TCP")
	}
	defer cleanup()
	defer tcpLn.Close()

	gatedTCPWSServer(t, tcpLn, time.Now()) // tcpws answers at once (after its 800ms stagger)

	cfg := Config{Host: "127.0.0.1", Port: addr.Port, Insecure: true, Fingerprint: "chrome", UDID: "d", Token: "t"}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	res, err := RaceTransport(ctx, cfg, "", nil, nil)
	if err != nil {
		t.Fatalf("RaceTransport: %v", err)
	}
	defer res.Dialer.Close()
	if res.TransportType != "tcpws" {
		t.Fatalf("QUIC won (%s) although its verdict was held until tcpws won", res.TransportType)
	}
	if res.QUICBackup == nil {
		t.Fatal("QUICBackup = nil: the QUIC candidate that connected after tcpws won was dropped")
	}
	defer res.QUICBackup.Close()
	time.Sleep(200 * time.Millisecond) // the race's deferred loser cleanup runs async
	qc, ok := res.QUICBackup.(*QUICClient)
	if !ok {
		t.Fatalf("QUICBackup is %T", res.QUICBackup)
	}
	qc.mu.Lock()
	closed, live := qc.closed, qc.conn != nil
	qc.mu.Unlock()
	if closed || !live {
		t.Fatalf("QUICBackup closed=%v live=%v: the race closed the backup it returned", closed, live)
	}
}
