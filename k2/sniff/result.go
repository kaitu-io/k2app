package sniff

// Protocol identifiers produced by sniffing.
const (
	ProtoTLS        = "tls"
	ProtoHTTP       = "http"
	ProtoQUIC       = "quic"
	ProtoBitTorrent = "bittorrent"
	ProtoSTUN       = "stun"
	ProtoDTLS       = "dtls"
)

// Result holds protocol metadata from sniffing a connection's first bytes.
type Result struct {
	Protocol string // detected protocol constant, "" if unknown
	Domain   string // extracted domain (SNI, Host header), "" if none
}
