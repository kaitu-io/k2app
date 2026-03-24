package k2p

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestRelayConn_ReadWrite(t *testing.T) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	// Echo server: reads binary messages and writes them back.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer ws.Close()
		for {
			mt, msg, err := ws.ReadMessage()
			if err != nil {
				return
			}
			if err := ws.WriteMessage(mt, msg); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	wsURL := "ws" + srv.URL[len("http"):]

	conn, err := DialRelay(wsURL)
	if err != nil {
		t.Fatalf("DialRelay: %v", err)
	}
	defer conn.Close()

	sent := []byte{0x01, 0x02, 0x03}
	dummyAddr := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 9999}

	n, err := conn.WriteTo(sent, dummyAddr)
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if n != len(sent) {
		t.Fatalf("WriteTo: wrote %d bytes, want %d", n, len(sent))
	}

	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline: %v", err)
	}

	buf := make([]byte, 1500)
	n, addr, err := conn.ReadFrom(buf)
	if err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	if n != len(sent) {
		t.Fatalf("ReadFrom: got %d bytes, want %d", n, len(sent))
	}
	for i, b := range sent {
		if buf[i] != b {
			t.Fatalf("ReadFrom: buf[%d] = %#x, want %#x", i, buf[i], b)
		}
	}
	if addr == nil {
		t.Fatal("ReadFrom: addr is nil")
	}
}
