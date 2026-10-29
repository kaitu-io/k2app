package smux

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

// Hardening tests pin k2 patch 5 (see K2_PATCHES.md).

// rawFrame encodes a frame the way sendLoop does, with an arbitrary length
// field and payload, so tests can send what a conforming peer never would.
func rawFrame(cmd byte, sid uint32, length uint16, payload []byte) []byte {
	b := make([]byte, headerSize+len(payload))
	b[0] = version
	b[1] = cmd
	binary.LittleEndian.PutUint16(b[2:], length)
	binary.LittleEndian.PutUint32(b[4:], sid)
	copy(b[headerSize:], payload)
	return b
}

// rawPeer returns a server session and the raw client end of its conn,
// with a goroutine draining whatever the session writes.
func rawPeer(t *testing.T, cfg *Config) (*Session, net.Conn) {
	t.Helper()
	a, b := net.Pipe()
	ss, err := Server(b, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close(); a.Close() })
	go io.Copy(io.Discard, a)
	return ss, a
}

func waitClosed(t *testing.T, s *Session, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !s.IsClosed() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !s.IsClosed() {
		t.Fatalf("%s: session still open", what)
	}
}

// A length on a NOP/SYN/FIN, or a UPD whose length is not 8, would be
// parsed as the next header. The session must fail instead of desyncing.
func TestHardening_FrameLengthValidated(t *testing.T) {
	for _, tc := range []struct {
		name  string
		frame []byte
	}{
		{"NOPWithPayload", rawFrame(cmdNOP, 0, 4, []byte{1, 2, 3, 4})},
		{"SYNWithPayload", rawFrame(cmdSYN, 1, 4, []byte{1, 2, 3, 4})},
		{"FINWithPayload", rawFrame(cmdFIN, 1, 4, []byte{1, 2, 3, 4})},
		{"UPDShort", rawFrame(cmdUPD, 1, 4, []byte{1, 2, 3, 4})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ss, raw := rawPeer(t, nil)
			raw.Write(tc.frame)
			waitClosed(t, ss, tc.name)
			if err, _ := ss.protoError.Load().(error); err != ErrInvalidProtocol {
				t.Fatalf("protoError = %v; want ErrInvalidProtocol", err)
			}
		})
	}
}

// Write computes the remaining window as int32. A window past MaxInt32
// would make it negative, and the writer would wait forever.
func TestHardening_UPDWindowOverflowRejected(t *testing.T) {
	ss, raw := rawPeer(t, nil)
	raw.Write(rawFrame(cmdSYN, 1, 0, nil))
	var upd [szCmdUPD]byte
	binary.LittleEndian.PutUint32(upd[0:], 0)
	binary.LittleEndian.PutUint32(upd[4:], 1<<31)
	raw.Write(rawFrame(cmdUPD, 1, szCmdUPD, upd[:]))
	waitClosed(t, ss, "UPD window 2^31")
}

// Once recvLoop has died the session can never receive again. It must
// close, so OpenStream fails and callers checking IsClosed see it.
func TestHardening_ReadErrorClosesSession(t *testing.T) {
	ss, raw := rawPeer(t, nil)
	raw.Close()
	waitClosed(t, ss, "after peer conn closed")
	if _, err := ss.OpenStream(); err == nil {
		t.Fatal("OpenStream succeeded on a session whose read side is dead")
	}
}

// A peer that keeps the session alive with NOPs but never reads used to
// block sendLoop in Write forever. Writes are now bounded by
// KeepAliveTimeout, and the session closes.
func TestHardening_StalledPeerWriteTimesOut(t *testing.T) {
	cfg := DefaultConfig()
	cfg.KeepAliveInterval = 100 * time.Millisecond
	cfg.KeepAliveTimeout = 500 * time.Millisecond
	a, b := net.Pipe()
	ss, err := Server(b, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	defer a.Close()
	// The raw peer: opens a stream, then only ever sends NOPs.
	a.Write(rawFrame(cmdSYN, 1, 0, nil))
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(50 * time.Millisecond):
				if _, err := a.Write(rawFrame(cmdNOP, 0, 0, nil)); err != nil {
					return
				}
			}
		}
	}()
	st, err := ss.AcceptStream()
	if err != nil {
		t.Fatal(err)
	}
	wrote := make(chan error, 1)
	go func() {
		_, err := st.Write(make([]byte, 128<<10))
		wrote <- err
	}()
	select {
	case err := <-wrote:
		if err == nil {
			t.Fatal("write to a peer that never reads succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("write to a peer that never reads is still blocked")
	}
	waitClosed(t, ss, "stalled writer")
}

// recvLoop hands accepted streams over outside streamLock. With the accept
// backlog full it blocks there, but must not take OpenStream and Close down
// with it.
func TestHardening_FullAcceptBacklogDoesNotHoldStreamLock(t *testing.T) {
	ss, raw := rawPeer(t, nil)
	go func() {
		for i := 0; i <= defaultAcceptBacklog+1; i++ {
			if _, err := raw.Write(rawFrame(cmdSYN, uint32(2*i+1), 0, nil)); err != nil {
				return
			}
		}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for len(ss.chAccepts) < defaultAcceptBacklog && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond) // let recvLoop block on the next SYN
	opened := make(chan error, 1)
	go func() {
		st, err := ss.OpenStream()
		if err == nil {
			st.Close()
		}
		opened <- err
	}()
	select {
	case err := <-opened:
		if err != nil {
			t.Fatalf("OpenStream: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("OpenStream blocked behind a full accept backlog")
	}
}

// The opener registers its stream before sending SYN, so an acceptor that
// writes and closes at once (no "opener speaks first") loses nothing.
func TestHardening_AcceptorMayWriteFirst(t *testing.T) {
	cs, ss := halfClosePair(t)
	want := []byte("hello from the acceptor")
	go func() {
		for {
			st, err := ss.AcceptStream()
			if err != nil {
				return
			}
			st.Write(want)
			st.Close()
		}
	}()
	for i := 0; i < 500; i++ {
		st, err := cs.OpenStream()
		if err != nil {
			t.Fatal(err)
		}
		st.SetReadDeadline(time.Now().Add(5 * time.Second))
		got, err := io.ReadAll(st)
		st.Close()
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("round %d: got %q, err=%v; want %q", i, got, err, want)
		}
	}
}

// SetReadDeadline / SetWriteDeadline from another goroutine must reach a
// Read or Write that is already blocked, as net.Conn specifies.
func TestHardening_DeadlineWakesBlockedCalls(t *testing.T) {
	cs, ss := halfClosePair(t)
	go func() {
		st, err := acceptAfterFirstByte(ss)
		if err != nil {
			return
		}
		<-time.After(10 * time.Second) // never reads, never writes
		st.Close()
	}()
	st, err := cs.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	st.Write([]byte("x"))

	read := make(chan error, 1)
	go func() {
		_, err := st.Read(make([]byte, 1))
		read <- err
	}()
	wrote := make(chan error, 1)
	go func() {
		_, err := st.Write(make([]byte, 1<<20)) // blocks on the peer window
		wrote <- err
	}()
	time.Sleep(50 * time.Millisecond)
	st.SetDeadline(time.Now().Add(50 * time.Millisecond))
	if err := waitErr(t, read, "blocked Read after SetReadDeadline"); err != ErrTimeout {
		t.Fatalf("Read: err=%v; want ErrTimeout", err)
	}
	if err := waitErr(t, wrote, "blocked Write after SetWriteDeadline"); err != ErrTimeout {
		t.Fatalf("Write: err=%v; want ErrTimeout", err)
	}
}
