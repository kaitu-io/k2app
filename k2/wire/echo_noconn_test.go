package wire

import (
	"context"
	"errors"
	"net"
	"strconv"
	"testing"
	"time"
)

// A probe that never reached a live connection says nothing about whether the
// server supports echo. IsEchoUnsupported is fail-open (every non-timeout
// error reads as "pre-echo server"), so before ErrEchoNoConnection these
// errors were reported as "unsupported": the engine's echoStep grew trust and
// zeroed the strike counter, and `DIAG: echo-probe-unsupported` fired against
// servers that do implement echo. Field shape (Android 0.4.13 vs k2s 0.4.14):
// the connectivity change Android raises right after the VPN comes up resets
// the transport, the 1s probe then finds no cached connection, and the log
// shows echo-probe-unsupported on nearly every connect.

// TestQUICRunEchoProbe_AfterResetIsNotUnsupported: Reset drops the cached
// connection (the next dial rebuilds it lazily), so a probe in between has
// nothing to probe.
func TestQUICRunEchoProbe_AfterResetIsNotUnsupported(t *testing.T) {
	client, _, cleanup := setupQUICPair(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := client.connect(ctx); err != nil {
		t.Fatal(err)
	}
	// Positive control: a real server answers echo over QUIC.
	if err := client.RunEchoProbe(ctx); err != nil {
		t.Fatalf("echo against a live server failed: %v", err)
	}

	client.Reset()

	err := client.RunEchoProbe(ctx)
	if err == nil {
		t.Fatal("expected an error with no cached connection")
	}
	if IsEchoUnsupported(err) {
		t.Fatalf("missing connection classified as 'server has no echo': %v", err)
	}
	if !IsEchoNotProbed(err) {
		t.Fatalf("missing connection should be IsEchoNotProbed, got %v", err)
	}
}

// TestQUICRunEchoProbe_LocallyClosedConnIsNotUnsupported: the cached
// connection is already closed (a Reset racing the probe, which is the
// "Application error 0x0 (local): connection reset" shape in field logs).
func TestQUICRunEchoProbe_LocallyClosedConnIsNotUnsupported(t *testing.T) {
	client, _, cleanup := setupQUICPair(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := client.connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Close the connection underneath the client without clearing c.conn.
	conn.CloseWithError(0, "connection reset")
	<-conn.Context().Done()

	err = client.RunEchoProbe(ctx)
	if err == nil {
		t.Fatal("expected an error on a closed connection")
	}
	if IsEchoUnsupported(err) {
		t.Fatalf("closed connection classified as 'server has no echo': %v", err)
	}
	if !IsEchoNotProbed(err) {
		t.Fatalf("closed connection should be IsEchoNotProbed, got %v", err)
	}
}

// TestTCPWSRunEchoProbe_ConnectRefusedIsNotUnsupported: a tcpws primary whose
// connect fails with a non-timeout error never opened an echo stream.
func TestTCPWSRunEchoProbe_ConnectRefusedIsNotUnsupported(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close() // nothing listens here any more → connection refused

	client := NewTCPWSClient(Config{Host: "127.0.0.1", Port: port, Insecure: true}, nil)
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err = client.RunEchoProbe(ctx)
	if err == nil {
		t.Fatal("expected connect to 127.0.0.1:" + strconv.Itoa(port) + " to fail")
	}
	if IsEchoUnsupported(err) {
		t.Fatalf("failed connect classified as 'server has no echo': %v", err)
	}
	if !IsEchoNotProbed(err) {
		t.Fatalf("failed connect should be IsEchoNotProbed, got %v", err)
	}
}

// TestIsEchoNotProbed_TimeoutsStayFailures: relabelling must never swallow a
// timeout — a dead data path has to keep striking (ticket #3463).
func TestIsEchoNotProbed_TimeoutsStayFailures(t *testing.T) {
	for _, err := range []error{context.DeadlineExceeded, &net.OpError{Op: "dial", Err: timeoutErr{}}} {
		if got := echoNotProbed(err); IsEchoNotProbed(got) || IsEchoUnsupported(got) {
			t.Fatalf("timeout %v relabelled: notProbed=%v unsupported=%v", err, IsEchoNotProbed(got), IsEchoUnsupported(got))
		}
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

// TestRecordEchoResult_NotProbedRecordsNothing: a probe with no live
// connection must not count as an availability failure — it is not evidence
// about the channel. (IsEchoUnsupported now returns false for it, so without
// the explicit check it would have become a failed sample.)
func TestRecordEchoResult_NotProbedRecordsNothing(t *testing.T) {
	tm := NewTransportManager()
	now := time.Unix(1000, 0)
	before := tm.ScoreQUIC(now)
	tm.RecordEchoResult("quic", 0, echoNotProbed(errors.New("closed")), now)
	if got := tm.ScoreQUIC(now); got != before {
		t.Fatalf("not-probed echo moved scoreQUIC %v → %v", before, got)
	}
}
