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
