package wire

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"net"
	"testing"
)

// TestProbeECHBlock verifies the no-ECH control probe: it completes against a
// live TLS server (the "no-ECH flow got through" half of the asymmetry) and
// fails against an unreachable port (so a down node is not a false positive).
func TestProbeECHBlock(t *testing.T) {
	pair, err := GenerateCertPair()
	if err != nil {
		t.Fatal(err)
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{pair.EC},
		MinVersion:   tls.VersionTLS13,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_ = c.(*tls.Conn).Handshake()
				c.Close()
			}()
		}
	}()

	host, port, _ := net.SplitHostPort(ln.Addr().String())
	if !ProbeECHBlock(context.Background(), host, port, "example.com", nil) {
		t.Error("probe should succeed against a live TLS server")
	}

	// Unreachable port → false (node down is not an ECH-block signal).
	if ProbeECHBlock(context.Background(), "127.0.0.1", "1", "example.com", nil) {
		t.Error("probe should fail against a closed port")
	}
}

func TestEchPublicName(t *testing.T) {
	_, echConfig, err := GenerateECHKey("cloudflare-ech.com")
	if err != nil {
		t.Fatal(err)
	}
	b64 := base64.StdEncoding.EncodeToString(echConfig)
	if got := echPublicName(b64); got != "cloudflare-ech.com" {
		t.Errorf("echPublicName = %q, want cloudflare-ech.com", got)
	}
	if got := echPublicName("!!!not-base64!!!"); got != "" {
		t.Errorf("echPublicName(bad input) = %q, want empty", got)
	}
}
