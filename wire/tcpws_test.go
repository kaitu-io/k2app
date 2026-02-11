package wire

import (
	"bufio"
	"bytes"
	"net"
	"sync"
	"testing"
	"time"

	smux "github.com/xtaci/smux/v2"
)

func TestDefaultSmuxConfig(t *testing.T) {
	cfg := DefaultSmuxConfig()
	if cfg == nil {
		t.Fatal("DefaultSmuxConfig returned nil")
	}
	if cfg.MaxFrameSize != 32768 {
		t.Errorf("MaxFrameSize = %d, want 32768", cfg.MaxFrameSize)
	}
	if cfg.MaxReceiveBuffer != 262144 {
		t.Errorf("MaxReceiveBuffer = %d, want 262144", cfg.MaxReceiveBuffer)
	}
	if cfg.MaxStreamBuffer != 65536 {
		t.Errorf("MaxStreamBuffer = %d, want 65536", cfg.MaxStreamBuffer)
	}
	if cfg.KeepAliveInterval != 10*time.Second {
		t.Errorf("KeepAliveInterval = %v, want 10s", cfg.KeepAliveInterval)
	}
	if cfg.KeepAliveTimeout != 30*time.Second {
		t.Errorf("KeepAliveTimeout = %v, want 30s", cfg.KeepAliveTimeout)
	}
}

func TestTCPWSClient_Create(t *testing.T) {
	cfg := Config{
		Host:        "1.2.3.4",
		Port:        443,
		SNI:         "www.microsoft.com",
		ECH:         "dGVzdA==",
		Fingerprint: "chrome",
	}
	tc := NewTCPWSClient(cfg, nil)
	if tc == nil {
		t.Fatal("NewTCPWSClient returned nil")
	}
	if tc.serverAddr != "1.2.3.4:443" {
		t.Errorf("serverAddr = %q, want %q", tc.serverAddr, "1.2.3.4:443")
	}
}

func TestTCPWSServer_Create(t *testing.T) {
	store := NewTicketStore()
	ts := NewTCPWSServer(store)
	if ts == nil {
		t.Fatal("NewTCPWSServer returned nil")
	}
}

