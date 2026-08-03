package wire

// rawAddr is a net.Addr carrying an unresolved "host:port" taken verbatim
// from a UDP frame's addr field.
//
// The wire layer never resolves names: a client may address a session by
// domain (SNI upgrade rewrites dest to the sniffed hostname), so returning a
// *net.UDPAddr here would force a resolution the wire layer has no resolver
// for. The proxy handler resolves and caches instead.
type rawAddr string

func (a rawAddr) Network() string { return "udp" }
func (a rawAddr) String() string  { return string(a) }
