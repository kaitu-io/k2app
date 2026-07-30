package server

import (
	"sync"

	"github.com/kaitu-io/k2/wire"
)

// DeviceDelta is the pending (un-reported) byte delta for one device.
type DeviceDelta struct {
	Rx, Tx int64
}

// DeviceKey identifies one accounting bucket: device + auth session (spec
// §6.6 会话可观测). Session is "" for legacy/unattributed traffic — the
// zero key remains the legal "unattributed" bucket.
type DeviceKey struct {
	UDID    string
	Session string
}

// deviceTraffic accumulates per-device byte deltas between reporter cycles.
// Semantics: entries are pending deltas — Take() drains the map, so memory is
// bounded by "devices active since the last report", not lifetime actives.
type deviceTraffic struct {
	mu sync.Mutex
	m  map[DeviceKey]*DeviceDelta
}

// Add accumulates a delta for key. Zero deltas are dropped so idle
// connections never materialize entries. The zero key is the legal
// "unattributed" bucket (old clients / raw smux without metadata).
func (d *deviceTraffic) Add(key DeviceKey, rx, tx int64) {
	if rx == 0 && tx == 0 {
		return
	}
	d.mu.Lock()
	// concurrency:allow — pure in-memory map op, no I/O under lock
	defer d.mu.Unlock()
	if d.m == nil {
		d.m = make(map[DeviceKey]*DeviceDelta)
	}
	e, ok := d.m[key]
	if !ok {
		e = &DeviceDelta{}
		d.m[key] = e
	}
	e.Rx += rx
	e.Tx += tx
}

// Take atomically drains all pending deltas.
func (d *deviceTraffic) Take() map[DeviceKey]DeviceDelta {
	d.mu.Lock()
	// concurrency:allow — pure in-memory map swap
	defer d.mu.Unlock()
	out := make(map[DeviceKey]DeviceDelta, len(d.m))
	for k, v := range d.m {
		out[k] = *v
	}
	d.m = nil
	return out
}

// connDeviceKey extracts the attributed device + session via optional
// assertions. Missing interfaces or empty values → zero fields (unattributed).
func connDeviceKey(c any) DeviceKey {
	var k DeviceKey
	if dc, ok := c.(wire.ConnDeviceID); ok {
		k.UDID = dc.DeviceID()
	}
	if sc, ok := c.(wire.ConnSessionID); ok {
		k.Session = sc.SessionID()
	}
	return k
}
