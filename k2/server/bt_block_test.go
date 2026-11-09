package server

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"testing"
	"time"

	"github.com/kaitu-io/k2/wire"
)

var btHandshake = append([]byte("\x13BitTorrent protocol"), make([]byte, 48)...)

// tcpEchoTarget accepts one connection, echoes it, and reports what it read.
func tcpEchoTarget(t *testing.T) (addr string, got <-chan []byte) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	ch := make(chan []byte, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		var seen bytes.Buffer
		io.Copy(io.MultiWriter(c, &seen), c)
		ch <- seen.Bytes()
	}()
	return ln.Addr().String(), ch
}

// runTCP starts HandleTCP over a pipe, consumes the stream response, writes
// chunks (each a separate Write), and returns what the client read back
// before the stream ended plus what the target received.
func runTCP(t *testing.T, chunks ...[]byte) (echoed []byte, targetGot []byte) {
	t.Helper()
	addr, got := tcpEchoTarget(t)
	h := newTestProxyHandler()
	client, server := net.Pipe()
	done := make(chan struct{})
	go func() { h.HandleTCP(server, addr); close(done) }()

	br := bufio.NewReader(client)
	if ok, _, msg, err := wire.ReadStreamResponse(br); err != nil || !ok {
		t.Fatalf("stream response: ok=%v msg=%q err=%v", ok, msg, err)
	}
	go func() {
		for _, c := range chunks {
			if _, err := client.Write(c); err != nil {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	echoed, _ = io.ReadAll(br)
	client.Close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("HandleTCP did not return")
	}
	select {
	case targetGot = <-got:
	case <-time.After(3 * time.Second):
		t.Fatal("target never finished")
	}
	return echoed, targetGot
}

func TestHandleTCP_BitTorrentHandshakeBlocked(t *testing.T) {
	echoed, targetGot := runTCP(t, btHandshake)
	if len(targetGot) != 0 || len(echoed) != 0 {
		t.Fatalf("BT handshake leaked: target got %d bytes, client got %d", len(targetGot), len(echoed))
	}
}

func TestHandleTCP_SplitBitTorrentHandshakeBlocked(t *testing.T) {
	// The 20-byte protocol header arrives across two writes.
	echoed, targetGot := runTCP(t, btHandshake[:5], btHandshake[5:])
	if len(targetGot) != 0 || len(echoed) != 0 {
		t.Fatalf("split BT handshake leaked: target got %d bytes, client got %d", len(targetGot), len(echoed))
	}
}

func TestHandleTCP_Leading0x13NotBitTorrentPasses(t *testing.T) {
	payload := append([]byte{0x13}, []byte("not a torrent at all, just data")...)
	echoed, targetGot := runTCP(t, payload[:3], payload[3:])
	if !bytes.Equal(targetGot, payload) || !bytes.Equal(echoed, payload) {
		t.Fatalf("non-BT stream altered: target=%q echoed=%q", targetGot, echoed)
	}
}

func TestHandleTCP_ShortFirstChunkPasses(t *testing.T) {
	// A protocol whose first write is a lone 0x13 then something else.
	echoed, targetGot := runTCP(t, []byte{0x13}, []byte("Xrest"))
	want := []byte("\x13Xrest")
	if !bytes.Equal(targetGot, want) || !bytes.Equal(echoed, want) {
		t.Fatalf("stream altered: target=%q echoed=%q", targetGot, echoed)
	}
}

func TestHandleUDP_BitTorrentOpenerEndsSession(t *testing.T) {
	cases := map[string][]byte{
		"utp-syn": append([]byte{0x41, 0x00, 0x12, 0x34}, make([]byte, 16)...),
		"dht":     []byte("d1:ad2:id20:xxxxxxxxxxxxxxxxxxxxe1:q4:ping1:t2:aa1:y1:qe"),
		"tracker": {0, 0, 0x04, 0x17, 0x27, 0x10, 0x19, 0x80, 0, 0, 0, 0, 0, 0, 0, 1},
	}
	for name, pkt := range cases {
		t.Run(name, func(t *testing.T) {
			okAddr, stopOK := echoUDPServer(t, "A")
			defer stopOK()
			btAddr, stopBT := echoUDPServer(t, "B")
			defer stopBT()

			h := newTestProxyHandler()
			sess := newFakeUDPSession()
			done := make(chan struct{})
			go func() { h.HandleUDP(sess, okAddr); close(done) }()

			sess.feed("hello", okAddr)
			select {
			case p := <-sess.out:
				if string(p.payload) != "Ahello" {
					t.Fatalf("unexpected echo %q", p.payload)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("normal packet not forwarded")
			}

			sess.in <- &fakePacket{payload: pkt, addr: strAddr(btAddr)}
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("session not ended after BT opener")
			}
			for {
				select {
				case p := <-sess.out:
					if bytes.HasPrefix(p.payload, []byte("B")) {
						t.Fatalf("BT packet reached its destination: %q", p.payload)
					}
				default:
					return
				}
			}
		})
	}
}

func TestHandleUDP_WireGuardInitNotBlocked(t *testing.T) {
	addr, stop := echoUDPServer(t, "W")
	defer stop()
	h := newTestProxyHandler()
	sess := newFakeUDPSession()
	go h.HandleUDP(sess, addr)
	defer sess.Close()

	wg := append([]byte{0x01, 0, 0, 0}, make([]byte, 144)...)
	sess.in <- &fakePacket{payload: wg, addr: strAddr(addr)}
	select {
	case p := <-sess.out:
		if !bytes.HasPrefix(p.payload, []byte("W\x01")) {
			t.Fatalf("unexpected echo %q", p.payload[:4])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WireGuard handshake was dropped")
	}
}

func TestBTBlockLog_RateLimitedPerDevice(t *testing.T) {
	var l btBlockLog
	now := time.Unix(1000, 0)
	k := DeviceKey{UDID: "dev-a"}
	if emit, _ := l.note(k, now); !emit {
		t.Fatal("first block must be logged")
	}
	for i := 0; i < 5; i++ {
		if emit, _ := l.note(k, now.Add(time.Second)); emit {
			t.Fatal("repeat inside the window must be suppressed")
		}
	}
	emit, suppressed := l.note(k, now.Add(btBlockLogEvery+time.Second))
	if !emit || suppressed != 5 {
		t.Fatalf("after window: emit=%v suppressed=%d, want true/5", emit, suppressed)
	}
	if emit, _ := l.note(DeviceKey{UDID: "dev-b"}, now.Add(time.Second)); !emit {
		t.Fatal("a different device has its own window")
	}
}
