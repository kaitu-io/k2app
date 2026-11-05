package core

import (
	"context"
	"log/slog"
	"net"
	"time"

	"github.com/kaitu-io/k2/wire"
)

// wireOutbound routes connections through a wire.Dialer (QUIC/TCP-WS transport).
// Encapsulates DIAG logging and WireErrorReporter integration that previously
// lived in ClientTunnel's handleTCPProxy/handleUDPProxy methods.
type wireOutbound struct {
	dialer       wire.Dialer
	wireReporter WireErrorReporter // nil = no reporting
	name         string
}

// NewWireOutbound creates an outbound backed by a wire.Dialer (e.g. TransportManager).
// name is a human-readable label for logs (e.g. "k2v5", "k2p-home").
// reporter receives wire transport errors for health monitoring (nil to disable).
func NewWireOutbound(name string, dialer wire.Dialer, reporter WireErrorReporter) Outbound {
	return &wireOutbound{dialer: dialer, wireReporter: reporter, name: name}
}

func (w *wireOutbound) DialTCP(ctx context.Context, dest string, earlyData []byte) (net.Conn, error) {
	start := time.Now()
	d := w.current()
	conn, err := d.DialTCP(ctx, dest, earlyData)
	dialLatency := time.Since(start)
	if err != nil {
		slog.Warn("DIAG: proxy-dial-fail", "dest", dest, "transport", "tcp", "outbound", w.name, "err", err)
		if w.wireReporter != nil && !isProxyLayerError(err) && w.stillCurrent(d) {
			w.wireReporter.ReportWireError(err)
		}
		return nil, err
	}
	if dialLatency > 3*time.Second {
		slog.Info("DIAG: proxy-dial-slow", "dest", dest, "transport", "tcp", "outbound", w.name, "latencyMs", dialLatency.Milliseconds())
	}
	if w.wireReporter != nil {
		w.wireReporter.ClearWireError()
	}
	return conn, nil
}

func (w *wireOutbound) DialUDP(ctx context.Context, dest string) (net.PacketConn, net.Addr, error) {
	start := time.Now()
	d := w.current()
	conn, err := d.DialUDP(ctx, dest)
	dialLatency := time.Since(start)
	if err != nil {
		slog.Warn("DIAG: proxy-dial-fail", "dest", dest, "transport", "udp", "outbound", w.name, "err", err)
		if w.wireReporter != nil && !isProxyLayerError(err) && w.stillCurrent(d) {
			w.wireReporter.ReportWireError(err)
		}
		return nil, nil, err
	}
	if dialLatency > 3*time.Second {
		slog.Info("DIAG: proxy-dial-slow", "dest", dest, "transport", "udp", "outbound", w.name, "latencyMs", dialLatency.Milliseconds())
	}
	if w.wireReporter != nil {
		w.wireReporter.ClearWireError()
	}
	return conn, nil, nil // nil destAddr = pre-connected
}

// current is the Dialer this dial goes to: for a wire.CurrentDialer (the
// engine's SwappableDialer) the currently-installed inner Dialer, captured
// once so the failure check below compares against exactly what was dialed.
func (w *wireOutbound) current() wire.Dialer {
	if cd, ok := w.dialer.(wire.CurrentDialer); ok {
		if d := cd.Current(); d != nil {
			return d
		}
	}
	return w.dialer
}

// stillCurrent reports whether d is still the installed Dialer. A dial that
// was in flight on a transport the engine has since replaced (Phase B swap
// to another server) fails against a dead, superseded node; its error says
// nothing about the session now running on the replacement and must not
// become lastError (PVE S1: connected on F1 with a stale 503 from the old
// node's in-flight dial).
func (w *wireOutbound) stillCurrent(d wire.Dialer) bool {
	cd, ok := w.dialer.(wire.CurrentDialer)
	if !ok {
		return true
	}
	return cd.Current() == d
}

func (w *wireOutbound) Name() string { return w.name }
func (w *wireOutbound) Close() error { return nil } // no-op: engine owns the wire.Dialer (tm)
