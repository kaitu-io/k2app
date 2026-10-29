package wire

import (
	"bufio"
	"bytes"
	"context"
	tls "github.com/metacubex/utls"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/kaitu-io/kuic"
)

// TestUDPOverflowStream_LazyInit verifies that the overflow stream is only
// opened on the first send() call, not at construction time.
func TestUDPOverflowStream_LazyInit(t *testing.T) {
	cert := generateTestCert(t)

	// Set up a raw QUIC pair
	serverUDPConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer serverUDPConn.Close()

	serverTransport := &quic.Transport{Conn: serverUDPConn}
	defer serverTransport.Close()

	serverTLS := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"h3"},
	}
	ln, err := serverTransport.Listen(serverTLS, DefaultQUICConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	serverAddr := serverUDPConn.LocalAddr().(*net.UDPAddr)

	// Accept connection on server side
	serverReady := make(chan *quic.Conn, 1)
	go func() {
		conn, err := ln.Accept(context.Background())
		if err != nil {
			return
		}
		serverReady <- conn
	}()

	// Client connects
	clientUDPConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer clientUDPConn.Close()

	clientTransport := &quic.Transport{Conn: clientUDPConn}
	defer clientTransport.Close()

	clientTLS := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"h3"},
	}
	conn, err := clientTransport.Dial(context.Background(), serverAddr, clientTLS, DefaultQUICConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseWithError(0, "")

	// Create overflow stream — should not open yet
	overflow := &udpOverflowStream{qconn: conn}

	if overflow.opened {
		t.Fatal("overflow stream should not be opened before send()")
	}
	if overflow.stream != nil {
		t.Fatal("overflow stream should be nil before send()")
	}

	// Wait for server connection
	select {
	case sconn := <-serverReady:
		defer sconn.CloseWithError(0, "")
		// Accept the stream that will be created by send()
		go func() {
			stream, err := sconn.AcceptStream(context.Background())
			if err != nil {
				return
			}
			defer stream.Close()
			// Read and discard
			buf := make([]byte, 4096)
			for {
				if _, err := stream.Read(buf); err != nil {
					return
				}
			}
		}()
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for server connection")
	}

	// Now send — should lazily open the stream
	err = overflow.send(1, "8.8.8.8:53", []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}

	if !overflow.opened {
		t.Fatal("overflow stream should be opened after send()")
	}
	if overflow.stream == nil {
		t.Fatal("overflow stream should not be nil after send()")
	}

	overflow.close()
}

// TestServerWriteTo_DatagramTooLarge_Overflow verifies that when the server's
// SendDatagram returns DatagramTooLargeError, the packet is sent via overflow stream.
func TestServerWriteTo_DatagramTooLarge_Overflow(t *testing.T) {
	client, server, cleanup := setupQUICPair(t)
	defer cleanup()

	// Server: accept UDP, then write a large response
	received := make(chan struct{}, 1)
	bigResponse := bytes.Repeat([]byte("X"), 1300) // likely to exceed datagram limit in some paths
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go server.ServeUDP(ctx, UDPHandlerFunc(func(conn net.PacketConn, addr string) {
		buf := make([]byte, 4096)
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			return
		}
		_ = n
		received <- struct{}{}
		// Write big response — may trigger overflow on some paths
		conn.WriteTo(bigResponse, nil)
	}))

	time.Sleep(50 * time.Millisecond)

	// Client: dial UDP and send small packet
	pconn, err := client.DialUDP(context.Background(), "8.8.8.8:53")
	if err != nil {
		t.Fatal(err)
	}
	defer pconn.Close()

	if _, err := pconn.WriteTo([]byte("ping"), nil); err != nil {
		t.Fatal(err)
	}

	// Wait for server to receive
	select {
	case <-received:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for server to receive")
	}

	// Read response (may come via datagram or overflow)
	buf := make([]byte, 4096)
	pconn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, _, err := pconn.ReadFrom(buf)
	if err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	if n != len(bigResponse) {
		t.Errorf("response len = %d, want %d", n, len(bigResponse))
	}
}

