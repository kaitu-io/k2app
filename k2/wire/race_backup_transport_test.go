package wire

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
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

// N-1 (re-review 4): TCP-WS wins and QUIC finishes ~10ms later — the race
// must keep that QUIC dialer as QUICBackup (installed as primary with
// tcpws active, "quic-race-lost") and must not close it. With the drain
// count one short, the drain returned at once and the deferred cleanup
// closed the QUIC dialer.
func TestRaceTransport_TCPWSWinsQUICBackupKept(t *testing.T) {
	var gate time.Time
	quicValidator := func(token, udid, mode string) AuthVerdict {
		time.Sleep(time.Until(gate.Add(10 * time.Millisecond)))
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

	gate = time.Now().Add(1500 * time.Millisecond) // after the 800ms tcpws stagger
	gatedTCPWSServer(t, tcpLn, gate)

	cfg := Config{Host: "127.0.0.1", Port: addr.Port, Insecure: true, Fingerprint: "chrome", UDID: "d", Token: "t"}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	res, err := RaceTransport(ctx, cfg, "", nil, nil)
	if err != nil {
		t.Fatalf("RaceTransport: %v", err)
	}
	defer res.Dialer.Close()
	if res.TransportType != "tcpws" {
		t.Skipf("QUIC won (%s): timing precondition not met", res.TransportType)
	}
	if res.QUICBackup == nil {
		t.Fatal("QUICBackup = nil: the QUIC candidate that connected ~10ms after tcpws was dropped")
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
