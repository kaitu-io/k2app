package wire

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ==========================================================================
// An unhealthy datagram relay heals itself
//
// The timeout streak is only cleared by a datagram arriving. Once the relay
// is marked unhealthy every new session goes to TCP-WS, and when the last
// datagram session has ended nothing can arrive any more: the mark stayed
// until the QUIC connection was replaced. Measured on an OpenWrt router
// (2026-09-30): three LAN broadcasts to nobody marked it unhealthy, and new
// UDP flows were still on TCP-WS five minutes later.
//
// So while unhealthy, one new session per relayTrialInterval is let through.
// ==========================================================================

// fakeRelayClock is a settable clock for quicUDPMux.now.
type fakeRelayClock struct{ at atomic.Int64 }

func (c *fakeRelayClock) now() time.Duration      { return time.Duration(c.at.Load()) }
func (c *fakeRelayClock) set(d time.Duration)     { c.at.Store(int64(d)) }
func (c *fakeRelayClock) advance(d time.Duration) { c.at.Add(int64(d)) }

// unhealthyMux returns a mux marked unhealthy at clock time zero.
func unhealthyMux(t *testing.T) (*quicUDPMux, *fakeRelayClock) {
	t.Helper()
	clock := &fakeRelayClock{}
	mux := &quicUDPMux{now: clock.now}
	for i := int32(0); i < relayTimeoutThreshold; i++ {
		mux.recordTimeout()
	}
	if mux.datagramRelayHealthy() {
		t.Fatal("mux should be unhealthy after relayTimeoutThreshold timeouts")
	}
	return mux, clock
}

func TestQuicUDPMux_AdmitSession_HealthyAdmitsEverySession(t *testing.T) {
	mux := &quicUDPMux{}
	for i := 0; i < 5; i++ {
		if admitted, trial := mux.admitSession(); !admitted || trial {
			t.Fatalf("session %d on a healthy mux: admitted=%v trial=%v, want admitted, not a trial", i, admitted, trial)
		}
	}
}

func TestQuicUDPMux_AdmitSession_UnhealthyAdmitsOnePerInterval(t *testing.T) {
	mux, clock := unhealthyMux(t)

	for _, at := range []time.Duration{0, time.Second, relayTrialInterval - time.Millisecond} {
		clock.set(at)
		if admitted, _ := mux.admitSession(); admitted {
			t.Fatalf("session admitted %v after the relay was marked unhealthy, before the first trial is due", at)
		}
	}

	clock.set(relayTrialInterval)
	if admitted, trial := mux.admitSession(); !admitted || !trial {
		t.Fatalf("at the interval: admitted=%v trial=%v, want a trial session", admitted, trial)
	}
	if admitted, _ := mux.admitSession(); admitted {
		t.Fatal("a second session was admitted in the same interval")
	}

	clock.set(2*relayTrialInterval - time.Millisecond)
	if admitted, _ := mux.admitSession(); admitted {
		t.Fatal("a session was admitted before the next trial was due")
	}
	clock.set(2 * relayTrialInterval)
	if admitted, trial := mux.admitSession(); !admitted || !trial {
		t.Fatalf("one interval after the first trial: admitted=%v trial=%v, want a trial session", admitted, trial)
	}
}

