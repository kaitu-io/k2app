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
	conn, err := w.dialer.DialTCP(ctx, dest, earlyData)
	dialLatency := time.Since(start)
	if err != nil {
		slog.Warn("DIAG: proxy-dial-fail", "dest", dest, "transport", "tcp", "outbound", w.name, "err", err)
		if w.wireReporter != nil && !isProxyLayerError(err) {
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
	conn, err := w.dialer.DialUDP(ctx, dest)
	dialLatency := time.Since(start)
	if err != nil {
		slog.Warn("DIAG: proxy-dial-fail", "dest", dest, "transport", "udp", "outbound", w.name, "err", err)
		if w.wireReporter != nil && !isProxyLayerError(err) {
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

func (w *wireOutbound) Name() string { return w.name }
func (w *wireOutbound) Close() error { return nil } // no-op: engine owns the wire.Dialer (tm)
