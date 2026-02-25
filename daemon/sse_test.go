package daemon

import (
	"strings"
	"testing"
	"time"

	"github.com/kaitu-io/k2/engine"
)

func TestBroadcastStatus_SendsToClients(t *testing.T) {
	d := New()
	ch := make(chan string, 8)
	d.sseMu.Lock()
	d.sseClients[ch] = struct{}{}
	d.sseMu.Unlock()

	d.OnStatus(engine.Status{State: engine.StateConnecting})

	select {
	case msg := <-ch:
		if !strings.Contains(msg, "connecting") {
			t.Fatalf("expected connecting in msg, got: %s", msg)
		}
		if !strings.Contains(msg, "event: status") {
			t.Fatalf("expected SSE event format, got: %s", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for broadcast")
	}
}

func TestBroadcastStatus_SlowClientDrop(t *testing.T) {
	d := New()
	// Small buffer — will overflow.
	ch := make(chan string, 1)
	d.sseMu.Lock()
	d.sseClients[ch] = struct{}{}
	d.sseMu.Unlock()

	// Fire 20 rapid status changes — should not block or panic.
	for i := 0; i < 20; i++ {
		d.OnStatus(engine.Status{State: engine.StateConnecting})
	}

	// Read whatever is available.
	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			goto done
		}
	}
done:
	if count == 0 {
		t.Fatal("expected at least one event delivered")
	}
	// Buffer is 1, so at most 1 event should be buffered (rest dropped).
	if count > 1 {
		t.Fatalf("expected at most 1 buffered event, got %d", count)
	}
}

func TestBroadcastStatus_MultipleClients(t *testing.T) {
	d := New()
	ch1 := make(chan string, 8)
	ch2 := make(chan string, 8)
	d.sseMu.Lock()
	d.sseClients[ch1] = struct{}{}
	d.sseClients[ch2] = struct{}{}
	d.sseMu.Unlock()

	d.OnStatus(engine.Status{State: engine.StateConnected, ConnectedAt: time.Now()})

	for _, ch := range []chan string{ch1, ch2} {
		select {
		case msg := <-ch:
			if !strings.Contains(msg, "connected") {
				t.Fatalf("expected connected in msg, got: %s", msg)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for broadcast")
		}
	}
}

func TestBroadcastStatus_NoClientsNoPanic(t *testing.T) {
	d := New()
	// No clients subscribed — should not panic.
	d.OnStatus(engine.Status{State: engine.StateDisconnected})
}
