package wire

import "sync/atomic"

// ConnDeviceID is implemented by transport conns that know which
// authenticated device the parent connection belongs to. Consumers
// (server.ProxyHandler) use an optional type assertion — absence of the
// interface (or an empty string) means "unattributed" and must be tolerated.
type ConnDeviceID interface {
	DeviceID() string
}

// deviceIDHolder is a connection-scoped device identity slot, written once
// by the metadata-stream handler and read by every proxy stream of that
// connection. Reads may race the write (streams can open before metadata
// lands); an empty read is a valid "not yet attributed" answer.
type deviceIDHolder struct {
	v atomic.Value // string
}

func (h *deviceIDHolder) set(udid string) { h.v.Store(udid) }

func (h *deviceIDHolder) get() string {
	s, _ := h.v.Load().(string)
	return s
}
