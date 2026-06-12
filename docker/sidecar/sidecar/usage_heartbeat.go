package sidecar

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

// usage_heartbeat.go bridges the loopback k2s usage API to Center so the
// per-node traffic cutoff actually fires (Plan 3-k2s Phase B). It is the only
// component that both reads k2s's honest byte counters AND holds K2_NODE_SECRET
// to talk to Center.
//
// Per cycle (runOnce):
//  1. GET {UsageAPIURL}/usage      → k2s's cumulative bytes (total = rx+tx)
//  2. POST {Center}/slave/usage    → Basic-auth report {epoch_id, cumulative_bytes, seq, ts}
//  3. on response: adopt epoch (reset k2s counters on epoch change) + apply serve/stop verdict
//  4. on Center failure: keep last verdict, but locally enforce the last-seen hard ceiling
//
// Zero-change-for-shared-nodes: the loop only runs when UsageAPIURL != "".
// Shared-pool nodes never set K2_USAGE_API_URL → Run() is a no-op.

const (
	// usageHeartbeatDefaultInterval is the fallback sleep when Center returns
	// no next_report_interval.
	usageHeartbeatDefaultInterval = 60 * time.Second
	// usageHeartbeatMaxBackoff caps the retry sleep after a Center failure.
	usageHeartbeatMaxBackoff = 30 * time.Second
	// usageHTTPTimeout bounds the loopback GET/POST calls to k2s.
	usageHTTPTimeout = 5 * time.Second
)

// NodeUsageRequest is the cumulative-traffic report the node POSTs to Center.
// Field tags MUST match Center's center.NodeUsageRequest exactly.
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

// usageResponse is the k2s GET /usage shape.
type usageResponse struct {
	RX    int64 `json:"rx"`
	TX    int64 `json:"tx"`
	Total int64 `json:"total"`
	Ts    int64 `json:"ts"`
}

// verdictRequest is the k2s POST /verdict body.
type verdictRequest struct {
	Accepting bool `json:"accepting"`
}

// UsageHeartbeat polls k2s usage, reports to Center, and applies the verdict.
type UsageHeartbeat struct {
	node       *Node
	usageURL   string // k2s loopback usage API base (e.g. http://127.0.0.1:9099); empty = disabled
	httpClient *http.Client

	// local epoch/seq state (single-goroutine; runOnce is never called concurrently)
	epochID int64
	seq     int64

	// last-seen values carried across cycles for offline fallback
	lastCeiling int64  // epoch_hard_ceiling_bytes from the most recent Center reply
	lastVerdict string // "serve"/"stop" from the most recent Center reply
}

// NewUsageHeartbeat constructs a heartbeat client. usageURL == "" disables it.
func NewUsageHeartbeat(node *Node, usageURL string) *UsageHeartbeat {
	return &UsageHeartbeat{
		node:        node,
		usageURL:    usageURL,
		httpClient:  &http.Client{Timeout: usageHTTPTimeout},
		lastVerdict: "serve",
	}
}

// Enabled reports whether the heartbeat is configured to run.
func (h *UsageHeartbeat) Enabled() bool { return h.usageURL != "" }

// Run drives the heartbeat loop until ctx is cancelled. It is a no-op (returns
// immediately) when disabled, so shared-pool nodes pay nothing.
func (h *UsageHeartbeat) Run(ctx context.Context) {
	if !h.Enabled() {
		slog.Info("Usage heartbeat disabled (K2_USAGE_API_URL not set)", "component", "usage")
		return
	}

	slog.Info("Starting usage heartbeat loop", "component", "usage", "usageURL", h.usageURL)
	sleep := usageHeartbeatDefaultInterval
	for {
		select {
		case <-ctx.Done():
			slog.Info("Usage heartbeat stopped", "component", "usage")
			return
		case <-time.After(sleep):
		}

		next, err := h.runOnce(ctx)
		if err != nil {
			// Center unreachable (or k2s unreachable): back off and retry sooner.
			sleep = next
			if sleep <= 0 || sleep > usageHeartbeatMaxBackoff {
				sleep = usageHeartbeatMaxBackoff
			}
			slog.Warn("Usage heartbeat cycle failed", "component", "usage", "retryIn", sleep, "err", err)
			continue
		}
		sleep = next
	}
}

