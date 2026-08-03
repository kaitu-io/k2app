package wire

import (
	"net"
	"os"
	"sync"
	"testing"
	"time"
)

// ============================================================================
// readDeadline unit tests
// ============================================================================

func TestReadDeadline_ZeroMeansNoDeadline(t *testing.T) {
	var d readDeadline
	ch, timer, err := d.makeTimer()
	if ch != nil || timer != nil || err != nil {
		t.Fatalf("zero deadline: got (%v, %v, %v), want (nil, nil, nil)", ch, timer, err)
	}
}

func TestReadDeadline_PastDeadline(t *testing.T) {
	var d readDeadline
	d.set(time.Now().Add(-time.Second))
	ch, timer, err := d.makeTimer()
	if ch != nil || timer != nil {
		t.Fatalf("past deadline: ch=%v, timer=%v, want nil", ch, timer)
	}
	if err != os.ErrDeadlineExceeded {
		t.Fatalf("past deadline: err=%v, want os.ErrDeadlineExceeded", err)
	}
}

func TestReadDeadline_FutureDeadline(t *testing.T) {
	var d readDeadline
	d.set(time.Now().Add(time.Hour))
	ch, timer, err := d.makeTimer()
	if err != nil {
		t.Fatalf("future deadline: err=%v", err)
	}
	if ch == nil || timer == nil {
		t.Fatal("future deadline: ch or timer is nil")
	}
	timer.Stop()
}

func TestReadDeadline_ClearDeadline(t *testing.T) {
	var d readDeadline
	d.set(time.Now().Add(time.Hour))
	d.set(time.Time{}) // clear
	ch, timer, err := d.makeTimer()
	if ch != nil || timer != nil || err != nil {
		t.Fatalf("cleared deadline: got (%v, %v, %v), want (nil, nil, nil)", ch, timer, err)
	}
}

func TestReadDeadline_ConcurrentAccess(t *testing.T) {
	var d readDeadline
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			d.set(time.Now().Add(time.Hour))
		}()
		go func() {
			defer wg.Done()
			d.makeTimer()
		}()
	}
	wg.Wait()
}

// ============================================================================
// ReadFrom deadline integration tests
// ============================================================================

