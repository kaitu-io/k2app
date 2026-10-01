//go:build windows

package provider

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// Real trace session against the real DNS client. Needs administrator
// rights (creating an ETW session); skipped — loudly — without them.
//
// What it pins that nothing portable can: that event 3006 is logged from the
// CALLING process (pid == ours), that its payload leads with the query name,
// and how long the kernel takes to deliver it. The delivery delay is the
// whole feasibility question: the wire query reaches the tunnel about a
// millisecond after the event is logged.
func TestRealKernelDNSClientWatch(t *testing.T) {
	tb := newDNSClientTable()
	w, err := startDNSClientWatch(tb)
	if err != nil {
		t.Skipf("cannot start an ETW session (needs administrator): %v", err)
	}
	defer w.stop()

	self := uint32(os.Getpid())
	var waits, lags []time.Duration
	for i := 0; i < 20; i++ {
		name := fmt.Sprintf("k2-etw-probe-%d-%d.example.com", self, i)
		start := time.Now()
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			(&net.Resolver{PreferGo: false}).LookupHost(ctx, name)
		}()
		var pid uint32
		var ok bool
		for time.Since(start) < 3*time.Second {
			if pid, ok = tb.lookup(name); ok {
				break
			}
			time.Sleep(100 * time.Microsecond)
		}
		if !ok {
			t.Fatalf("query %d: no event 3006 for %s within 3s", i, name)
		}
		if pid != self {
			t.Fatalf("query %d: event pid = %d, want our own pid %d", i, pid, self)
		}
		waits = append(waits, time.Since(start))
		lags = append(lags, time.Duration(tb.lagNs.Load()))
		time.Sleep(50 * time.Millisecond)
	}
	sort.Slice(waits, func(i, j int) bool { return waits[i] < waits[j] })
	sort.Slice(lags, func(i, j int) bool { return lags[i] < lags[j] })

	// The production entry point, end to end: a system-resolver query for a
	// name we just asked for comes back as OUR image name and path. selfPID
	// is zeroed because the searcher (rightly) ignores the daemon's own
	// lookups, and here the test process is the app.
	s := NewWindowsProcessSearcher()
	s.selfPID = 0
	s.dnsWatch = w
	exe, _ := os.Executable()
	name := fmt.Sprintf("k2-etw-find-%d.example.com", self)
	go (&net.Resolver{}).LookupHost(context.Background(), name)
	gotName, gotPath, ok := s.FindDNSClient(windowsSystemResolver, name)
	if !ok {
		t.Fatalf("FindDNSClient found no caller for %s", name)
	}
	if want := strings.ToLower(exe); gotPath != want || gotName != filepath.Base(want) {
		t.Fatalf("FindDNSClient = (%q, %q), want (%q, %q)", gotName, gotPath, filepath.Base(want), want)
	}

	t.Logf("lookup-start → visible in table: p50=%v p90=%v max=%v", waits[10], waits[18], waits[19])
	t.Logf("event logged → callback:        p50=%v p90=%v max=%v", lags[10], lags[18], lags[19])
}
