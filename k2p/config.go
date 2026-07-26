package k2p

import (
	"crypto/tls"

	"github.com/kaitu-io/k2/wire"
)

// Default TURN server URL (used when TURNURLs is empty).
const DefaultTURNURL = "turn:turn.kaitu.io:3478"

// Config holds all parameters for a k2p connection.
type Config struct {
	// HomeUDID is the UDID of the home peer to connect to (away) or self (home).
	HomeUDID string

	// SignalServer is the URL of the central API server.
	SignalServer string

	// Token is the JWT for authentication with signal server + k2v5 data plane.
	Token string

	// WireCfg is the k2v5 wire config for the data plane after hole punch.
	WireCfg wire.Config

	// TLSCert is the TLS certificate for the home side to accept QUIC connections.
	// If nil, GenerateCertPair() creates an ephemeral self-signed cert.
	// For persistent identity, load from disk and pass here.
	// Away side must use insecure=1 or pin= to trust this cert.
	TLSCert *tls.Certificate

	// ICE/TURN configuration (populated from signal lookup).
	TURNURLs     []string
	TURNUsername string
	TURNPassword string

	// PeerSecret is the shared secret for peer authentication (Away proves identity).
	// Never sent to k2s signal server.
	PeerSecret string

	// Pin is the expected TLS certificate pin hash(es) for Home's self-signed cert.
	// Format: "sha256:<b64>,sha256:<b64>"
	Pin string

	// Networks are Home's LAN CIDRs (e.g. ["192.168.1.0/24"]).
	Networks []string

	// DNS is Home's LAN DNS server (e.g. "192.168.1.1").
	DNS string

	// RegToken is the registration token for signal server auth (Home only).
	RegToken string

	// SecretHash is the bcrypt hash of PeerSecret (Home only, for verification).
	SecretHash string
}
