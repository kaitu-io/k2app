package engine

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kaitu-io/k2/config"
)

// fixedTime returns a deterministic clock for tests.
func fixedTime() func() time.Time {
	t := time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC)
	return func() time.Time { return t }
}

func newTestReporter(t *testing.T, cfg config.RuleMissConfig) *missReporter {
	t.Helper()
	r := newMissReporter(cfg)
	r.nowFn = fixedTime()
	return r
}

func TestExtractETLDPlus1(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"mail.google.com", "google.com"},
		{"google.com", "google.com"},
		{"store.example.co.uk", "example.co.uk"},
		{"example.co.uk", "example.co.uk"},
		{"a.b.c.github.io", "c.github.io"}, // github.io is a public suffix; c.github.io is the registrable part
		{"localhost", ""},
		{"", ""},
		{"example.com.", "example.com"}, // trailing dot stripped
	}
	for _, tt := range tests {
		if got := extractETLDPlus1(tt.in); got != tt.want {
			t.Errorf("extractETLDPlus1(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestHashETLD1_Deterministic(t *testing.T) {
	a := hashETLD1("2026-04-11", "google.com")
	b := hashETLD1("2026-04-11", "google.com")
	if a != b {
		t.Errorf("hashETLD1 not deterministic: %q vs %q", a, b)
	}
	if len(a) != 16 {
		t.Errorf("hashETLD1 length = %d, want 16", len(a))
	}
	// Different salt → different hash.
	c := hashETLD1("2026-04-12", "google.com")
	if a == c {
		t.Errorf("hashETLD1 salt not mixed: %q == %q", a, c)
	}
	// Different etld1 → different hash.
	d := hashETLD1("2026-04-11", "bing.com")
	if a == d {
		t.Errorf("hashETLD1 etld1 not mixed: %q == %q", a, d)
	}
}

func TestWeekBucket(t *testing.T) {
	// ISO week for 2026-04-11 is W15.
	got := weekBucket(time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC))
	if got != "2026-W15" {
		t.Errorf("weekBucket = %q, want 2026-W15", got)
	}
}

func TestNormalizeCountry(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"US", "US"},
		{"us", "US"},
		{"CN", "CN"},
		{"", "XX"},
		{"U", "XX"},
		{"USA", "XX"},
		{"U1", "XX"},
	}
	for _, tt := range tests {
		if got := normalizeCountry(tt.in); got != tt.want {
			t.Errorf("normalizeCountry(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestMissReporter_DropOnFull verifies the non-blocking channel send and
// drop counter increment.
func TestMissReporter_DropOnFull(t *testing.T) {
	r := newTestReporter(t, config.RuleMissConfig{Enabled: true, ClientVersion: "test"})
	// Fill the ring buffer without starting the drain loop.
	for i := 0; i < missRingCapacity; i++ {
		r.Report("a.example.com", "tcp")
	}
	// Next Report must drop.
	before := r.drops.Load()
	r.Report("b.example.com", "tcp")
	if got := r.drops.Load(); got <= before {
		t.Errorf("expected drop counter increment, got %d (was %d)", got, before)
	}
}

// TestMissReporter_Dedupe verifies the drain loop collapses duplicate
// (etld1, protocol) pairs and that the uploaded batch has exactly one
// record per unique key.
func TestMissReporter_Dedupe(t *testing.T) {
	var (
		mu      sync.Mutex
		batches []ruleMissBatch
		done    = make(chan struct{})
	)
	r := newTestReporter(t, config.RuleMissConfig{
		Enabled:       true,
		ClientVersion: "0.4.3",
		Country:       "US",
	})
	r.flushTrigger = make(chan struct{}, 1)
	r.sendFn = func(batch ruleMissBatch) error {
		mu.Lock()
		batches = append(batches, batch)
		mu.Unlock()
		close(done)
		return nil
	}
	r.start()
	defer r.Stop()

	// Send 5 events across 2 unique (etld1, proto) pairs.
	r.Report("mail.google.com", "tcp") // google.com|tcp
	r.Report("www.google.com", "tcp")  // google.com|tcp (dup)
	r.Report("maps.google.com", "udp") // google.com|udp
	r.Report("bing.com", "tcp")        // bing.com|tcp
	r.Report("www.bing.com", "tcp")    // bing.com|tcp (dup)

	// Wait briefly for events to land in the drain loop's map, then
	// trigger an immediate flush.
	time.Sleep(50 * time.Millisecond)
	r.flushTrigger <- struct{}{}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("send callback not invoked")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(batches) != 1 {
		t.Fatalf("expected 1 batch, got %d", len(batches))
	}
	batch := batches[0]
	if batch.SchemaVersion != phase1SchemaVersion {
		t.Errorf("schema_version = %d, want %d", batch.SchemaVersion, phase1SchemaVersion)
	}
	if batch.ClientVersion != "0.4.3" {
		t.Errorf("client_version = %q, want 0.4.3", batch.ClientVersion)
	}
	if batch.SaltDay != "2026-04-11" {
		t.Errorf("salt_day = %q, want 2026-04-11", batch.SaltDay)
	}
	if len(batch.Records) != 3 {
		t.Errorf("expected 3 deduped records, got %d", len(batch.Records))
	}
	for _, rec := range batch.Records {
		if len(rec.Hash16) != 16 {
			t.Errorf("hash16 length = %d, want 16", len(rec.Hash16))
		}
		if rec.Country != "US" {
			t.Errorf("country = %q, want US", rec.Country)
		}
		if rec.WeekBucket != "2026-W15" {
			t.Errorf("week_bucket = %q, want 2026-W15", rec.WeekBucket)
		}
		if rec.Revealed != "" {
			t.Errorf("revealed should be empty in Phase 1, got %q", rec.Revealed)
		}
	}
}

// TestMissReporter_SizeFlush verifies that reaching missBatchMaxRecords
// triggers an automatic flush.
func TestMissReporter_SizeFlush(t *testing.T) {
	var flushed atomic.Bool
	done := make(chan struct{})
	r := newTestReporter(t, config.RuleMissConfig{
		Enabled:       true,
		ClientVersion: "0.4.3",
	})
	r.sendFn = func(batch ruleMissBatch) error {
		if len(batch.Records) < missBatchMaxRecords {
			t.Errorf("flush triggered with only %d records", len(batch.Records))
		}
		if flushed.CompareAndSwap(false, true) {
			close(done)
		}
		return nil
	}
	r.start()
	defer r.Stop()

	// Queue exactly missBatchMaxRecords unique etld1 values. We build
	// synthetic "tld-N.example" — each has a distinct etld1, forcing no
	// dedupe collisions.
	for i := 0; i < missBatchMaxRecords; i++ {
		r.Report(synthDomain(i), "tcp")
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("size-based flush did not fire")
	}
}

// synthDomain returns a host whose ETLD+1 is unique per index.
func synthDomain(i int) string {
	// We use a .com domain — publicsuffix treats .com as a TLD, so the
	// ETLD+1 becomes "hostN.com".
	return "leaf.host" + itoa(i) + ".com"
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	n := len(buf)
	for i > 0 {
		n--
		buf[n] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[n:])
}
