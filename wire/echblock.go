package wire

import (
	"context"
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"strconv"
	"time"

	"github.com/kaitu-io/k2/safego"
)

// probeECHBlockTimeout bounds the no-ECH control probe. It runs fire-and-forget
// off the race's failure path, so this never delays reconnect.
const probeECHBlockTimeout = 4 * time.Second

// ProbeECHBlock is a leading indicator for GFW ECH-presence filtering. It dials
// host:port and attempts a plain TLS 1.3 handshake WITHOUT the ECH extension,
// using sni (the node's ECH public_name) so a healthy node reverse-proxies it to
// the real public_name site and the handshake completes.
//
// The signal is an ASYMMETRY, evaluated by the caller: all ECH-bearing transport
// candidates failed at the handshake layer, yet a no-ECH TLS to the SAME IP
// still completes. That points at a censor dropping ECH-bearing flows
// specifically — not at an unreachable node (then this probe also fails) and not
// at an auth rejection (then ECH established fine; the caller gates that out).
//
// Returns true iff the no-ECH handshake completed.
func ProbeECHBlock(ctx context.Context, host, port, sni string, directDialer DirectDialer) bool {
	ctx, cancel := context.WithTimeout(ctx, probeECHBlockTimeout)
	defer cancel()

	addr := net.JoinHostPort(host, port)
	var conn net.Conn
	var err error
	if directDialer != nil {
		conn, err = directDialer.DialContext(ctx, "tcp", addr)
	} else {
		conn, err = (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return false // IP unreachable — not ECH-specific.
	}
	defer conn.Close()

	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	// InsecureSkipVerify is deliberate and safe here: this is a reachability
	// probe that sends NO application data and reads NO response body — it only
	// observes whether the TLS handshake completes at all. Verifying a peer is
	// meaningless (a healthy node reverse-proxies this to the real public_name
	// site, whose cert we neither control nor need), and the whole point is to
	// detect whether the network path passes a no-ECH flow. Never send secrets
	// or trust the peer on this connection.
	tc := tls.Client(conn, &tls.Config{
		ServerName:         sni,
		MinVersion:         tls.VersionTLS13,
		InsecureSkipVerify: true, //nolint:gosec // reachability probe only; no data sent/read
	})
	return tc.HandshakeContext(ctx) == nil
}

// echPublicName extracts the ECH config's public_name from the base64 ech= value.
// Empty string on any parse failure (the caller then skips the probe).
func echPublicName(echB64 string) string {
	raw, err := ParseECHConfig(echB64)
	if err != nil || len(raw) == 0 {
		return ""
	}
	tmpl, err := ParseECHConfigBytes(raw)
	if err != nil || tmpl == nil {
		return ""
	}
	return tmpl.PublicName
}

// maybeProbeECHBlock fires the no-ECH control probe off the race-failure path
// (background, non-blocking) when the failures look like ECH-transport failures
// rather than an auth rejection, and emits DIAG: ech-block-suspected on the
// asymmetry. joined is the combined candidate error (used to gate out auth).
func maybeProbeECHBlock(cfg Config, directDialer DirectDialer, joined error) {
	if cfg.ECH == "" {
		return
	}
	// ErrMetadataRejected means a candidate got past the TLS/ECH handshake to
	// auth — ECH is not being blocked.
	if errors.Is(joined, ErrMetadataRejected) {
		return
	}
	sni := echPublicName(cfg.ECH)
	if sni == "" {
		return
	}
	host, port := cfg.Host, strconv.Itoa(cfg.Port)
	safego.Go(func() {
		if ProbeECHBlock(context.Background(), host, port, sni, directDialer) {
			slog.Warn("DIAG: ech-block-suspected",
				"host", host, "sni", sni,
				"hint", "all ECH transports failed but a no-ECH TLS to the same IP completed")
		}
	})
}