// runOnce performs a single heartbeat cycle and returns the sleep duration the
// loop should wait before the next cycle. On Center failure it returns the
// error (after applying the offline hard-ceiling guard) so the loop can back
// off; the returned duration is the backoff hint.
func (h *UsageHeartbeat) runOnce(ctx context.Context) (time.Duration, error) {
	// 1. Read k2s cumulative bytes.
	usage, err := h.getUsage(ctx)
	if err != nil {
		return usageHeartbeatMaxBackoff, fmt.Errorf("read k2s usage: %w", err)
	}

	// 2. Report to Center.
	h.seq++
	report := NodeUsageRequest{
		EpochID:         h.epochID,
		CumulativeBytes: usage.Total,
		Seq:             h.seq,
		Ts:              time.Now().Unix(),
	}
	resp, err := h.reportToCenter(report)
	if err != nil {
		// 3a. Offline fallback: keep last verdict but enforce the last-seen hard
		// ceiling locally so a disconnected node still cuts off at 100% of sold
		// quota. Epoch is NOT changed.
		if h.lastCeiling > 0 && usage.Total >= h.lastCeiling {
			slog.Warn("Usage heartbeat offline — enforcing hard ceiling",
				"component", "usage", "total", usage.Total, "ceiling", h.lastCeiling)
			if verr := h.pushVerdict(ctx, false); verr != nil {
				slog.Warn("Failed to push offline cutoff verdict", "component", "usage", "err", verr)
			}
		}
		return min(usageHeartbeatMaxBackoff, h.intervalOr(0)), fmt.Errorf("report to Center: %w", err)
	}

	// 3b. Epoch change → reset k2s counters and adopt the new epoch.
	if resp.EpochID != h.epochID {
		slog.Info("Usage epoch changed — resetting k2s counters",
			"component", "usage", "from", h.epochID, "to", resp.EpochID)
		if rerr := h.resetK2s(ctx); rerr != nil {
			slog.Warn("Failed to reset k2s counters", "component", "usage", "err", rerr)
		}
		h.epochID = resp.EpochID
	}

	// Remember last-seen values for the offline guard.
	if resp.EpochHardCeilingBytes > 0 {
		h.lastCeiling = resp.EpochHardCeilingBytes
	}
	h.lastVerdict = resp.Verdict

	// 4. Apply verdict.
	accepting := resp.Verdict != "stop"
	if err := h.pushVerdict(ctx, accepting); err != nil {
		slog.Warn("Failed to push verdict to k2s", "component", "usage", "accepting", accepting, "err", err)
	}

	slog.Info("Usage heartbeat cycle ok",
		"component", "usage",
		"epoch", h.epochID,
		"total", usage.Total,
		"verdict", resp.Verdict,
		"quotaUsed", resp.QuotaUsed,
		"quotaTotal", resp.QuotaTotal)

	return h.intervalOr(resp.NextReportInterval), nil
}

// intervalOr converts Center's next_report_interval (seconds) to a Duration,
// falling back to the default when zero/missing.
func (h *UsageHeartbeat) intervalOr(secs int64) time.Duration {
	if secs <= 0 {
		return usageHeartbeatDefaultInterval
	}
	return time.Duration(secs) * time.Second
}

// getUsage performs GET {usageURL}/usage against the loopback k2s API.
func (h *UsageHeartbeat) getUsage(ctx context.Context) (usageResponse, error) {
	var out usageResponse
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.usageURL+"/usage", nil)
	if err != nil {
		return out, err
	}
	resp, err := h.httpClient.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return out, fmt.Errorf("k2s usage HTTP %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, fmt.Errorf("decode usage: %w", err)
	}
	return out, nil
}

// resetK2s performs POST {usageURL}/reset to zero k2s counters.
func (h *UsageHeartbeat) resetK2s(ctx context.Context) error {
	return h.postK2s(ctx, "/reset", nil)
}

// pushVerdict performs POST {usageURL}/verdict {"accepting":<bool>}.
func (h *UsageHeartbeat) pushVerdict(ctx context.Context, accepting bool) error {
	return h.postK2s(ctx, "/verdict", verdictRequest{Accepting: accepting})
}

// postK2s POSTs an optional JSON body to the loopback k2s API.
func (h *UsageHeartbeat) postK2s(ctx context.Context, path string, body any) error {
	var rdr *bytes.Buffer
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewBuffer(b)
	} else {
		rdr = bytes.NewBuffer(nil)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.usageURL+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("k2s %s HTTP %d", path, resp.StatusCode)
	}
	return nil
}

// reportToCenter POSTs the usage report to Center with Basic auth (ipv4:secret)
// and parses the verdict response. Mirrors node.requestWithAuth's auth scheme
// without reusing it directly so we keep a context-aware client and avoid the
// shared 10s timeout; the credential never leaves this function.
func (h *UsageHeartbeat) reportToCenter(report NodeUsageRequest) (NodeUsageResponse, error) {
	var out NodeUsageResponse

	body, err := json.Marshal(report)
	if err != nil {
		return out, err
	}
	req, err := http.NewRequest(http.MethodPost, h.node.CenterURL+"/slave/usage", bytes.NewBuffer(body))
	if err != nil {
		return out, err
	}
	req.Header.Set("Content-Type", "application/json")
	auth := base64.StdEncoding.EncodeToString([]byte(h.node.IPv4 + ":" + h.node.Secret))
	req.Header.Set("Authorization", "Basic "+auth)

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return out, fmt.Errorf("center usage HTTP %d", resp.StatusCode)
	}

	var wrapped CenterResponse[NodeUsageResponse]
	if err := json.NewDecoder(resp.Body).Decode(&wrapped); err != nil {
		return out, fmt.Errorf("decode center response: %w", err)
	}
	if wrapped.Code != 0 || wrapped.Data == nil {
		return out, fmt.Errorf("center usage failed: code=%d, message=%s", wrapped.Code, wrapped.Message)
	}
	return *wrapped.Data, nil
}
