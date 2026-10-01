package core

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestAppStats_RecordTakeFormat(t *testing.T) {
	var s appStats
	direct := NewDirectOutbound(nil)
	proxy := NewWireOutbound("test", newMockDialer(), nil)

	for i := 0; i < 3; i++ {
		s.record("Doubao Browser Helper", direct, "1.1.1.1:443", nil)
	}
	s.record("curl", proxy, "2.2.2.2:443", nil)
	s.record("curl", proxy, "3.3.3.3:443", errors.New("boom"))
	s.record("curl", direct, "4.4.4.4:80", errors.New("unreachable"))
	s.record("ads", nil, "5.5.5.5:443", nil) // reject route
	s.record("", direct, "6.6.6.6:443", nil) // unattributed: not counted

	rows := s.take()
	if len(rows) != 3 {
		t.Fatalf("rows = %+v, want 3 processes", rows)
	}
	// Busiest first; ties broken by name.
	if rows[0].Process != "Doubao Browser Helper" || rows[1].Process != "curl" || rows[2].Process != "ads" {
		t.Fatalf("order = %q, %q, %q", rows[0].Process, rows[1].Process, rows[2].Process)
	}
	if got := rows[0]; got.Direct != 3 || got.Proxy != 0 || got.Fail != 0 || got.LastFail != "" {
		t.Errorf("Doubao row = %+v", got)
	}
	if got := rows[1]; got.Direct != 1 || got.Proxy != 2 || got.Fail != 2 || got.LastFail != "direct/4.4.4.4:80" {
		t.Errorf("curl row = %+v", got)
	}
	if got := rows[2]; got.Reject != 1 || got.Direct != 0 || got.Proxy != 0 {
		t.Errorf("ads row = %+v", got)
	}

	want := `Doubao Browser Helper: direct=3 proxy=0 fail=0; curl: direct=1 proxy=2 fail=2 lastFail=direct/4.4.4.4:80; ads: direct=0 proxy=0 reject=1 fail=0`
	if got := FormatAppRouteStats(rows, 12); got != want {
		t.Errorf("format:\n got %s\nwant %s", got, want)
	}
	if got := FormatAppRouteStats(rows, 1); got != `Doubao Browser Helper: direct=3 proxy=0 fail=0; +2 more` {
		t.Errorf("truncated format = %s", got)
	}

	// take() starts a new window.
	if again := s.take(); len(again) != 0 {
		t.Errorf("second take = %+v, want empty", again)
	}
}

func TestAppStats_BoundedTable(t *testing.T) {
	var s appStats
	direct := NewDirectOutbound(nil)
	for i := 0; i < appStatsCap+50; i++ {
		s.record(fmt.Sprintf("proc-%04d", i), direct, "1.1.1.1:443", nil)
	}
	rows := s.take()
	if len(rows) != appStatsCap+1 {
		t.Fatalf("rows = %d, want cap %d + one overflow row", len(rows), appStatsCap)
	}
	var overflow *AppRouteStat
	for i := range rows {
		if rows[i].Process == appStatsOverflow {
			overflow = &rows[i]
		}
	}
	if overflow == nil || overflow.Direct != 50 {
		t.Fatalf("overflow row = %+v, want 50 folded connections", overflow)
	}
	if !strings.HasPrefix(FormatAppRouteStats(rows, 1), `(other): direct=50`) {
		t.Errorf("overflow row should sort first: %s", FormatAppRouteStats(rows, 1))
	}
}
