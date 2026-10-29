// Package sniff QUIC client identification.
// Adapted from github.com/sagernet/sing-box/common/sniff/quic.go (BSD 3-Clause License).
package sniff

// QUICClient identifies the QUIC implementation sending an Initial packet.
type QUICClient string

const (
	QUICClientChromium QUICClient = "chromium"
	QUICClientFirefox  QUICClient = "firefox"
	QUICClientSafari   QUICClient = "safari"
	QUICClientQUICGo   QUICClient = "quic-go"
	QUICClientUnknown  QUICClient = "unknown"
)

// IsBrowser returns true if the client is a known browser QUIC implementation.
func (c QUICClient) IsBrowser() bool {
	return c == QUICClientChromium || c == QUICClientFirefox || c == QUICClientSafari
}

// identifyQUICClient determines the QUIC client from frame type sequence and
// parsed ClientHello fingerprint.
// frameTypes is the ordered list of non-padding frame types from the QUIC Initial.
// ch may be nil if ClientHello parsing failed.
func identifyQUICClient(frameTypes []uint8, ch *QUICClientHello) QUICClient {
	if len(frameTypes) == 0 {
		return QUICClientUnknown
	}

	// Count CRYPTO frames and track positions
	cryptoCount := 0
	hasPing := false
	for _, ft := range frameTypes {
		switch ft {
		case 0x06: // CRYPTO
			cryptoCount++
		case 0x01: // PING
			hasPing = true
		}
	}

	if cryptoCount == 0 {
		return QUICClientUnknown
	}

	// 1. Check JA3 fingerprint first (takes priority over frame structure).

	// quic-go: has specific TLS fingerprint markers (renegotiation_info, Kyber curve).
	if ch != nil && isQUICGo(ch) {
		return QUICClientQUICGo
	}

	// Safari: CRYPTO-only frame structure + specific JA3 fingerprint.
	if cryptoCount == 1 && !hasPing && ch != nil && isSafariFingerprint(ch) {
		return QUICClientSafari
	}

	// 2. Frame structure heuristics (when JA3 didn't match above).

	// quic-go alternative: CRYPTO is last frame, preceded by PING(s).
	if len(frameTypes) > 1 && frameTypes[len(frameTypes)-1] == 0x06 {
		allPing := true
		for _, ft := range frameTypes[:len(frameTypes)-1] {
			if ft != 0x01 {
				allPing = false
				break
			}
		}
		if allPing {
			return QUICClientQUICGo
		}
	}

	// Chromium: multiple CRYPTO frames, or PING frame present.
	if cryptoCount > 1 || hasPing {
		return QUICClientChromium
	}

	// Firefox: single CRYPTO frame, no PING, no distinctive JA3.
	if cryptoCount == 1 && !hasPing {
		return QUICClientFirefox
	}

	return QUICClientUnknown
}

// isSafariFingerprint checks Safari-specific TLS ClientHello patterns.
// Safari uses either:
// 1. GREASE versions (len=2) + GREASE curves (len=5)
// 2. Single AES_256_GCM (0x1302) + single X25519 (0x001d) + single ECDSA_P256_SHA256 (0x0403)
func isSafariFingerprint(ch *QUICClientHello) bool {
	// Pattern 1: GREASE versions + GREASE curves
	if len(ch.Versions) == 2 && len(ch.EllipticCurves) == 5 {
		greaseVersions := 0
		for _, v := range ch.Versions {
			if isGREASE(v) {
				greaseVersions++
			}
		}
		greaseCurves := 0
		for _, c := range ch.EllipticCurves {
			if isGREASE(c) {
				greaseCurves++
			}
		}
		if greaseVersions >= 1 && greaseCurves >= 1 {
			return true
		}
	}

	// Pattern 2: minimal cipher/curve/sigalg set
	if len(ch.CipherSuites) >= 1 && len(ch.EllipticCurves) == 1 && len(ch.SignatureAlgorithms) == 1 {
		hasAES256GCM := false
		for _, cs := range ch.CipherSuites {
			if cs == 0x1302 { // TLS_AES_256_GCM_SHA384
				hasAES256GCM = true
				break
			}
		}
		if hasAES256GCM && ch.EllipticCurves[0] == 0x001d && ch.SignatureAlgorithms[0] == 0x0403 {
			return true
		}
	}

	return false
}

// isQUICGo checks quic-go specific TLS fingerprint markers:
// 1. X25519Kyber768Draft00 curve (0x6399, formerly 0x11EC pre-draft)
// 2. renegotiation_info extension (0xFF01)
func isQUICGo(ch *QUICClientHello) bool {
	for _, curve := range ch.EllipticCurves {
		if curve == 0x6399 || curve == 0x11EC { // X25519Kyber768Draft00
			return true
		}
	}
	for _, ext := range ch.Extensions {
		if ext == 0xFF01 { // renegotiation_info
			return true
		}
	}
	return false
}

// isGREASE checks if a TLS value is a GREASE (Generate Random Extensions And
// Sustain Extensibility) value. GREASE values have the pattern 0x?A?A.
func isGREASE(val uint16) bool {
	return val&0x0F0F == 0x0A0A
}
