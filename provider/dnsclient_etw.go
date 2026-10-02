package provider

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf16"
	"unsafe"
)

// Windows system-resolver attribution, portable half.
//
// An app that resolves through the system resolver (getaddrinfo, DnsQuery —
// i.e. nearly every native app) never sends its own DNS packet: the Dnscache
// service does, from svchost.exe, and the socket table has no "on behalf of"
// field (macOS has so_e_pid; Windows has nothing). The only place the
// requesting process is visible is the DNS client's own ETW provider:
// Microsoft-Windows-DNS-Client event 3006 ("query called") is logged from
// dnsapi.dll INSIDE the calling process, so its header pid is the app and its
// payload leads with the query name. dnsClientTable is the short-lived
// qname → pid index those events feed; dnsclient_etw_windows.go runs the
// trace session.
//
// This file is build-tag-free on purpose (same reason as
// process_wintable.go): the struct layouts, the payload parser, and the table
// are all testable on any OS. Keep it free of Windows API imports.

// dnsClientQueryEventID is DNS-Client event 3006: DnsQuery* entered, before
// any cache lookup. It fires on cache hits too; 3009 (query sent on the
// wire) is logged by svchost and so carries the wrong pid.
const dnsClientQueryEventID = 3006

const (
	// dnsClientTTL bounds how long a "process X asked for name N" record may
	// attribute a wire query for N. The wire query follows the event within
	// about a millisecond; anything much older is a different lookup that
	// Dnscache answered from cache.
	dnsClientTTL = 3 * time.Second
	// dnsClientCap bounds the table. Entries are swept on insert once the
	// cap is reached.
	dnsClientCap = 2048
	// dnsClientWait is how long a wire query waits for its event. ETW hands
	// events to a real-time consumer in batches; with the millisecond flush
	// timer the measured delay is a steady 26–30 ms (Win11 25H2, 80 lookups),
	// and it does not go lower — flush periods of 1 ms and 10 ms measure the
	// same. So the event for a query ALWAYS lands after the query itself has
	// reached the tunnel, and attribution has to wait for it.
	dnsClientWait = 60 * time.Millisecond
	// dnsClientFresh is how far before "now" a record may have been made and
	// still count as this query's own event, with no waiting. It is not a
	// clock-skew allowance: the DNS handler serves one flow's queries one at
	// a time, so when the resolver sends A and AAAA for a name down the same
	// socket, the second is looked at only after the first's upstream round
	// trip — by which time the event they share is that much old. Without
	// this the second query would sit out the full dnsClientWait for an event
	// that already came.
	dnsClientFresh = 500 * time.Millisecond
)

type dnsClientEntry struct {
	pid uint32
	at  time.Time
}

// dnsClientTable maps a normalized query name to the process that most
// recently asked the system resolver for it.
//
// Two processes asking for the same name inside the TTL is a real ambiguity:
// Dnscache coalesces them into one wire query and nothing says which caller
// it serves. Latest caller wins.
type dnsClientTable struct {
	mu sync.Mutex
	m  map[string]dnsClientEntry
	// changed is closed and replaced on every record, waking await.
	changed chan struct{}
	// lagNs is the latest event's logged→delivered delay. See slow.
	lagNs atomic.Int64
	ttl   time.Duration
	cap   int
	now   func() time.Time
}

func newDNSClientTable() *dnsClientTable {
	return &dnsClientTable{
		m:       make(map[string]dnsClientEntry),
		changed: make(chan struct{}),
		ttl:     dnsClientTTL,
		cap:     dnsClientCap,
		now:     time.Now,
	}
}

// normalizeDNSClientName is the join key between the ETW payload and the
// wire query: lowercase, no trailing dot.
func normalizeDNSClientName(qname string) string {
	return strings.ToLower(strings.TrimSuffix(qname, "."))
}

func (t *dnsClientTable) record(qname string, pid uint32) {
	key := normalizeDNSClientName(qname)
	if key == "" || pid == 0 {
		return
	}
	now := t.now()
	t.mu.Lock()
	if len(t.m) >= t.cap {
		t.sweepLocked(now)
	}
	if len(t.m) < t.cap {
		t.m[key] = dnsClientEntry{pid: pid, at: now}
		close(t.changed)
		t.changed = make(chan struct{})
	}
	t.mu.Unlock()
}

// noteLag records how late the latest event was delivered.
func (t *dnsClientTable) noteLag(d time.Duration) { t.lagNs.Store(int64(d)) }

// slow reports that events are arriving later than a query is willing to
// wait — a Windows build that ignores the millisecond flush timer delivers
// once a SECOND. Waiting then buys nothing and would tax every system DNS
// lookup by the full dnsClientWait, so await stops waiting until delivery is
// fast again (each event re-measures).
func (t *dnsClientTable) slow() bool { return t.lagNs.Load() > int64(dnsClientWait) }

