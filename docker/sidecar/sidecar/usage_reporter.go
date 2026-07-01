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

// usage_reporter.go reports the node's traffic to Center as a pure record. The
// node is the single metering AND cutoff authority: the shared TrafficMonitor
// self-meters the host NIC and owns the monthly billing cycle, the enforcer
// reads the same monitor and pauses data-plane containers locally, and this
// reporter merely POSTs the monitor's stats (used + limit) to Center for
// visibility. Center returns only an ack carrying the next report interval; it
// never sends a quota verdict back.
//
// Per cycle (runOnce):
//  1. stats = TrafficMonitor.GetTrafficStats() (host-NIC used + limit + cycle end)
//  2. POST {center}/slave/usage → Basic-auth record {epoch_id,cumulative_bytes,
//     quota_total_bytes,seq,ts}
//  3. sleep for the server-supplied NextReportInterval (Center owns the cadence)
//
// The reporter holds the node secret to Basic-auth with Center; the credential
// is never logged.

const (
	usageReportDefaultInterval = 60 * time.Second
	usageReportMaxBackoff      = 30 * time.Second
	usageReportHTTPTimeout     = 10 * time.Second
)

// NodeUsageRequest — JSON tags MUST match center.NodeUsageRequest exactly
// (api/slave_api_usage.go). A wrong tag silently breaks metering.
type NodeUsageRequest struct {
	EpochID         int64 `json:"epoch_id"`
	CumulativeBytes int64 `json:"cumulative_bytes"`
	QuotaTotalBytes int64 `json:"quota_total_bytes"`
	Seq             int64 `json:"seq"`
	Ts              int64 `json:"ts"`
}

// NodeUsageResponse — JSON tags MUST match center.NodeUsageResponse exactly.
// Center is a pure recorder now: it returns only the next report interval (no
// quota verdict — the node is the cutoff authority).
type NodeUsageResponse struct {
	NextReportInterval int64 `json:"next_report_interval"`
}

// usageEnvelope is Center's standard {code,message,data} wrapper.
type usageEnvelope struct {
	Code    int                `json:"code"`
	Message string             `json:"message"`
	Data    *NodeUsageResponse `json:"data"`
}

// usageReporter owns its loop state. A single goroutine (Run) touches seq, so
// there is no mutex.
type usageReporter struct {
	src        statsSource
	centerURL  string // Center base, e.g. https://k2.52j.me
	ipv4       string // node public IPv4 (Basic-auth username)
	secret     string // node secret (Basic-auth password) — NEVER log
	httpClient *http.Client

	seq int64
}

// NewUsageReporter constructs a reporter reading the shared TrafficMonitor.
func NewUsageReporter(src statsSource, centerURL, ipv4, secret string) *usageReporter {
	return &usageReporter{
		src:        src,
		centerURL:  centerURL,
		ipv4:       ipv4,
		secret:     secret,
		httpClient: &http.Client{Timeout: usageReportHTTPTimeout},
	}
}

// Run drives the loop until ctx is cancelled. The first cycle fires immediately
// so a node re-deployed mid-cycle reports its current usage at once.
func (r *usageReporter) Run(ctx context.Context) {
	slog.Info("DIAG: usage-reporter-start", "component", "usage", "center", r.centerURL, "ipv4", r.ipv4)
	sleep := time.Duration(0)
	for {
		select {
		case <-ctx.Done():
			slog.Info("DIAG: usage-reporter-stop", "component", "usage")
			return
		case <-time.After(sleep):
		}
		sleep = r.runOnce(ctx)
	}
}

// runOnce performs one cycle and returns the sleep before the next. On any
// failure it returns a backoff (and logs); it never panics or exits. On a meter
// read error it does NOT POST (fail-closed: never report garbage to Center).
func (r *usageReporter) runOnce(ctx context.Context) time.Duration {
	stats, err := r.src.GetTrafficStats()
	if err != nil {
		slog.Warn("DIAG: usage-reporter-meter-fail", "component", "usage", "err", err)
		return usageReportMaxBackoff // fail-closed: do NOT POST garbage
	}

	resp, err := r.report(ctx, stats)
	if err != nil {
		slog.Warn("DIAG: usage-reporter-cycle-fail", "component", "usage",
			"used", stats.UsedTrafficBytes, "limit", stats.MonthlyTrafficLimitBytes, "err", err)
		return usageReportMaxBackoff
	}

	slog.Info("DIAG: usage-reporter-cycle-ok", "component", "usage",
		"epoch", stats.BillingCycleEndAt, "cumulative", stats.UsedTrafficBytes,
		"quotaTotal", stats.MonthlyTrafficLimitBytes)
	r.seq++

	sleep := time.Duration(resp.NextReportInterval) * time.Second
	if sleep < 10*time.Second {
		sleep = usageReportDefaultInterval // floor; Center owns cadence
	}
	return sleep
}

// report POSTs the traffic record to Center with Basic auth (ipv4:secret) and
// unwraps the {code,message,data} envelope. The credential never leaves this
// function and is never logged.
func (r *usageReporter) report(ctx context.Context, stats TrafficStats) (NodeUsageResponse, error) {
	var out NodeUsageResponse

	body, err := json.Marshal(NodeUsageRequest{
		EpochID:         stats.BillingCycleEndAt,
		CumulativeBytes: stats.UsedTrafficBytes,
		QuotaTotalBytes: stats.MonthlyTrafficLimitBytes,
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
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(r.ipv4+":"+r.secret)))

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return out, fmt.Errorf("POST center: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return out, fmt.Errorf("center HTTP %d", resp.StatusCode)
	}

	var env usageEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return out, fmt.Errorf("decode center envelope: %w", err)
	}
	if env.Code != 0 || env.Data == nil {
		return out, fmt.Errorf("center usage failed: code=%d message=%s", env.Code, env.Message)
	}
	return *env.Data, nil
}
