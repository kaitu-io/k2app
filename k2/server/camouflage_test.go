package server

import (
	"testing"
	"time"

	"github.com/kaitu-io/k2/config"
)

// The camouflage whitelist is the node's fourth egress path, and the only one
// that never passes the device-auth admission gate: a non-ECH connection whose
// SNI is trusted gets its raw TCP relayed to <SNI>:443 (handleRawTCP). The
// whitelist is populated from the outer SNI of successful ECH connections —
// which the *client* chooses. Without a pin, anyone holding this node's ECH
// config (it ships in the k2v5:// URL, so every current and former user has it,
// including revoked ones) can teach the node any string and then use it as an
// open TCP relay — reaching targets the authenticated proxy path refuses,
// because ReverseProxyWithTimeout has no isReservedIP check.

func newCamouflageTestServer(camouflage string) *Server {
	return &Server{
		trustedSNIs:    make(map[string]time.Time),
		camouflageName: camouflage,
	}
}

// TestLearnOuterSNI_RefusesForeignSNI is the security regression: an attacker
// picks the outer SNI, so anything but the node's own camouflage domain must be
// refused entry to the relay whitelist.
func TestLearnOuterSNI_RefusesForeignSNI(t *testing.T) {
	s := newCamouflageTestServer("cloudflare-ech.com")

	for _, sni := range []string{
		"10.0.0.5",        // private network behind the node
		"127.0.0.1",       // the node itself
		"169.254.169.254", // cloud metadata
		"victim.example",  // arbitrary third party — bandwidth theft + misattribution
	} {
		s.learnOuterSNI(sni)
		if s.isTrustedSNI(sni) {
			t.Errorf("SECURITY REGRESSION: %q entered the relay whitelist — the node is an open TCP relay to %s:443", sni, sni)
		}
	}
}

// TestLearnOuterSNI_AcceptsCamouflageName proves the pin costs legitimate
// camouflage nothing: uTLS forces a real client's outer SNI to the ECH config's
// public_name (spec §3.1), which is exactly what the node expects.
func TestLearnOuterSNI_AcceptsCamouflageName(t *testing.T) {
	s := newCamouflageTestServer("cloudflare-ech.com")

	s.learnOuterSNI("cloudflare-ech.com")
	if !s.isTrustedSNI("cloudflare-ech.com") {
		t.Fatal("the node's own camouflage domain must be trusted — probers would otherwise get a closed connection instead of the cover site")
	}

	// SNI comparison is case-insensitive per DNS.
	s2 := newCamouflageTestServer("Cloudflare-ECH.com")
	s2.learnOuterSNI("cloudflare-ech.com")
	if !s2.isTrustedSNI("cloudflare-ech.com") {
		t.Error("outer SNI matching must be case-insensitive")
	}
}

// TestLearnOuterSNI_NoCamouflageNameLearnsNothing covers the degenerate config
// (no ECH public_name resolved): fail closed — no whitelist, no relay.
func TestLearnOuterSNI_NoCamouflageNameLearnsNothing(t *testing.T) {
	s := newCamouflageTestServer("")
	s.learnOuterSNI("anything.example")
	if s.isTrustedSNI("anything.example") {
		t.Error("with no camouflage domain configured the whitelist must stay empty")
	}
}

// TestNew_NoValidatorIsRefused: device auth is always enforced, so a static-TLS
// config with no users_file, no remote_url and no cert_dir (hence no
// provisioned auth.json) has nothing to check a credential against. It must
// fail at startup, not fall back to admitting any two non-empty strings.
func TestNew_NoValidatorIsRefused(t *testing.T) {
	cfg := &config.ServerConfig{Listen: "127.0.0.1:0"}
	if _, err := New(cfg); err == nil {
		t.Fatal("a config with no auth backend must be refused, not downgraded to accept-all")
	}
}
