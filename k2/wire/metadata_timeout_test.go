package wire

// PVE S15 (2026-11-05): Center hangs instead of refusing. k2s's remote auth
// gives up after 5s and answers the code-less reject; the client's race and
// metadata deadlines were also 5s, so the race ended 2ms earlier with "all
// candidates failed or timed out" — read by the engine as node death, then
// fail(503). The race deadline now covers the transport step only, the
// metadata read outlives k2s's 5s, and a metadata reply that never comes is
// ErrMetadataTimeout (node alive, verdict stuck), never node death.

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

// slowValidator answers v after d, or as soon as release closes.
func slowValidator(d time.Duration, v AuthVerdict, release <-chan struct{}) Validator {
	return func(token, udid, mode string) AuthVerdict {
		select {
		case <-time.After(d):
		case <-release:
		}
		return v
	}
}

// raceLocalQUIC races against a local enforce QUIC server (the TCP-WS
// candidate is refused at TCP: nothing listens there).
func raceLocalQUIC(t *testing.T, validator Validator) (*RaceResult, error, time.Duration) {
	t.Helper()
	_, addr, cleanup := startRawQUICServer(t, validator, true)
	t.Cleanup(cleanup)
	cfg := Config{Host: "127.0.0.1", Port: addr.Port, Insecure: true, UDID: "d", Token: "t"}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	start := time.Now()
	res, err := RaceTransport(ctx, cfg, "", nil, nil)
	took := time.Since(start)
	if res != nil {
		t.Cleanup(func() {
			res.Dialer.Close()
			if res.QUICBackup != nil {
				res.QUICBackup.Close()
			}
			if res.TCPWS != nil && res.TCPWS != res.Dialer {
				res.TCPWS.Close()
			}
		})
	}
	return res, err, took
}

func TestMetaReadTimeoutOutlivesNodeAuthTimeout(t *testing.T) {
	if metaReadTimeout < 8*time.Second || metaReadTimeout <= remoteAuthClient.Timeout {
		t.Fatalf("metaReadTimeout %v must be ≥8s and outlive k2s remote auth %v", metaReadTimeout, remoteAuthClient.Timeout)
	}
	if raceTimeout+metaPhaseBudget() >= 15*time.Second {
		t.Fatalf("worst-case race %v must fit the engine's 15s race ctx", raceTimeout+metaPhaseBudget())
	}
}

// S15 reproduction: Center hangs, the node's own auth gives up at ~5s and
// answers code-less — after the race's transport deadline. The race must
// still report that code-less reject, not a bare timeout.
func TestRaceTransport_SlowCenterCodeLessRejectAfterTransportDeadline(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	_, err, took := raceLocalQUIC(t, slowValidator(5600*time.Millisecond, AuthVerdict{Code: -1}, release))
	if err == nil {
		t.Fatal("race succeeded; want the code-less reject")
	}
	var se *StreamError
	if !errors.Is(err, ErrMetadataRejected) || errors.As(err, &se) {
		t.Fatalf("err = %v (after %v), want the code-less ErrMetadataRejected", err, took)
	}
	if took < 5*time.Second {
		t.Fatalf("race ended after %v: the verdict came at 5.6s", took)
	}
}

// The transport deadline does not cut a slow but positive verdict.
func TestRaceTransport_SlowCenterOKWinsAfterTransportDeadline(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	res, err, took := raceLocalQUIC(t, slowValidator(5600*time.Millisecond, AuthVerdict{OK: true, UserID: 7}, release))
	if err != nil || res == nil {
		t.Fatalf("race = %v after %v, want the QUIC candidate (verdict OK at 5.6s)", err, took)
	}
}

// No verdict at all within metaReadTimeout: ErrMetadataTimeout, not a
// transport failure and not a reject.
func TestRaceTransport_MetadataNeverAnsweredIsMetadataTimeout(t *testing.T) {
	old := metaReadTimeout
	metaReadTimeout = time.Second
	t.Cleanup(func() { metaReadTimeout = old })
	release := make(chan struct{})
	defer close(release)
	_, err, took := raceLocalQUIC(t, slowValidator(time.Hour, AuthVerdict{Code: -1}, release))
	if !errors.Is(err, ErrMetadataTimeout) || errors.Is(err, ErrMetadataRejected) {
		t.Fatalf("err = %v (after %v), want ErrMetadataTimeout only", err, took)
	}
}

// The QUIC lazy connect (a user dial, no race) fails the same way.
func TestQUICClient_MetadataNeverAnsweredFailsConnect(t *testing.T) {
	old := metaReadTimeout
	metaReadTimeout = 500 * time.Millisecond
	t.Cleanup(func() { metaReadTimeout = old })
	release := make(chan struct{})
	defer close(release)
	_, addr, cleanup := startRawQUICServer(t, slowValidator(time.Hour, AuthVerdict{Code: -1}, release), true)
	defer cleanup()
	client := NewQUICClient(Config{Host: "127.0.0.1", Port: addr.Port, UDID: "d", Token: "t"}, nil)
	client.tlsConfig = buildInsecureTLSConfig()
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := client.DialTCP(ctx, "example.com:80", nil)
	if !errors.Is(err, ErrMetadataTimeout) || !isProxyLayerError(err) {
		t.Fatalf("err = %v, want ErrMetadataTimeout as a ProxyLayerError", err)
	}
}

func TestSendMetadata_ReadTimeoutIsMetadataTimeout(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	hold := make(chan struct{})
	defer close(hold)
	runFakeMetaServer(t, server, false, func(io.Writer) { <-hold }) // reads, never answers
	client.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	_, err := sendMetadata(context.Background(), fakeMetaTransport{stream: client}, MetadataFrame{UDID: "d", Token: "t"})
	if !errors.Is(err, ErrMetadataTimeout) || isMetadataReject(err) {
		t.Fatalf("err = %v, want ErrMetadataTimeout (not a reject)", err)
	}
}

// Dial evidence: a metadata timeout is no opinion, like a code-less reject —
// it neither records a failure nor clears one.
func TestConnectFailures_MetadataTimeoutNoOpinion(t *testing.T) {
	tm, d := newFailTM(errRefused)
	tm.DialTCP(context.Background(), "x:80", nil)
	seq0, last0, _ := cfTuple(tm)
	d.set(&ProxyLayerError{Err: errors.Join(ErrMetadataTimeout, errTimeout{})})
	tm.DialTCP(context.Background(), "x:80", nil)
	seq, last, _ := cfTuple(tm)
	if seq != seq0 || !last.Equal(last0) {
		t.Fatalf("metadata timeout changed evidence: seq %d→%d last %v→%v", seq0, seq, last0, last)
	}
}

// The TCP-WS twin of the lazy-connect case.
func TestTCPWSClient_MetadataNeverAnsweredFailsConnect(t *testing.T) {
	old := metaReadTimeout
	metaReadTimeout = 500 * time.Millisecond
	t.Cleanup(func() { metaReadTimeout = old })
	release := make(chan struct{})
	defer close(release)
	cfg := Config{Host: "127.0.0.1", Port: 443, UDID: "d", Token: "t"}
	client, _, cleanup := setupTCPWSEnforcePair(t, cfg, true, slowValidator(time.Hour, AuthVerdict{Code: -1}, release))
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := client.DialTCP(ctx, "example.com:80", nil)
	if !errors.Is(err, ErrMetadataTimeout) || !isProxyLayerError(err) {
		t.Fatalf("err = %v, want ErrMetadataTimeout as a ProxyLayerError", err)
	}
}
