package server

import (
	"crypto/tls"
	"testing"

	"github.com/kaitu-io/k2/wire"
)

// echConfig builds a minimal raw ECHConfig ([version:2][length:2][config_id:1]...)
// carrying the given config_id, enough for ECHConfigIDFromConfig to read it.
func echConfigWithID(id uint8) []byte {
	return []byte{0xfe, 0x0d, 0x00, 0x10, id, 0x00, 0x20}
}

// TestServer_echDisposition_ConfigIDGate is the §3.7 fix: a ClientHello whose
// plaintext outer ECH config_id is not one this node owns (GREASE, forged, or
// foreign) must route to camouflage — NOT to handleK2V5, where a bare
// Handshake() would complete TLS and expose the self-signed cert to a
// config-free active prober.
func TestServer_echDisposition_ConfigIDGate(t *testing.T) {
	srv := newTestServer(t)
	srv.TLSConfig.EncryptedClientHelloKeys = []tls.EncryptedClientHelloKey{
		{Config: echConfigWithID(0x2a)},
	}
	srv.camouflageName = "cloudflare-ech.com"

	tests := []struct {
		name string
		info wire.ClientHelloInfo
		want echRoute
	}{
		{"own config_id → k2v5", wire.ClientHelloInfo{HasECH: true, ECHOuter: true, ECHConfigID: 0x2a}, echToK2V5},
		{"foreign config_id → camouflage", wire.ClientHelloInfo{HasECH: true, ECHOuter: true, ECHConfigID: 0x99}, echToCamouflage},
		{"GREASE / no outer config_id → camouflage", wire.ClientHelloInfo{HasECH: true, ECHOuter: false}, echToCamouflage},
	}
	for _, tc := range tests {
		if got := srv.echDisposition(tc.info); got != tc.want {
			t.Errorf("%s: echDisposition = %d, want %d", tc.name, got, tc.want)
		}
	}

	// A foreign config_id with no camouflage target configured closes rather
	// than falling through to the k2v5 handler.
	srv.camouflageName = ""
	if got := srv.echDisposition(wire.ClientHelloInfo{HasECH: true, ECHOuter: true, ECHConfigID: 0x99}); got != echReject {
		t.Errorf("no camouflage target: echDisposition = %d, want echReject", got)
	}
}

func TestServer_ownsECHConfigID(t *testing.T) {
	srv := newTestServer(t)
	srv.TLSConfig.EncryptedClientHelloKeys = []tls.EncryptedClientHelloKey{
		{Config: echConfigWithID(0x2a)},
		{Config: echConfigWithID(0x2b)}, // rotation: multiple active configs
	}
	if !srv.ownsECHConfigID(0x2a) || !srv.ownsECHConfigID(0x2b) {
		t.Error("should own both active config_ids 0x2a and 0x2b")
	}
	if srv.ownsECHConfigID(0x99) {
		t.Error("should not own foreign config_id 0x99")
	}
}
