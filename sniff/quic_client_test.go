package sniff

import "testing"

func TestQUICClient_IsBrowser(t *testing.T) {
	tests := []struct {
		client   QUICClient
		isBrowser bool
	}{
		{QUICClientChromium, true},
		{QUICClientFirefox, true},
		{QUICClientSafari, true},
		{QUICClientQUICGo, false},
		{QUICClientUnknown, false},
		{"", false},
	}
	for _, tt := range tests {
		if got := tt.client.IsBrowser(); got != tt.isBrowser {
			t.Errorf("QUICClient(%q).IsBrowser() = %v, want %v", tt.client, got, tt.isBrowser)
		}
	}
}

func TestIdentifyQUICClient_Firefox(t *testing.T) {
	// Firefox: single CRYPTO frame
	frameTypes := []uint8{0x06}
	client := identifyQUICClient(frameTypes, nil)
	if client != QUICClientFirefox {
		t.Errorf("single CRYPTO → %q, want %q", client, QUICClientFirefox)
	}
}

func TestIdentifyQUICClient_Chromium(t *testing.T) {
	// Chromium: multiple CRYPTO frames
	frameTypes := []uint8{0x06, 0x06}
	client := identifyQUICClient(frameTypes, nil)
	if client != QUICClientChromium {
		t.Errorf("multi CRYPTO → %q, want %q", client, QUICClientChromium)
	}

	// Chromium: CRYPTO + PING
	frameTypes2 := []uint8{0x06, 0x01}
	client2 := identifyQUICClient(frameTypes2, nil)
	if client2 != QUICClientChromium {
		t.Errorf("CRYPTO+PING → %q, want %q", client2, QUICClientChromium)
	}
}

func TestIdentifyQUICClient_QUICGo_ByFingerprint(t *testing.T) {
	// quic-go: has renegotiation_info extension (0xFF01)
	ch := &QUICClientHello{
		Extensions: []uint16{0x0000, 0x000a, 0xFF01},
	}
	frameTypes := []uint8{0x06}
	client := identifyQUICClient(frameTypes, ch)
	if client != QUICClientQUICGo {
		t.Errorf("renegotiation_info → %q, want %q", client, QUICClientQUICGo)
	}
}

func TestIdentifyQUICClient_QUICGo_ByKyber(t *testing.T) {
	// quic-go: has X25519Kyber768Draft00 curve (0x6399)
	ch := &QUICClientHello{
		EllipticCurves: []uint16{0x001d, 0x6399},
	}
	frameTypes := []uint8{0x06}
	client := identifyQUICClient(frameTypes, ch)
	if client != QUICClientQUICGo {
		t.Errorf("Kyber curve → %q, want %q", client, QUICClientQUICGo)
	}
}

func TestIdentifyQUICClient_QUICGo_PINGBeforeCRYPTO(t *testing.T) {
	// quic-go alternative: PING(s) followed by CRYPTO at end
	frameTypes := []uint8{0x01, 0x06}
	client := identifyQUICClient(frameTypes, nil)
	if client != QUICClientQUICGo {
		t.Errorf("PING+CRYPTO → %q, want %q", client, QUICClientQUICGo)
	}
}

func TestIdentifyQUICClient_Safari(t *testing.T) {
	// Safari: single CRYPTO + specific fingerprint (GREASE versions + curves)
	ch := &QUICClientHello{
		CipherSuites:   []uint16{0x1301, 0x1302},
		Versions:       []uint16{0x0304, 0x6A6A}, // TLS 1.3 + GREASE
		EllipticCurves: []uint16{0x001d, 0x0017, 0x0018, 0x7A7A, 0x0019}, // curves + GREASE
	}
	frameTypes := []uint8{0x06}
	client := identifyQUICClient(frameTypes, ch)
	if client != QUICClientSafari {
		t.Errorf("Safari fingerprint → %q, want %q", client, QUICClientSafari)
	}
}

func TestIdentifyQUICClient_Empty(t *testing.T) {
	client := identifyQUICClient(nil, nil)
	if client != QUICClientUnknown {
		t.Errorf("empty → %q, want %q", client, QUICClientUnknown)
	}
}

func TestIdentifyQUICClient_NoCRYPTO(t *testing.T) {
	frameTypes := []uint8{0x01} // only PING
	client := identifyQUICClient(frameTypes, nil)
	if client != QUICClientUnknown {
		t.Errorf("no CRYPTO → %q, want %q", client, QUICClientUnknown)
	}
}

func TestIsGREASE(t *testing.T) {
	greaseValues := []uint16{0x0A0A, 0x1A1A, 0x2A2A, 0x6A6A, 0x7A7A, 0xDADA, 0xFAFA}
	for _, v := range greaseValues {
		if !isGREASE(v) {
			t.Errorf("0x%04x should be GREASE", v)
		}
	}
	nonGrease := []uint16{0x0000, 0x0001, 0x001d, 0x0304, 0x1301}
	for _, v := range nonGrease {
		if isGREASE(v) {
			t.Errorf("0x%04x should NOT be GREASE", v)
		}
	}
}