// await returns the process whose lookup produced the wire query that
// arrived at `since`. It prefers a record made around or after `since` (see
// dnsClientFresh) — the event trails the query (see dnsClientWait), so that
// one is this query's own — and waits up to timeout for it. Only when none shows up does it
// settle for an older live record: a retransmission of a query whose event
// was delivered the first time round.
func (t *dnsClientTable) await(qname string, since time.Time, timeout time.Duration) (uint32, bool) {
	key := normalizeDNSClientName(qname)
	if key == "" {
		return 0, false
	}
	if t.slow() {
		timeout = 0
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		t.mu.Lock()
		e, ok := t.m[key]
		changed := t.changed
		t.mu.Unlock()
		if ok && !e.at.Before(since.Add(-dnsClientFresh)) {
			return e.pid, true
		}
		select {
		case <-changed:
		case <-deadline.C:
			return t.lookup(qname)
		}
	}
}

func (t *dnsClientTable) sweepLocked(now time.Time) {
	for k, e := range t.m {
		if now.Sub(e.at) > t.ttl {
			delete(t.m, k)
		}
	}
}

func (t *dnsClientTable) lookup(qname string) (uint32, bool) {
	key := normalizeDNSClientName(qname)
	t.mu.Lock()
	e, ok := t.m[key]
	t.mu.Unlock()
	if !ok || t.now().Sub(e.at) > t.ttl {
		return 0, false
	}
	return e.pid, true
}

// etwEventRecord mirrors EVENT_RECORD (evntcons.h) on 64-bit Windows, which
// is the only Windows k2 builds for. Only the fields the callback reads are
// named.
type etwEventRecord struct {
	_              [12]byte // Size, HeaderType, Flags, EventProperty, ThreadId
	ProcessID      uint32
	TimeStamp      int64    // FILETIME, 100 ns since 1601
	_              [16]byte // ProviderId
	EventID        uint16
	_              [38]byte // rest of EVENT_DESCRIPTOR, ProcessorTime, ActivityId
	_              [4]byte  // ETW_BUFFER_CONTEXT
	_              uint16   // ExtendedDataCount
	UserDataLength uint16
	_              unsafe.Pointer // ExtendedData
	UserData       unsafe.Pointer
	_              unsafe.Pointer // UserContext
}

// etwTraceLogfile mirrors EVENT_TRACE_LOGFILEW on 64-bit Windows.
type etwTraceLogfile struct {
	LogFileName         *uint16
	LoggerName          *uint16
	CurrentTime         int64
	BuffersRead         uint32
	ProcessTraceMode    uint32
	_                   [88]byte  // EVENT_TRACE CurrentEvent
	_                   [280]byte // TRACE_LOGFILE_HEADER LogfileHeader
	BufferCallback      uintptr
	BufferSize          uint32
	Filled              uint32
	EventsLost          uint32
	_                   uint32
	EventRecordCallback uintptr
	IsKernelTrace       uint32
	_                   uint32
	Context             uintptr
}

// etwGUID is a Windows GUID without importing x/sys/windows here.
type etwGUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

// etwTraceProperties mirrors EVENT_TRACE_PROPERTIES (with its leading
// WNODE_HEADER) on 64-bit Windows. The session name is written right after
// it, at LoggerNameOffset.
type etwTraceProperties struct {
	// WNODE_HEADER
	WnodeBufferSize uint32
	_               uint32 // ProviderId
	_               uint64 // HistoricalContext
	_               int64  // TimeStamp
	_               etwGUID
	ClientContext   uint32
	WnodeFlags      uint32
	// EVENT_TRACE_PROPERTIES
	BufferSize        uint32
	MinimumBuffers    uint32
	MaximumBuffers    uint32
	_                 uint32 // MaximumFileSize
	LogFileMode       uint32
	FlushTimer        uint32
	_                 uint32 // EnableFlags
	_                 int32  // AgeLimit
	_                 [6]uint32
	_                 uintptr // LoggerThreadId
	LogFileNameOffset uint32
	LoggerNameOffset  uint32
}

// parseDNSClientQueryName extracts QueryName from an event-3006 payload: the
// first field, a NUL-terminated UTF-16LE string. Returns "" for a payload
// with no terminator (truncated / not the layout we know) — an unparseable
// event must be dropped, never guessed at.
func parseDNSClientQueryName(data []byte) string {
	units := make([]uint16, 0, 64)
	for i := 0; i+1 < len(data); i += 2 {
		u := uint16(data[i]) | uint16(data[i+1])<<8
		if u == 0 {
			return string(utf16.Decode(units))
		}
		units = append(units, u)
	}
	return ""
}
