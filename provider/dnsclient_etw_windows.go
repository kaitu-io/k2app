//go:build windows

package provider

import (
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The trace-session half of system-resolver attribution. See dnsclient_etw.go
// for why this exists and what event it reads.
//
// One private real-time session, one provider, one consumer goroutine parked
// in ProcessTrace. Creating a session needs administrator rights; the desktop
// daemon runs as SYSTEM. Without them start() fails, the watch stays off, and
// DNS attribution is exactly what it was before this file existed.

// dnsClientSessionName is fixed so a daemon that died without stopping its
// session can find and stop the orphan on the next start (sessions outlive
// their creating process).
const dnsClientSessionName = "k2-dns-client"

// dnsClientFlushMs is how often the kernel hands buffered events to the
// consumer. The stock granularity is one SECOND — useless here, the wire
// query is a millisecond behind the event — so the session asks for
// millisecond units (EVENT_TRACE_USE_MS_FLUSH_TIMER).
const dnsClientFlushMs = 1

const (
	etwWnodeFlagTracedGUID       = 0x00020000
	etwRealTimeMode              = 0x00000100
	etwUseMsFlushTimer           = 0x00000010
	etwControlStop               = 1
	etwEnableProvider            = 1
	etwLevelInformation          = 4
	etwProcessTraceRealTime      = 0x00000100
	etwProcessTraceEventRecord   = 0x10000000
	etwInvalidProcessTraceHandle = ^uint64(0)
	etwErrAlreadyExists          = 183
)

// {1C95126E-7EEA-49A9-A3FE-A378B03DDB4D} Microsoft-Windows-DNS-Client
var dnsClientProviderGUID = windows.GUID{
	Data1: 0x1c95126e, Data2: 0x7eea, Data3: 0x49a9,
	Data4: [8]byte{0xa3, 0xfe, 0xa3, 0x78, 0xb0, 0x3d, 0xdb, 0x4d},
}

var (
	advapi32           = windows.NewLazySystemDLL("advapi32.dll")
	procStartTraceW    = advapi32.NewProc("StartTraceW")
	procControlTraceW  = advapi32.NewProc("ControlTraceW")
	procEnableTraceEx2 = advapi32.NewProc("EnableTraceEx2")
	procOpenTraceW     = advapi32.NewProc("OpenTraceW")
	procProcessTrace   = advapi32.NewProc("ProcessTrace")
	procCloseTrace     = advapi32.NewProc("CloseTrace")
)

// activeDNSClientTable is where the ETW callback writes. The callback is a
// process-wide C entry point (windows.NewCallback slots are never freed, so
// it is created once), hence the indirection instead of a closure.
var (
	activeDNSClientTable  atomic.Pointer[dnsClientTable]
	dnsClientCallback     uintptr
	dnsClientCallbackOnce sync.Once
	// dnsClientLagNs is the last event's emit→callback delay, for the
	// real-kernel test and the start log line.
	dnsClientLagNs atomic.Int64
)

func dnsClientEventCallback(rec *etwEventRecord) uintptr {
	if rec == nil || rec.EventID != dnsClientQueryEventID || rec.UserData == nil {
		return 0
	}
	t := activeDNSClientTable.Load()
	if t == nil {
		return 0
	}
	data := unsafe.Slice((*byte)(rec.UserData), int(rec.UserDataLength))
	if qname := parseDNSClientQueryName(data); qname != "" {
		t.record(qname, rec.ProcessID)
		// FILETIME epoch (1601) → Unix epoch, 100 ns units.
		emitted := time.Unix(0, (rec.TimeStamp-116444736000000000)*100)
		dnsClientLagNs.Store(int64(time.Since(emitted)))
	}
	return 0
}

// dnsClientWatch is one running trace session.
type dnsClientWatch struct {
	table   *dnsClientTable
	session uint64
	trace   uint64
	done    chan struct{}
}

// dnsClientProps builds the EVENT_TRACE_PROPERTIES block for our session.
// The buffer is []uint64 so it is 8-aligned like the struct it holds.
func dnsClientProps(name []uint16) ([]uint64, *etwTraceProperties) {
	size := int(unsafe.Sizeof(etwTraceProperties{})) + len(name)*2
	buf := make([]uint64, (size+7)/8)
	p := (*etwTraceProperties)(unsafe.Pointer(&buf[0]))
	p.WnodeBufferSize = uint32(len(buf) * 8)
	p.WnodeFlags = etwWnodeFlagTracedGUID
	p.ClientContext = 2 // system time: event timestamps comparable to time.Now
	p.LoggerNameOffset = uint32(unsafe.Sizeof(etwTraceProperties{}))
	return buf, p
}

func stopDNSClientSession(session uint64, name []uint16) {
	buf, _ := dnsClientProps(name)
	var namePtr uintptr
	if session == 0 {
		namePtr = uintptr(unsafe.Pointer(&name[0]))
	}
	procControlTraceW.Call(uintptr(session), namePtr, uintptr(unsafe.Pointer(&buf[0])), etwControlStop)
}

func startDNSClientWatch(table *dnsClientTable) (*dnsClientWatch, error) {
	name, err := windows.UTF16FromString(dnsClientSessionName)
	if err != nil {
		return nil, err
	}
	var session uint64
	for attempt := 0; ; attempt++ {
		buf, p := dnsClientProps(name)
		p.LogFileMode = etwRealTimeMode | etwUseMsFlushTimer
		p.FlushTimer = dnsClientFlushMs
		p.BufferSize = 16 // KB; DNS events are tiny and rare
		p.MinimumBuffers = 4
		p.MaximumBuffers = 16
		r, _, _ := procStartTraceW.Call(
			uintptr(unsafe.Pointer(&session)),
			uintptr(unsafe.Pointer(&name[0])),
			uintptr(unsafe.Pointer(&buf[0])))
		if r == 0 {
			break
		}
		if r == etwErrAlreadyExists && attempt == 0 {
			stopDNSClientSession(0, name) // orphan from a crashed daemon
			continue
		}
		return nil, fmt.Errorf("StartTrace: %w", windows.Errno(r))
	}

	r, _, _ := procEnableTraceEx2.Call(
		uintptr(session),
		uintptr(unsafe.Pointer(&dnsClientProviderGUID)),
		etwEnableProvider, etwLevelInformation,
		0, 0, 0, 0)
	if r != 0 {
		stopDNSClientSession(session, name)
		return nil, fmt.Errorf("EnableTraceEx2: %w", windows.Errno(r))
	}

	dnsClientCallbackOnce.Do(func() {
		dnsClientCallback = windows.NewCallback(dnsClientEventCallback)
	})
	activeDNSClientTable.Store(table)
	logfile := etwTraceLogfile{
		LoggerName:          &name[0],
		ProcessTraceMode:    etwProcessTraceRealTime | etwProcessTraceEventRecord,
		EventRecordCallback: dnsClientCallback,
	}
	h, _, callErr := procOpenTraceW.Call(uintptr(unsafe.Pointer(&logfile)))
	if uint64(h) == etwInvalidProcessTraceHandle {
		activeDNSClientTable.Store(nil)
		stopDNSClientSession(session, name)
		return nil, fmt.Errorf("OpenTrace: %w", callErr)
	}

	w := &dnsClientWatch{table: table, session: session, trace: uint64(h), done: make(chan struct{})}
	go func() {
		defer close(w.done)
		// Blocks until the session stops; events arrive on this thread.
		trace := w.trace
		if r, _, _ := procProcessTrace.Call(uintptr(unsafe.Pointer(&trace)), 1, 0, 0); r != 0 {
			slog.Warn("DIAG: dns-client-watch-ended", "err", windows.Errno(r).Error())
		}
	}()
	return w, nil
}

func (w *dnsClientWatch) stop() {
	name, _ := windows.UTF16FromString(dnsClientSessionName)
	stopDNSClientSession(w.session, name)
	procCloseTrace.Call(uintptr(w.trace))
	select {
	case <-w.done:
	case <-time.After(3 * time.Second):
	}
	activeDNSClientTable.CompareAndSwap(w.table, nil)
}

// windowsSystemResolver is the image the Dnscache service runs in. Every
// svchost instance shares the name; only one of them sends DNS, and a
// non-Dnscache svchost that happens to send port-53 traffic simply finds no
// matching event and stays svchost.
const windowsSystemResolver = "svchost.exe"

// StartDNSClientWatch implements DNSClientSearcher.
func (s *WindowsProcessSearcher) StartDNSClientWatch() (func(), error) {
	if err := s.holdDNSClientWatch(); err != nil {
		return nil, err
	}
	var once sync.Once
	return func() { once.Do(s.releaseDNSClientWatch) }, nil
}

func (s *WindowsProcessSearcher) holdDNSClientWatch() error {
	s.dnsMu.Lock()
	defer s.dnsMu.Unlock()
	if s.dnsWatch == nil {
		w, err := startDNSClientWatch(newDNSClientTable())
		if err != nil {
			return err
		}
		s.dnsWatch = w
	}
	s.dnsHolds++
	return nil
}

func (s *WindowsProcessSearcher) releaseDNSClientWatch() {
	s.dnsMu.Lock()
	s.dnsHolds--
	var w *dnsClientWatch
	if s.dnsHolds == 0 {
		w, s.dnsWatch = s.dnsWatch, nil
	}
	s.dnsMu.Unlock()
	if w != nil {
		w.stop()
	}
}

// FindDNSClient implements DNSClientSearcher.
func (s *WindowsProcessSearcher) FindDNSClient(resolverName, qname string) (string, string, bool) {
	if resolverName != windowsSystemResolver {
		return "", "", false
	}
	arrived := time.Now()
	s.dnsMu.Lock()
	w := s.dnsWatch
	s.dnsMu.Unlock()
	if w == nil {
		return "", "", false
	}
	pid, ok := w.table.await(qname, arrived, dnsClientWait)
	if !ok || pid == s.selfPID {
		return "", "", false
	}
	name, path := s.identForPID(pid)
	if name == "" {
		return "", "", false
	}
	return name, path, true
}