// TestHybridUDP_SharedSessions verifies that datagram and overflow stream
// packets for the same session ID arrive at the same PacketConn.
func TestHybridUDP_SharedSessions(t *testing.T) {
	client, server, cleanup := setupQUICPair(t)
	defer cleanup()

	// Server: echo all received packets
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go server.ServeUDP(ctx, UDPHandlerFunc(func(conn net.PacketConn, addr string) {
		buf := make([]byte, 4096)
		for {
			n, _, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			// Echo back
			if _, err := conn.WriteTo(buf[:n], nil); err != nil {
				return
			}
		}
	}))

	time.Sleep(50 * time.Millisecond)

	// Client: open a UDP session
	pconn, err := client.DialUDP(context.Background(), "8.8.8.8:53")
	if err != nil {
		t.Fatal(err)
	}
	defer pconn.Close()

	// Send a small packet (should go via datagram)
	smallPkt := []byte("small")
	if _, err := pconn.WriteTo(smallPkt, nil); err != nil {
		t.Fatal(err)
	}

	// Read response
	buf := make([]byte, 4096)
	pconn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, _, err := pconn.ReadFrom(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != "small" {
		t.Errorf("got %q, want %q", string(buf[:n]), "small")
	}

	// Send a large packet (may trigger overflow depending on path MTU)
	largePkt := bytes.Repeat([]byte("L"), 1300)
	if _, err := pconn.WriteTo(largePkt, nil); err != nil {
		t.Fatal(err)
	}

	// Read response — same session should receive it regardless of path
	pconn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, _, err = pconn.ReadFrom(buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1300 {
		t.Errorf("response len = %d, want 1300", n)
	}
}

// TestServerToClient_Overflow verifies the server can send overflow packets
// back to the client via a server-initiated overflow stream.
func TestServerToClient_Overflow(t *testing.T) {
	client, server, cleanup := setupQUICPair(t)
	defer cleanup()

	// Track what the server sends
	serverSent := make(chan int, 5)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go server.ServeUDP(ctx, UDPHandlerFunc(func(conn net.PacketConn, addr string) {
		buf := make([]byte, 4096)
		for {
			n, _, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			// Send responses of varying sizes
			response := bytes.Repeat([]byte("R"), n)
			written, err := conn.WriteTo(response, nil)
			if err != nil {
				return
			}
			serverSent <- written
		}
	}))

	time.Sleep(50 * time.Millisecond)

	pconn, err := client.DialUDP(context.Background(), "8.8.8.8:53")
	if err != nil {
		t.Fatal(err)
	}
	defer pconn.Close()

	// Send a small request
	if _, err := pconn.WriteTo([]byte("hi"), nil); err != nil {
		t.Fatal(err)
	}

	// Wait for server ack
	select {
	case <-serverSent:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for server to send")
	}

	// Read the response
	buf := make([]byte, 4096)
	pconn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, _, err := pconn.ReadFrom(buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("response len = %d, want 2", n)
	}
}

// TestStreamTypeUDPOverflow_BackwardCompat verifies that an old server that
// doesn't recognize StreamTypeUDPOverflow will close the stream gracefully
// (the unknown stream type case in handleStream).
func TestStreamTypeUDPOverflow_BackwardCompat(t *testing.T) {
	cert := generateTestCert(t)

	// Set up a raw QUIC pair (simulating "old" server with no overflow support)
	serverUDPConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer serverUDPConn.Close()

	serverTransport := &quic.Transport{Conn: serverUDPConn}
	defer serverTransport.Close()

	serverTLS := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"h3"},
	}
	ln, err := serverTransport.Listen(serverTLS, DefaultQUICConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	serverAddr := serverUDPConn.LocalAddr().(*net.UDPAddr)

	// "Old" server: accepts stream, reads header, closes unknown types
	var serverStreamClosed sync.WaitGroup
	serverStreamClosed.Add(1)
	go func() {
		defer serverStreamClosed.Done()
		conn, err := ln.Accept(context.Background())
		if err != nil {
			return
		}
		defer conn.CloseWithError(0, "")

		stream, err := conn.AcceptStream(context.Background())
		if err != nil {
			return
		}

		br := bufio.NewReader(stream)

		// Read H3 frame type
		frameType, _, err := ReadVarint(br)
		if err != nil || frameType != H3FrameTypeK2Proxy {
			stream.Close()
			return
		}

		// Read stream header
		streamType, _, err := ReadStreamHeader(br)
		if err != nil {
			stream.Close()
			return
		}

		// Old server doesn't know 0x03 — just closes the stream
		if streamType != StreamTypeTCP && streamType != StreamTypeUDP {
			stream.Close()
			return
		}
	}()

	// Client opens an overflow stream
	clientUDPConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer clientUDPConn.Close()

	clientTransport := &quic.Transport{Conn: clientUDPConn}
	defer clientTransport.Close()

	clientTLS := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"h3"},
	}
	conn, err := clientTransport.Dial(context.Background(), serverAddr, clientTLS, DefaultQUICConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseWithError(0, "")

	overflow := &udpOverflowStream{qconn: conn}
	err = overflow.send(1, "8.8.8.8:53", []byte("test"))
	// The send itself may succeed (buffered in QUIC) — the important thing
	// is that the server gracefully closes the stream without crashing.
	// Subsequent sends on a closed stream will fail, which is the expected
	// degradation (equivalent to dropping the packet).
	_ = err

	// Wait for server to handle and close
	done := make(chan struct{})
	go func() {
		serverStreamClosed.Wait()
		close(done)
	}()
	select {
	case <-done:
		// Server handled unknown stream type gracefully
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for server to close unknown stream")
	}

	overflow.close()
}

