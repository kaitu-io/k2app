package provider

import (
	"testing"
	"time"
	"unsafe"
)

// The sizes and offsets below are the 64-bit Windows SDK layouts
// (evntrace.h / evntcons.h). They are asserted on every platform because the
// structs are handed to the kernel as raw memory: a drifted field does not
// fail to compile, it makes StartTrace reject the block or the callback read
// a neighbouring field.
func TestETWLayouts(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("layouts are the 64-bit ones")
	}
	var rec etwEventRecord
	var lf etwTraceLogfile
	var props etwTraceProperties
	checks := []struct {
		name      string
		got, want uintptr
	}{
		{"EVENT_RECORD size", unsafe.Sizeof(rec), 112},
		{"EVENT_RECORD.ProcessId", unsafe.Offsetof(rec.ProcessID), 12},
		{"EVENT_RECORD.TimeStamp", unsafe.Offsetof(rec.TimeStamp), 16},
		{"EVENT_RECORD.EventDescriptor.Id", unsafe.Offsetof(rec.EventID), 40},
		{"EVENT_RECORD.UserDataLength", unsafe.Offsetof(rec.UserDataLength), 86},
		{"EVENT_RECORD.UserData", unsafe.Offsetof(rec.UserData), 96},
		{"EVENT_TRACE_LOGFILEW size", unsafe.Sizeof(lf), 448},
		{"EVENT_TRACE_LOGFILEW.LoggerName", unsafe.Offsetof(lf.LoggerName), 8},
		{"EVENT_TRACE_LOGFILEW.ProcessTraceMode", unsafe.Offsetof(lf.ProcessTraceMode), 28},
		{"EVENT_TRACE_LOGFILEW.BufferCallback", unsafe.Offsetof(lf.BufferCallback), 400},
		{"EVENT_TRACE_LOGFILEW.EventRecordCallback", unsafe.Offsetof(lf.EventRecordCallback), 424},
		{"EVENT_TRACE_LOGFILEW.Context", unsafe.Offsetof(lf.Context), 440},
		{"EVENT_TRACE_PROPERTIES size", unsafe.Sizeof(props), 120},
		{"WNODE_HEADER.ClientContext", unsafe.Offsetof(props.ClientContext), 40},
		{"WNODE_HEADER.Flags", unsafe.Offsetof(props.WnodeFlags), 44},
		{"EVENT_TRACE_PROPERTIES.BufferSize", unsafe.Offsetof(props.BufferSize), 48},
		{"EVENT_TRACE_PROPERTIES.LogFileMode", unsafe.Offsetof(props.LogFileMode), 64},
		{"EVENT_TRACE_PROPERTIES.FlushTimer", unsafe.Offsetof(props.FlushTimer), 68},
		{"EVENT_TRACE_PROPERTIES.LogFileNameOffset", unsafe.Offsetof(props.LogFileNameOffset), 112},
		{"EVENT_TRACE_PROPERTIES.LoggerNameOffset", unsafe.Offsetof(props.LoggerNameOffset), 116},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %d, want %d", c.name, c.got, c.want)
		}
	}
}

func utf16le(s string, terminate bool) []byte {
	var b []byte
	for _, r := range s {
		b = append(b, byte(r), byte(r>>8))
	}
	if terminate {
		b = append(b, 0, 0)
	}
	return b
}

func TestParseDNSClientQueryName(t *testing.T) {
	// QueryName, then QueryType (uint32) and the rest of the 3006 payload.
	payload := append(utf16le("Api.Example.com", true), 0x01, 0, 0, 0, 0xAA, 0xBB)
	if got := parseDNSClientQueryName(payload); got != "Api.Example.com" {
		t.Errorf("got %q", got)
	}
	// No terminator: unknown layout, drop rather than guess.
	if got := parseDNSClientQueryName(utf16le("truncated.example", false)); got != "" {
		t.Errorf("unterminated payload parsed as %q, want empty", got)
	}
	if got := parseDNSClientQueryName(nil); got != "" {
		t.Errorf("nil payload parsed as %q", got)
	}
	// Odd trailing byte must not be read past.
	if got := parseDNSClientQueryName([]byte{'a'}); got != "" {
		t.Errorf("1-byte payload parsed as %q", got)
	}
}

