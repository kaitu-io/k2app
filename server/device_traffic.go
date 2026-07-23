package server

import (
	"sync"

	"github.com/kaitu-io/k2/wire"
)

// DeviceDelta is the pending (un-reported) byte delta for one device.
type DeviceDelta struct {
	Rx, Tx int64
}

// deviceTraffic accumulates per-device byte deltas between reporter cycles.
// Semantics: entries are pending deltas — Take() drains the map, so memory is
// bounded by "devices active since the last report", not lifetime actives.
type deviceTraffic struct {
	mu sync.Mutex
	m  map[string]*DeviceDelta
}

// Add accumulates a delta for udid. Zero deltas are dropped so idle
// connections never materialize entries. The empty udid is the legal
// "unattributed" bucket (old clients / raw smux without metadata).
func (d *deviceTraffic) Add(udid string, rx, tx int64) {
	if rx == 0 && tx == 0 {
		return
	}
	d.mu.Lock()
	// concurrency:allow — pure in-memory map op, no I/O under lock
	defer d.mu.Unlock()
	if d.m == nil {
		d.m = make(map[string]*DeviceDelta)
	}
	e, ok := d.m[udid]
	if !ok {
		e = &DeviceDelta{}
		d.m[udid] = e
	}
	e.Rx += rx
	e.Tx += tx
}

// Take atomically drains all pending deltas.
func (d *deviceTraffic) Take() map[string]DeviceDelta {
	d.mu.Lock()
	// concurrency:allow — pure in-memory map swap
	defer d.mu.Unlock()
	out := make(map[string]DeviceDelta, len(d.m))
	for k, v := range d.m {
		out[k] = *v
	}
	d.m = nil
	return out
}

// connDeviceID extracts the attributed device id from a proxy-side conn via
// optional assertion. Missing interface or empty value → "" (unattributed).
func connDeviceID(c any) string {
	if dc, ok := c.(wire.ConnDeviceID); ok {
		return dc.DeviceID()
	}
	return ""
}