func TestSmuxLoopback_ProxyStream(t *testing.T) {
	// Create a pipe to simulate the WebSocket transport layer
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	// Server side: smux server accepts streams
	serverDone := make(chan error, 1)
	go func() {
		sess, err := smux.Server(serverConn, DefaultSmuxConfig())
		if err != nil {
			serverDone <- err
			return
		}
		defer sess.Close()

		stream, err := sess.AcceptStream()
		if err != nil {
			serverDone <- err
			return
		}

		br := bufio.NewReader(stream)
		typ, addr, err := ReadStreamHeader(br)
		if err != nil {
			serverDone <- err
			return
		}
		if typ != StreamTypeTCP || addr != "8.8.8.8:53" {
			serverDone <- net.UnknownNetworkError("unexpected header")
			return
		}

		WriteStreamResponse(stream, nil)

		// Echo data
		buf := make([]byte, 100)
		n, err := br.Read(buf)
		if err != nil {
			serverDone <- err
			return
		}
		stream.Write(buf[:n])
		stream.Close()
		serverDone <- nil
	}()

	// Client side: smux client opens stream
	sess, err := smux.Client(clientConn, DefaultSmuxConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	stream, err := sess.OpenStream()
	if err != nil {
		t.Fatal(err)
	}

	WriteStreamHeader(stream, StreamTypeTCP, "8.8.8.8:53")

	br := bufio.NewReader(stream)
	ok, errMsg, err := ReadStreamResponse(br)
	if err != nil {
		t.Fatalf("ReadStreamResponse: %v", err)
	}
	if !ok {
		t.Fatalf("server rejected: %s", errMsg)
	}

	stream.Write([]byte("hello"))

	buf := make([]byte, 100)
	n, err := br.Read(buf)
	if err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if string(buf[:n]) != "hello" {
		t.Errorf("echo = %q, want %q", string(buf[:n]), "hello")
	}

	if err := <-serverDone; err != nil {
		t.Fatalf("server error: %v", err)
	}
}

func TestSmuxLoopback_UDPStream(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	serverDone := make(chan error, 1)
	go func() {
		sess, err := smux.Server(serverConn, DefaultSmuxConfig())
		if err != nil {
			serverDone <- err
			return
		}
		defer sess.Close()

		stream, err := sess.AcceptStream()
		if err != nil {
			serverDone <- err
			return
		}

		// Read stream type header
		br := bufio.NewReader(stream)
		typ, addr, err := ReadStreamHeader(br)
		if err != nil {
			serverDone <- err
			return
		}
		if typ != StreamTypeUDP {
			serverDone <- net.UnknownNetworkError("expected UDP stream type")
			return
		}
		_ = addr // UDP stream header has a reserved addr

		// Read a smux-framed UDP message
		frame, err := ReadSmuxUDPFrame(br)
		if err != nil {
			serverDone <- err
			return
		}
		if frame.SessionID != 1 {
			serverDone <- net.UnknownNetworkError("wrong session ID")
			return
		}

		// Echo it back
		WriteSmuxUDPFrame(stream, frame.SessionID, frame.Addr, frame.Payload)
		stream.Close()
		serverDone <- nil
	}()

	sess, err := smux.Client(clientConn, DefaultSmuxConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	stream, err := sess.OpenStream()
	if err != nil {
		t.Fatal(err)
	}

	// Send UDP stream header (type=0x02, no addr for shared stream)
	WriteStreamHeader(stream, StreamTypeUDP, "")

	// Send a UDP frame with smux framing
	WriteSmuxUDPFrame(stream, 1, "8.8.8.8:53", []byte("dns-query"))

	br := bufio.NewReader(stream)
	// Skip stream header echo (we don't send one from server)
	// Read echoed UDP frame
	frame, err := ReadSmuxUDPFrame(br)
	if err != nil {
		t.Fatalf("ReadSmuxUDPFrame: %v", err)
	}
	if frame.SessionID != 1 {
		t.Errorf("session_id = %d, want 1", frame.SessionID)
	}
	if !bytes.Equal(frame.Payload, []byte("dns-query")) {
		t.Errorf("payload = %q, want dns-query", frame.Payload)
	}

	if err := <-serverDone; err != nil {
		t.Fatalf("server error: %v", err)
	}
}

func TestSmuxLoopback_MultipleStreams(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	const numStreams = 5

	serverDone := make(chan error, 1)
	go func() {
		sess, err := smux.Server(serverConn, DefaultSmuxConfig())
		if err != nil {
			serverDone <- err
			return
		}

		var wg sync.WaitGroup
		for i := 0; i < numStreams; i++ {
			stream, err := sess.AcceptStream()
			if err != nil {
				serverDone <- err
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer stream.Close()
				br := bufio.NewReader(stream)
				_, _, _ = ReadStreamHeader(br)
				WriteStreamResponse(stream, nil)
				buf := make([]byte, 100)
				n, _ := br.Read(buf)
				stream.Write(buf[:n])
			}()
		}
		wg.Wait()
		sess.Close()
		serverDone <- nil
	}()

	sess, err := smux.Client(clientConn, DefaultSmuxConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	for i := 0; i < numStreams; i++ {
		stream, err := sess.OpenStream()
		if err != nil {
			t.Fatal(err)
		}
		WriteStreamHeader(stream, StreamTypeTCP, "10.0.0.1:80")

		br := bufio.NewReader(stream)
		ok, _, err := ReadStreamResponse(br)
		if err != nil {
			t.Fatalf("stream %d: ReadStreamResponse: %v", i, err)
		}
		if !ok {
			t.Fatalf("stream %d: rejected", i)
		}

		stream.Write([]byte("data"))
		buf := make([]byte, 100)
		n, err := br.Read(buf)
		if err != nil {
			t.Fatalf("stream %d: read echo: %v", i, err)
		}
		if string(buf[:n]) != "data" {
			t.Errorf("stream %d: echo = %q", i, string(buf[:n]))
		}
		stream.Close()
	}

	if err := <-serverDone; err != nil {
		t.Fatalf("server error: %v", err)
	}
}
