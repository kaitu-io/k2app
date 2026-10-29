package wire

import "sync/atomic"

// ConnDeviceID is implemented by transport conns that know which
// authenticated device the parent connection belongs to. Consumers
// (server.ProxyHandler) use an optional type assertion — absence of the
// interface (or an empty string) means "unattributed" and must be tolerated.
type ConnDeviceID interface {
	DeviceID() string
}

// ConnSessionID is implemented by transport conns that know the server-issued
// auth session id (hex, 32 chars) of the parent connection. Optional type
// assertion; absence or "" means "no session" and must be tolerated (legacy
// clients, permissive-unauthenticated connections).
type ConnSessionID interface {
	SessionID() string
}

// deviceIDHolder is a connection-scoped device identity slot, written once
// by the metadata-stream handler and read by every proxy stream of that
// connection. Reads may race the write (streams can open before metadata
// lands); an empty read is a valid "not yet attributed" answer.
type deviceIDHolder struct {
	v    atomic.Value // string: udid
	sess atomic.Value // string: hex session id
}

func (h *deviceIDHolder) set(udid string) { h.v.Store(udid) }

func (h *deviceIDHolder) get() string {
	s, _ := h.v.Load().(string)
	return s
}

func (h *deviceIDHolder) setSession(sid string) { h.sess.Store(sid) }

func (h *deviceIDHolder) session() string {
	s, _ := h.sess.Load().(string)
	return s
}
