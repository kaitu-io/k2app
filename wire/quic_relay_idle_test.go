package wire

import (
	"errors"
	"os"
	"testing"
	"time"
)

// ==========================================================================
// Relay health vs. session end-of-life
//
// UDP has no close, so every relayed session ends the same way: its reader
// hits the idle deadline. That is only evidence of a broken datagram path when
// the session sent something and never heard back. A session that was
// answered and then went quiet (NTP, a finished QUIC connection, a game that
// was closed) ended normally.
// ==========================================================================

// expireIdle drives c into its idle deadline and asserts the reader saw it.
func expireIdle(t *testing.T, c *quicUDPConn) {
	t.Helper()
	c.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
	if _, _, err := c.ReadFrom(make([]byte, 64)); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("ReadFrom = %v, want os.ErrDeadlineExceeded", err)
	}
}

func TestQuicUDPConn_IdleExpiryAfterReply_IsNotARelayTimeout(t *testing.T) {
	first := newTestQUICUDPConn(&fakeDatagramSender{})
	mux := first.mux

	// More answered sessions than the threshold, all ending idle with no
	// other traffic on the mux in between — a quiet network.
	for i := int32(0); i < relayTimeoutThreshold+1; i++ {
		c := newTestQUICUDPConn(&fakeDatagramSender{})
		c.mux = mux

		if _, err := c.WriteTo([]byte("request"), nil); err != nil {
			t.Fatalf("WriteTo: %v", err)
		}
		c.readCh <- &UDPFrame{SessionID: c.sessionID, Payload: []byte("reply")}
		if _, _, err := c.ReadFrom(make([]byte, 64)); err != nil {
			t.Fatalf("ReadFrom reply: %v", err)
		}
		expireIdle(t, c)
	}

	if got := mux.relayTimeouts.Load(); got != 0 {
		t.Fatalf("relayTimeouts = %d after answered sessions went idle, want 0", got)
	}
	if !mux.datagramRelayHealthy() {
		t.Fatal("answered sessions ending idle must not mark the datagram relay unhealthy")
	}
}

func TestQuicUDPConn_UnansweredSession_CountsAsRelayTimeout(t *testing.T) {
	first := newTestQUICUDPConn(&fakeDatagramSender{})
	mux := first.mux

	for i := int32(0); i < relayTimeoutThreshold; i++ {
		if !mux.datagramRelayHealthy() {
			t.Fatalf("unhealthy after %d unanswered sessions, threshold is %d", i, relayTimeoutThreshold)
		}
		c := newTestQUICUDPConn(&fakeDatagramSender{})
		c.mux = mux
		if _, err := c.WriteTo([]byte("request"), nil); err != nil {
			t.Fatalf("WriteTo: %v", err)
		}
		expireIdle(t, c)
		if got := mux.relayTimeouts.Load(); got != i+1 {
			t.Fatalf("relayTimeouts = %d after %d unanswered sessions, want %d", got, i+1, i+1)
		}
	}

	if mux.datagramRelayHealthy() {
		t.Fatal("sessions that sent and never heard back must mark the datagram relay unhealthy")
	}
}

func TestQuicUDPConn_NothingSent_TimeoutIsNotARelayTimeout(t *testing.T) {
	c := newTestQUICUDPConn(&fakeDatagramSender{})

	expireIdle(t, c)

	if got := c.mux.relayTimeouts.Load(); got != 0 {
		t.Fatalf("relayTimeouts = %d for a session that never sent, want 0", got)
	}
}
