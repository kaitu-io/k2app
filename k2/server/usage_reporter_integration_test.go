package server

import (
	"context"
	"testing"
)

// TestUsageReporterIntegration_RealHandlerGate is the end-to-end gate for
// Option D: it wires a REAL *ProxyHandler (not a fake controller) to the
// reporter and proves the whole in-process loop —
//
//	real byte counters  →  POST Center  →  real accept gate flips
//	new epoch from Center  →  real ResetTraffic() zeroes the real counters
//
// Bytes are driven through the handler's real pipe() path via drivePipe (the
// same technique TestProxyHandlerMetering uses) — no test-only setter is added
// to production code.
func TestUsageReporterIntegration_RealHandlerGate(t *testing.T) {
	stub, srv := newStubServer(t)

	// Real handler with real counters driven through the real pipe path.
	h := NewProxyHandler()
	const uplink = 1234   // bytes client -> target (rx)
	const downlink = 5678 // bytes target -> client (tx)
	drivePipe(t, h, uplink, downlink)

	wantTotal := int64(uplink + downlink)
	if rx, tx := h.TrafficBytes(); rx+tx != wantTotal {
		t.Fatalf("precondition: real handler total = %d, want %d", rx+tx, wantTotal)
	}
	if !h.Accepting() {
		t.Fatalf("precondition: fresh handler must default to Accepting()==true")
	}

	// *ProxyHandler satisfies trafficController as-is — pass the concrete handler.
	rep := newUsageReporter(h, srv.URL, "203.0.113.9", "topsecret")

	// --- Sub-case 1: Center says "stop" → the REAL accept gate flips off, and
	// the POST body carried the real cumulative_bytes total. ---
	stub.setResp(NodeUsageResponse{Verdict: "stop", EpochID: 0, NextReportInterval: 1})
	if _, err := rep.runOnce(context.Background()); err != nil {
		t.Fatalf("runOnce stop: %v", err)
	}
	if h.Accepting() {
		t.Fatalf("after Center verdict=stop, real handler Accepting() should be false")
	}
	req := stub.lastReq()
	if req == nil {
		t.Fatal("Center received no request")
	}
	// JSON numbers deserialize as float64.
	if cb, _ := req["cumulative_bytes"].(float64); int64(cb) != wantTotal {
		t.Fatalf("POST cumulative_bytes = %v, want %d (real counters)", req["cumulative_bytes"], wantTotal)
	}

	// --- Sub-case 2: Center returns a NEW epoch_id → the REAL ResetTraffic runs
	// and the real counters go to zero. ---
	stub.setResp(NodeUsageResponse{Verdict: "serve", EpochID: 42, NextReportInterval: 1})
	if _, err := rep.runOnce(context.Background()); err != nil {
		t.Fatalf("runOnce epoch-change: %v", err)
	}
	if rx, tx := h.TrafficBytes(); rx != 0 || tx != 0 {
		t.Fatalf("after epoch change, real handler counters = %d/%d, want 0/0 (real ResetTraffic ran)", rx, tx)
	}
	// And the gate recovered to serve.
	if !h.Accepting() {
		t.Fatalf("after Center verdict=serve, real handler Accepting() should be true")
	}
}