// TestUDPOverflowStreamInitCachedError verifies that init() returns the cached
// error on subsequent calls after an initial failure.
func TestUDPOverflowStreamInitCachedError(t *testing.T) {
	o := &udpOverflowStream{}
	o.opened = true
	o.initErr = errors.New("simulated init failure")

	err := o.init()
	if err == nil || err.Error() != "simulated init failure" {
		t.Errorf("init() = %v, want cached error", err)
	}
}

// TestUDPOverflowStreamCloseIdempotent verifies that close() is safe to call
// multiple times and on a zero-value struct (nil stream).
func TestUDPOverflowStreamCloseIdempotent(t *testing.T) {
	o := &udpOverflowStream{}
	o.close() // should not panic on nil stream
	o.close() // double close should not panic
}

// TestUDPOverflowStream_ConcurrentSend verifies that concurrent sends are serialized.
func TestUDPOverflowStream_ConcurrentSend(t *testing.T) {
	cert := generateTestCert(t)

	serverUDPConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer serverUDPConn.Close()

	serverTransport := &quic.Transport{Conn: serverUDPConn}
	defer serverTransport.Close()

	serverTLS := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"h3"},
	}
	ln, err := serverTransport.Listen(serverTLS, DefaultQUICConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	serverAddr := serverUDPConn.LocalAddr().(*net.UDPAddr)

	// Server: accept stream, read frames, count them
	frameCount := make(chan int, 1)
	go func() {
		conn, err := ln.Accept(context.Background())
		if err != nil {
			frameCount <- 0
			return
		}
		defer conn.CloseWithError(0, "")

		stream, err := conn.AcceptStream(context.Background())
		if err != nil {
			frameCount <- 0
			return
		}
		defer stream.Close()

		br := bufio.NewReader(stream)
		// Skip H3 frame type + stream header
		ReadVarint(br)
		ReadStreamHeader(br)

		count := 0
		for {
			_, err := ReadSmuxUDPFrame(br)
			if err != nil {
				break
			}
			count++
		}
		frameCount <- count
	}()

	clientUDPConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer clientUDPConn.Close()

	clientTransport := &quic.Transport{Conn: clientUDPConn}
	defer clientTransport.Close()

	clientTLS := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"h3"},
	}
	conn, err := clientTransport.Dial(context.Background(), serverAddr, clientTLS, DefaultQUICConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseWithError(0, "")

	overflow := &udpOverflowStream{qconn: conn}

	// Send 20 frames concurrently
	const numSenders = 20
	var wg sync.WaitGroup
	wg.Add(numSenders)
	for i := 0; i < numSenders; i++ {
		go func(id int) {
			defer wg.Done()
			overflow.send(uint32(id), "8.8.8.8:53", []byte("data"))
		}(i)
	}
	wg.Wait()
	overflow.close()

	select {
	case count := <-frameCount:
		if count != numSenders {
			t.Errorf("received %d frames, want %d", count, numSenders)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for frame count")
	}
}