func TestDNSClientTable(t *testing.T) {
	now := time.Unix(1000, 0)
	tb := newDNSClientTable()
	tb.now = func() time.Time { return now }

	tb.record("Api.Example.COM.", 42)
	if pid, ok := tb.lookup("api.example.com"); !ok || pid != 42 {
		t.Fatalf("lookup = %d,%v, want 42,true (case and trailing dot must not matter)", pid, ok)
	}
	if _, ok := tb.lookup("other.example.com"); ok {
		t.Fatal("unrelated name matched")
	}

	// Latest caller wins.
	tb.record("api.example.com", 77)
	if pid, _ := tb.lookup("api.example.com"); pid != 77 {
		t.Fatalf("pid = %d, want the latest caller 77", pid)
	}

	// Past the TTL the record must not attribute a later wire query.
	now = now.Add(dnsClientTTL + time.Millisecond)
	if _, ok := tb.lookup("api.example.com"); ok {
		t.Fatal("expired record still attributes")
	}

	// pid 0 / empty name are never recorded.
	tb.record("", 5)
	tb.record("zero.example.com", 0)
	if _, ok := tb.lookup("zero.example.com"); ok {
		t.Fatal("pid 0 recorded")
	}
}

func TestDNSClientTable_CapSweepsExpired(t *testing.T) {
	now := time.Unix(1000, 0)
	tb := newDNSClientTable()
	tb.now = func() time.Time { return now }
	tb.cap = 3

	tb.record("a.example", 1)
	tb.record("b.example", 2)
	tb.record("c.example", 3)
	// Full of live entries: the newcomer is dropped, nothing live is evicted.
	tb.record("d.example", 4)
	if _, ok := tb.lookup("d.example"); ok {
		t.Fatal("table grew past its cap")
	}
	if pid, ok := tb.lookup("a.example"); !ok || pid != 1 {
		t.Fatal("live entry evicted")
	}
	// Once the old ones expire the next insert sweeps them and fits.
	now = now.Add(dnsClientTTL + time.Millisecond)
	tb.record("e.example", 5)
	if pid, ok := tb.lookup("e.example"); !ok || pid != 5 {
		t.Fatal("insert after expiry did not fit")
	}
	if len(tb.m) != 1 {
		t.Fatalf("len = %d after sweep, want 1", len(tb.m))
	}
}

// The event for a query is delivered AFTER the query reaches the tunnel, so
// await must wait for it rather than answer from what is already there.
func TestDNSClientTable_AwaitWaitsForTheQuerysOwnEvent(t *testing.T) {
	tb := newDNSClientTable()
	// An older lookup of the same name by another process.
	tb.record("api.example.com", 11)
	time.Sleep(2 * dnsClientSkew)

	arrived := time.Now()
	go func() {
		time.Sleep(20 * time.Millisecond)
		tb.record("api.example.com", 22)
	}()
	pid, ok := tb.await("api.example.com", arrived, time.Second)
	if !ok || pid != 22 {
		t.Fatalf("await = %d,%v, want 22 (the event that followed the query), not the stale 11", pid, ok)
	}
	if waited := time.Since(arrived); waited > 500*time.Millisecond {
		t.Fatalf("await took %v — it must return as soon as the event lands", waited)
	}
}

// No fresh event (a retransmitted query): settle for the live older record
// once the wait is over.
func TestDNSClientTable_AwaitFallsBackToLiveRecord(t *testing.T) {
	tb := newDNSClientTable()
	tb.record("api.example.com", 11)
	time.Sleep(2 * dnsClientSkew)

	start := time.Now()
	pid, ok := tb.await("api.example.com", start, 30*time.Millisecond)
	if !ok || pid != 11 {
		t.Fatalf("await = %d,%v, want the older record 11", pid, ok)
	}
	if time.Since(start) < 30*time.Millisecond {
		t.Fatal("await returned the stale record without waiting for a fresh one")
	}
}

func TestDNSClientTable_AwaitMiss(t *testing.T) {
	tb := newDNSClientTable()
	start := time.Now()
	if _, ok := tb.await("nobody.example.com", start, 20*time.Millisecond); ok {
		t.Fatal("await matched a name nobody asked for")
	}
	// Unrelated records must wake the waiter without satisfying it.
	go tb.record("other.example.com", 5)
	if _, ok := tb.await("nobody.example.com", time.Now(), 20*time.Millisecond); ok {
		t.Fatal("await satisfied by a different name")
	}
}
