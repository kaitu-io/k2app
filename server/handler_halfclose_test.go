package server

import (
	"io"
	"net"
	"testing"
	"time"
)

// halfCloseConn gives a net.Pipe end a CloseWrite that leaves it readable,
// as a TCP-WS or QUIC stream's does.
type halfCloseConn struct{ net.Conn }

func (halfCloseConn) CloseWrite() error { return nil }

// After the target finishes, pipe half-closes the client side and waits for
// its upload. A client that never closes must not hold the handler (and the
// target conn) past pipeHalfCloseGrace.
func TestPipe_HalfClosedClientIsBounded(t *testing.T) {
	old := pipeHalfCloseGrace
	pipeHalfCloseGrace = 100 * time.Millisecond
	t.Cleanup(func() { pipeHalfCloseGrace = old })

	aClient, a := net.Pipe()
	b, bPeer := net.Pipe()
	defer aClient.Close()
	go io.Copy(io.Discard, aClient) // the client reads, never writes or closes
	go func() {
		bPeer.Write([]byte("response"))
		bPeer.Close()
	}()

	done := make(chan struct{})
	go func() {
		NewProxyHandler().pipe(halfCloseConn{a}, b)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("pipe still waiting on a half-closed client that never closes")
	}
}

// The bound must not cut a client that is still uploading inside it: the
// target (a real TCP conn, so it can half-close) sends its response and
// FIN, then keeps reading the client's upload.
func TestPipe_HalfClosedClientUploadCompletes(t *testing.T) {
	old := pipeHalfCloseGrace
	pipeHalfCloseGrace = 500 * time.Millisecond // the upload ends inside it
	t.Cleanup(func() { pipeHalfCloseGrace = old })

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan int64, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			got <- -1
			return
		}
		defer c.Close()
		c.Write([]byte("response"))
		c.(*net.TCPConn).CloseWrite()
		n, _ := io.Copy(io.Discard, c)
		got <- n
	}()
	b, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	aClient, a := net.Pipe()
	go func() {
		io.ReadFull(aClient, make([]byte, 8))
		time.Sleep(50 * time.Millisecond) // upload starts after the target's FIN
		aClient.Write(make([]byte, 1<<16))
		aClient.Close()
	}()
	done := make(chan struct{})
	go func() {
		NewProxyHandler().pipe(halfCloseConn{a}, b)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("pipe did not finish")
	}
	if n := <-got; n != 1<<16 {
		t.Fatalf("target received %d upload bytes; want %d", n, 1<<16)
	}
}

// peerClosedConn is a half-close client conn whose PeerClosed the test
// fires, as a TCP-WS stream's fires on the client's full close.
type peerClosedConn struct {
	halfCloseConn
	closed chan struct{}
}

func (c peerClosedConn) PeerClosed() <-chan struct{} { return c.closed }

// A client that has closed fully can receive nothing more. If the target
// stays silent (never sends, never closes), pipe must end anyway rather
// than hold the handler and the target conn until the target gives up.
// The target is a real TCP conn: pipe half-closes it after the upload, and
// a net.Pipe, having no CloseWrite, would be closed outright instead.
func TestPipe_ClientFullCloseEndsSilentTarget(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	stop, targetDone := make(chan struct{}), make(chan struct{})
	defer func() { close(stop); <-targetDone }()
	go func() {
		defer close(targetDone)
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		io.Copy(io.Discard, c) // reads to the FIN, never sends
		<-stop
	}()
	b, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	aClient, a := net.Pipe()
	pc := peerClosedConn{halfCloseConn{a}, make(chan struct{})}
	done := make(chan struct{})
	go func() {
		NewProxyHandler().pipe(pc, b)
		close(done)
	}()
	aClient.Write([]byte("request"))
	aClient.Close()
	close(pc.closed)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("pipe still open on a silent target after the client closed fully")
	}
}

// The client's full close must not cut its upload short: data it sent
// before closing still reaches the target.
func TestPipe_ClientFullCloseKeepsUploadTail(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan int64, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			got <- -1
			return
		}
		defer c.Close()
		n, _ := io.Copy(io.Discard, c)
		got <- n
	}()
	b, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	aClient, a := net.Pipe()
	pc := peerClosedConn{halfCloseConn{a}, make(chan struct{})}
	close(pc.closed) // the client's close is already known
	go func() {
		aClient.Write(make([]byte, 1<<20))
		aClient.Close()
	}()
	done := make(chan struct{})
	go func() {
		NewProxyHandler().pipe(pc, b)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("pipe did not finish")
	}
	if n := <-got; n != 1<<20 {
		t.Fatalf("target received %d upload bytes; want %d", n, 1<<20)
	}
}