// Every flow a router carries dials through here; the trial slot must go to
// exactly one of them.
func TestQuicUDPMux_AdmitSession_OneTrialUnderContention(t *testing.T) {
	mux, clock := unhealthyMux(t)
	clock.set(relayTrialInterval)

	var admitted atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if ok, _ := mux.admitSession(); ok {
				admitted.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := admitted.Load(); got != 1 {
		t.Fatalf("%d sessions admitted in one interval, want 1", got)
	}
}

func TestQuicUDPMux_AdmitSession_AnsweredTrialHeals(t *testing.T) {
	mux, clock := unhealthyMux(t)
	clock.set(relayTrialInterval)
	if admitted, _ := mux.admitSession(); !admitted {
		t.Fatal("trial session refused")
	}

	mux.recordRecv() // the trial session's reply

	for i := 0; i < 5; i++ {
		if admitted, trial := mux.admitSession(); !admitted || trial {
			t.Fatalf("session %d after the trial was answered: admitted=%v trial=%v, want admitted, not a trial", i, admitted, trial)
		}
	}
}

// A trial to a destination that never answers (it is one more silent peer, or
// the path really is broken) leaves the relay unhealthy and the schedule
// where it was.
func TestQuicUDPMux_AdmitSession_UnansweredTrialKeepsSchedule(t *testing.T) {
	mux, clock := unhealthyMux(t)
	clock.set(relayTrialInterval)
	if admitted, _ := mux.admitSession(); !admitted {
		t.Fatal("trial session refused")
	}

	clock.advance(relayTrialInterval / 2)
	mux.recordTimeout() // the trial session's idle deadline

	if admitted, _ := mux.admitSession(); admitted {
		t.Fatal("a session was admitted right after a failed trial")
	}
	clock.set(2 * relayTrialInterval)
	if admitted, trial := mux.admitSession(); !admitted || !trial {
		t.Fatalf("one interval after the failed trial: admitted=%v trial=%v, want a trial session", admitted, trial)
	}
}

// Sends shed by the datagram budget count as relay timeouts too, and arrive in
// bursts. They must not keep pushing the first trial away.
func TestQuicUDPMux_AdmitSession_FurtherTimeoutsDoNotPostponeTrial(t *testing.T) {
	mux, clock := unhealthyMux(t)

	for at := time.Second; at < relayTrialInterval; at += time.Second {
		clock.set(at)
		mux.recordTimeout()
	}

	clock.set(relayTrialInterval)
	if admitted, trial := mux.admitSession(); !admitted || !trial {
		t.Fatalf("at the interval: admitted=%v trial=%v, want a trial session", admitted, trial)
	}
}

// A relay that went unhealthy, healed and went unhealthy again waits a full
// interval again: the first episode's schedule is not reused.
func TestQuicUDPMux_AdmitSession_SecondEpisodeWaitsAFullInterval(t *testing.T) {
	mux, clock := unhealthyMux(t)
	clock.set(relayTrialInterval)
	mux.admitSession()
	mux.recordRecv()

	clock.set(10 * relayTrialInterval)
	for i := int32(0); i < relayTimeoutThreshold; i++ {
		mux.recordTimeout()
	}
	clock.advance(time.Second)
	if admitted, _ := mux.admitSession(); admitted {
		t.Fatal("a session was admitted one second into the second unhealthy episode")
	}
	clock.set(11 * relayTrialInterval)
	if admitted, trial := mux.admitSession(); !admitted || !trial {
		t.Fatalf("one interval into the second episode: admitted=%v trial=%v, want a trial session", admitted, trial)
	}
}

// End to end over a real QUIC pair: refused, then the trial session goes out
// as a datagram session, its echo comes back, and the relay is healthy again.
func TestQUICClient_DialUDP_RelayUnhealthy_TrialSessionHeals(t *testing.T) {
	client, server, cleanup := setupQUICPair(t)
	defer cleanup()
	echoCtx, stopEcho := context.WithCancel(context.Background())
	defer stopEcho()
	go server.ServeUDP(echoCtx, UDPHandlerFunc(func(conn net.PacketConn, addr string) {
		buf := make([]byte, 512)
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			return
		}
		_, _ = conn.WriteTo(buf[:n], nil)
	}))
	time.Sleep(50 * time.Millisecond) // let ServeUDP install the handler

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := client.connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	client.mu.Lock()
	mux := client.udpMux
	client.mu.Unlock()
	clock := &fakeRelayClock{}
	mux.now = clock.now
	for i := int32(0); i < relayTimeoutThreshold; i++ {
		mux.recordTimeout()
	}

	if _, err := client.DialUDP(ctx, "8.8.8.8:53"); !errors.Is(err, ErrDatagramRelayUnhealthy) {
		t.Fatalf("DialUDP before the trial is due = %v, want ErrDatagramRelayUnhealthy", err)
	}

	clock.set(relayTrialInterval)
	trial, err := client.DialUDP(ctx, "8.8.8.8:53")
	if err != nil {
		t.Fatalf("trial DialUDP: %v", err)
	}
	defer trial.Close()
	if _, ok := trial.(*quicUDPConn); !ok {
		t.Fatalf("trial session is a %T, want a datagram session", trial)
	}
	if _, err := client.DialUDP(ctx, "8.8.8.8:53"); !errors.Is(err, ErrDatagramRelayUnhealthy) {
		t.Fatalf("DialUDP while the trial is unanswered = %v, want ErrDatagramRelayUnhealthy", err)
	}

	roundTrip(t, trial, "trial")

	if !mux.datagramRelayHealthy() {
		t.Fatal("relay still unhealthy after the trial session was answered")
	}
	next, err := client.DialUDP(ctx, "9.9.9.9:53")
	if err != nil {
		t.Fatalf("DialUDP after the trial was answered: %v", err)
	}
	next.Close()
}
