package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// usage_reporter.go runs the per-node traffic-cutoff loop entirely in-process
// (Plan 3-k2s Phase B, Option D). It is the only k2s component that both reads
// the node's honest byte counters AND holds K2_NODE_SECRET to talk to Center.
//
// Per cycle (runOnce):
//  1. total = rx+tx from the trafficController (in-process method call)
//  2. POST {center}/slave/usage  → Basic-auth report {epoch_id,cumulative_bytes,seq,ts}
//  3. on response: adopt epoch (reset counters on epoch change) + apply serve/stop verdict
//  4. on Center failure: keep last verdict, but locally enforce the last-seen hard ceiling
//
// Unlike the retired sidecar version, the reset and verdict are in-process
// method calls on the ProxyHandler — they cannot fail — so the "reset failed but
// epoch adopted" hazard does not exist here.
//
// Zero-change-for-shared-nodes: the caller only constructs/starts a reporter
// when UsageReportURL and NodeSecret are both set (the enable gate lives in the
// caller, not here).

const (
	// usageReportDefaultInterval is the fallback sleep when Center returns no
	// next_report_interval.
	usageReportDefaultInterval = 60 * time.Second
	// usageReportMaxBackoff caps the retry sleep after a Center failure.
	usageReportMaxBackoff = 30 * time.Second
	// usageReportHTTPTimeout bounds the Center POST (belt-and-suspenders with ctx).
	usageReportHTTPTimeout = 10 * time.Second
)

// NodeUsageRequest is the cumulative-traffic report the node POSTs to Center.
// Field tags MUST match Center's center.NodeUsageRequest exactly
// (api/slave_api_usage.go). A wrong tag silently breaks metering.
type NodeUsageRequest struct {
	EpochID         int64 `json:"epoch_id"`
	CumulativeBytes int64 `json:"cumulative_bytes"`
	Seq             int64 `json:"seq"`
	Ts              int64 `json:"ts"`
}

// NodeUsageResponse is Center's verdict + epoch identity.
// Field tags MUST match Center's center.NodeUsageResponse exactly.
type NodeUsageResponse struct {
	Verdict               string `json:"verdict"` // serve | stop
	EpochID               int64  `json:"epoch_id"`
	QuotaTotal            int64  `json:"quota_total"`
	QuotaUsed             int64  `json:"quota_used"`
	EpochHardCeilingBytes int64  `json:"epoch_hard_ceiling_bytes"`
	NextReportInterval    int64  `json:"next_report_interval"`
}

// centerEnvelope is Center's standard {code,message,data} wrapper.
type centerEnvelope struct {
	Code    int                `json:"code"`
	Message string             `json:"message"`
	Data    *NodeUsageResponse `json:"data"`
}

// trafficController is the slice of ProxyHandler the reporter drives. Depending
// on this interface (not *ProxyHandler) keeps the reporter on the right side of
// the Layer Boundary + Encapsulation constitutions: it only calls methods and
// never touches the handler's atomic fields. *ProxyHandler satisfies it as-is.
type trafficController interface {
	TrafficBytes() (rx, tx int64) // node-level cumulative
	ResetTraffic()                // zero counters (in-process, cannot fail)
	SetAccepting(bool)            // flip the accept gate
}

// usageReporter owns its loop state. A single goroutine (Run) touches epochID /
// seq / lastCeiling / lastVerdict, so there is no mutex (Concurrency
// constitution: single-goroutine state needs no lock).
type usageReporter struct {
	ctl        trafficController
	centerURL  string // Center base, e.g. https://k2.52j.me
	ipv4       string // node public IPv4 (Basic auth username)
	secret     string // K2_NODE_SECRET (Basic auth password) — NEVER log
	httpClient *http.Client

	// loop state (only the Run goroutine touches these):
	epochID     int64
	seq         int64
	lastCeiling int64
	lastVerdict string // "serve"/"stop"; starts "serve"
}

// newUsageReporter constructs a reporter. The caller is responsible for the
// enable gate (centerURL/secret non-empty) before constructing one.
func newUsageReporter(ctl trafficController, centerURL, ipv4, secret string) *usageReporter {
	return &usageReporter{
		ctl:         ctl,
		centerURL:   centerURL,
		ipv4:        ipv4,
		secret:      secret,
		httpClient:  &http.Client{Timeout: usageReportHTTPTimeout},
		lastVerdict: "serve",
	}
}