func TestQuicUDPConn_ReadFrom_Timeout(t *testing.T) {
	c := &quicUDPConn{
		readCh:     make(chan *UDPFrame),
		reaped:     make(chan struct{}),
		done:       make(chan struct{}),
		targetAddr: "8.8.8.8:53",
		mux: &quicUDPMux{
			sessionDone: make(map[uint32]chan struct{}),
			done:        make(chan struct{}),
		},
	}
	c.SetReadDeadline(time.Now().Add(50 * time.Millisecond))

	start := time.Now()
	buf := make([]byte, 512)
	_, _, err := c.ReadFrom(buf)
	elapsed := time.Since(start)

	if err != os.ErrDeadlineExceeded {
		t.Fatalf("ReadFrom err=%v, want os.ErrDeadlineExceeded", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("ReadFrom took %v, want <500ms", elapsed)
	}
}

func TestQuicUDPConn_ReadFrom_NoDeadline(t *testing.T) {
	readCh := make(chan *UDPFrame, 1)
	readCh <- &UDPFrame{Payload: []byte("hello")}

	c := &quicUDPConn{
		readCh:     readCh,
		reaped:     make(chan struct{}),
		done:       make(chan struct{}),
		targetAddr: "8.8.8.8:53",
		mux: &quicUDPMux{
			sessionDone: make(map[uint32]chan struct{}),
			done:        make(chan struct{}),
		},
	}

	buf := make([]byte, 512)
	n, addr, err := c.ReadFrom(buf)
	if err != nil {
		t.Fatalf("ReadFrom err=%v", err)
	}
	if string(buf[:n]) != "hello" {
		t.Fatalf("ReadFrom data=%q, want %q", buf[:n], "hello")
	}
	if addr == nil {
		t.Fatal("ReadFrom addr is nil")
	}
}

func TestServerUDPConn_ReadFrom_Timeout(t *testing.T) {
	c := &serverUDPConn{
		readCh: make(chan *UDPFrame),
		done:   make(chan struct{}),
		addr:   "1.2.3.4:443",
	}
	c.SetReadDeadline(time.Now().Add(50 * time.Millisecond))

	buf := make([]byte, 512)
	_, _, err := c.ReadFrom(buf)
	if err != os.ErrDeadlineExceeded {
		t.Fatalf("ReadFrom err=%v, want os.ErrDeadlineExceeded", err)
	}
}

func TestSmuxUDPConn_ReadFrom_Timeout(t *testing.T) {
	c := &smuxUDPConn{
		readCh:     make(chan *UDPFrame),
		reaped:     make(chan struct{}),
		done:       make(chan struct{}),
		targetAddr: "8.8.8.8:53",
		mux: &smuxUDPMux{
			sessionDone: make(map[uint32]chan struct{}),
			done:        make(chan struct{}),
		},
	}
	c.SetReadDeadline(time.Now().Add(50 * time.Millisecond))

	buf := make([]byte, 512)
	_, _, err := c.ReadFrom(buf)
	if err != os.ErrDeadlineExceeded {
		t.Fatalf("ReadFrom err=%v, want os.ErrDeadlineExceeded", err)
	}
}

func TestSmuxServerUDPConn_ReadFrom_Timeout(t *testing.T) {
	c := &smuxServerUDPConn{
		readCh: make(chan *UDPFrame),
		done:   make(chan struct{}),
		addr:   "1.2.3.4:443",
	}
	c.SetReadDeadline(time.Now().Add(50 * time.Millisecond))

	buf := make([]byte, 512)
	_, _, err := c.ReadFrom(buf)
	if err != os.ErrDeadlineExceeded {
		t.Fatalf("ReadFrom err=%v, want os.ErrDeadlineExceeded", err)
	}
}

func TestReadFrom_DeadlineExceeded_IsTimeout(t *testing.T) {
	// os.ErrDeadlineExceeded satisfies net.Error with Timeout()=true
	var netErr net.Error
	if !errorAs(os.ErrDeadlineExceeded, &netErr) {
		t.Fatal("os.ErrDeadlineExceeded does not satisfy net.Error")
	}
	if !netErr.Timeout() {
		t.Fatal("os.ErrDeadlineExceeded.Timeout() = false, want true")
	}
}

// errorAs is a local helper to avoid importing errors package just for As.
func errorAs(err error, target interface{}) bool {
	// os.ErrDeadlineExceeded is *os.SyscallError wrapping... actually it's
	// a direct *errors.errorString in Go, but it implements net.Error.
	// We just need to check the interface assertion.
	netErr, ok := err.(net.Error)
	if ok {
		if p, ok2 := target.(*net.Error); ok2 {
			*p = netErr
			return true
		}
	}
	return false
}

func TestReadFrom_PastDeadline_ImmediateReturn(t *testing.T) {
	c := &quicUDPConn{
		readCh:     make(chan *UDPFrame),
		reaped:     make(chan struct{}),
		done:       make(chan struct{}),
		targetAddr: "8.8.8.8:53",
		mux: &quicUDPMux{
			sessionDone: make(map[uint32]chan struct{}),
			done:        make(chan struct{}),
		},
	}
	c.SetReadDeadline(time.Now().Add(-time.Second))

	start := time.Now()
	buf := make([]byte, 512)
	_, _, err := c.ReadFrom(buf)
	elapsed := time.Since(start)

	if err != os.ErrDeadlineExceeded {
		t.Fatalf("ReadFrom err=%v, want os.ErrDeadlineExceeded", err)
	}
	if elapsed > 10*time.Millisecond {
		t.Fatalf("past deadline took %v, want <10ms", elapsed)
	}
}

func TestReadFrom_DataBeforeDeadline(t *testing.T) {
	readCh := make(chan *UDPFrame, 1)
	c := &quicUDPConn{
		readCh:     readCh,
		reaped:     make(chan struct{}),
		done:       make(chan struct{}),
		targetAddr: "8.8.8.8:53",
		mux: &quicUDPMux{
			sessionDone: make(map[uint32]chan struct{}),
			done:        make(chan struct{}),
		},
	}
	// Deadline in the future, but data arrives immediately
	c.SetReadDeadline(time.Now().Add(5 * time.Second))
	readCh <- &UDPFrame{Payload: []byte("fast")}

	buf := make([]byte, 512)
	n, _, err := c.ReadFrom(buf)
	if err != nil {
		t.Fatalf("ReadFrom err=%v", err)
	}
	if string(buf[:n]) != "fast" {
		t.Fatalf("data=%q, want %q", buf[:n], "fast")
	}
}