// Run drives the loop until ctx is cancelled. The first cycle fires immediately
// (zero initial delay) so a node re-deployed mid-epoch already over quota gets
// its first Center enforcement check at once, closing the startup blind spot.
// Subsequent cycles sleep for the interval runOnce returns (a short backoff
// after a failure).
func (r *usageReporter) Run(ctx context.Context) {
	slog.Info("DIAG: usage-reporter-start", "center", r.centerURL, "ipv4", r.ipv4)
	sleep := time.Duration(0) // fire the first cycle immediately
	for {
		select {
		case <-ctx.Done():
			slog.Info("DIAG: usage-reporter-stop")
			return
		case <-time.After(sleep):
		}
		// runOnce returns the next interval (normal interval on success, short
		// backoff on error) and already logs any failure internally.
		next, _ := r.runOnce(ctx)
		sleep = next
	}
}

// runOnce performs one cycle and returns the sleep duration the loop should wait
// before the next cycle. On Center failure it applies the offline hard-ceiling
// guard, returns a short backoff, and surfaces the (wrapped) error.
func (r *usageReporter) runOnce(ctx context.Context) (time.Duration, error) {
	rx, tx := r.ctl.TrafficBytes()
	total := rx + tx

	resp, err := r.report(ctx, total)
	if err != nil {
		// Offline fallback: keep last verdict but enforce the last-seen hard
		// ceiling locally so a disconnected node still cuts off at 100% of sold
		// quota. Epoch is NOT changed.
		if r.lastCeiling > 0 && total >= r.lastCeiling {
			slog.Warn("DIAG: usage-reporter-offline-cutoff", "total", total, "ceiling", r.lastCeiling)
			r.ctl.SetAccepting(false)
		}
		backoff := usageReportMaxBackoff
		if usageReportDefaultInterval < backoff {
			backoff = usageReportDefaultInterval
		}
		slog.Warn("DIAG: usage-reporter-cycle-fail", "epoch", r.epochID, "total", total, "retryIn", backoff, "err", err)
		return backoff, fmt.Errorf("usage report: %w", err)
	}

	// Epoch change → reset counters and adopt the new epoch. The reset is an
	// in-process store that cannot fail, so we adopt unconditionally.
	if resp.EpochID != r.epochID {
		slog.Info("DIAG: usage-reporter-epoch-change", "from", r.epochID, "to", resp.EpochID)
		r.ctl.ResetTraffic()
		r.epochID = resp.EpochID
	}

	if resp.EpochHardCeilingBytes > 0 {
		r.lastCeiling = resp.EpochHardCeilingBytes
	}

	accepting := resp.Verdict == "serve"
	r.ctl.SetAccepting(accepting)
	if resp.Verdict != r.lastVerdict {
		slog.Info("DIAG: usage-reporter-verdict-change",
			"from", r.lastVerdict, "to", resp.Verdict,
			"epoch", r.epochID, "total", total)
	}
	r.lastVerdict = resp.Verdict
	r.seq++

	sleep := time.Duration(resp.NextReportInterval) * time.Second
	if sleep <= 0 {
		sleep = usageReportDefaultInterval
	}
	return sleep, nil
}

// report POSTs the cumulative-usage report to Center with Basic auth
// (ipv4:secret) and unwraps the {code,message,data} envelope. The credential
// never leaves this function and is never logged.
func (r *usageReporter) report(ctx context.Context, total int64) (NodeUsageResponse, error) {
	var out NodeUsageResponse

	body, err := json.Marshal(NodeUsageRequest{
		EpochID:         r.epochID,
		CumulativeBytes: total,
		Seq:             r.seq,
		Ts:              time.Now().Unix(),
	})
	if err != nil {
		return out, fmt.Errorf("marshal report: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.centerURL+"/slave/usage", bytes.NewReader(body))
	if err != nil {
		return out, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Basic "+base64StdEncode([]byte(r.ipv4+":"+r.secret)))

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return out, fmt.Errorf("POST center: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return out, fmt.Errorf("center HTTP %d", resp.StatusCode)
	}

	var env centerEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return out, fmt.Errorf("decode center envelope: %w", err)
	}
	if env.Code != 0 || env.Data == nil {
		return out, fmt.Errorf("center usage failed: code=%d message=%s", env.Code, env.Message)
	}
	return *env.Data, nil
}

// base64StdEncode wraps the stdlib encoder so callers (and the auth header) have
// a single, audited path for credential encoding.
func base64StdEncode(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}
