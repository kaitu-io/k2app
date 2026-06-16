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

// usage_reporter.go drives the per-node traffic-cutoff loop from the SIDECAR
// (Part 2). It is the single component that reads the node's honest HOST-NIC
// counters AND holds the node secret to talk to Center. Ported from the retired
// k2 server/usage_reporter.go.
//
// Per cycle (runOnce):
//  1. total = host-NIC cumulative bytes since last epoch (nicMeter)
//  2. POST {center}/slave/usage → Basic-auth report {epoch_id,cumulative_bytes,seq,ts}
//  3. on epoch change: rebaseline the meter and adopt the new epoch
//
// There is NO SetAccepting: the sidecar cannot flip k2s's accept gate. Online
// enforcement is Center-side (slave_api_device_auth.go rejects new connections
// at 95%; isTunnelOverQuota hides the node from /api/tunnels + /api/subs) — both
// read CloudInstance.TrafficUsedBytes, which THIS reporter now feeds accurately.
//
// The enable gate lives in the caller (main.go): only constructed for private
// nodes (PrivateClaim != ""). Shared-pool nodes never report → byte-identical.

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
	Seq             int64 `json:"seq"`
	Ts              int64 `json:"ts"`
}

// NodeUsageResponse — JSON tags MUST match center.NodeUsageResponse exactly.
type NodeUsageResponse struct {
	Verdict               string `json:"verdict"`
	EpochID               int64  `json:"epoch_id"`
	QuotaTotal            int64  `json:"quota_total"`
	QuotaUsed             int64  `json:"quota_used"`
	EpochHardCeilingBytes int64  `json:"epoch_hard_ceiling_bytes"`
	NextReportInterval    int64  `json:"next_report_interval"`
}

// usageEnvelope is Center's standard {code,message,data} wrapper.
type usageEnvelope struct {
	Code    int                `json:"code"`
	Message string             `json:"message"`
	Data    *NodeUsageResponse `json:"data"`
}

// nicMeter is the host-NIC accounting the reporter drives. CumulativeBytes
// returns bytes since the last epoch; Rebaseline restarts that count from 0.
type nicMeter interface {
	CumulativeBytes() (int64, error)
	Rebaseline()
}

// quotaSink receives each cycle's Center quota, for the node-side cutoff enforcer.
type quotaSink interface {
	SetQuota(epochID, quotaTotal, quotaUsed int64)
}

// usageReporter owns its loop state. A single goroutine (Run) touches epochID /
// seq, so there is no mutex.
type usageReporter struct {
	meter      nicMeter
	centerURL  string // Center base, e.g. https://k2.52j.me
	ipv4       string // node public IPv4 (Basic-auth username)
	secret     string // node secret (Basic-auth password) — NEVER log
	httpClient *http.Client
	sink       quotaSink

	epochID int64
	seq     int64
}

// NewUsageReporter constructs a reporter. The caller owns the enable gate
// (private node only) before constructing one.
func NewUsageReporter(meter nicMeter, centerURL, ipv4, secret string) *usageReporter {
	return &usageReporter{
		meter:      meter,
		centerURL:  centerURL,
		ipv4:       ipv4,
		secret:     secret,
		httpClient: &http.Client{Timeout: usageReportHTTPTimeout},
	}
}

// SetSink attaches a quota consumer (the cutoff enforcer). Optional: with no sink
// the reporter only does accounting.
func (r *usageReporter) SetSink(s quotaSink) { r.sink = s }

// Run drives the loop until ctx is cancelled. The first cycle fires immediately
// so a node re-deployed mid-epoch already over quota reports at once.
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
// failure it returns a short backoff (and logs); it never panics or exits.
func (r *usageReporter) runOnce(ctx context.Context) time.Duration {
	total, err := r.meter.CumulativeBytes()
	if err != nil {
		slog.Warn("DIAG: usage-reporter-meter-fail", "component", "usage", "err", err)
		return usageReportMaxBackoff
	}

	resp, err := r.report(ctx, total)
	if err != nil {
		slog.Warn("DIAG: usage-reporter-cycle-fail", "component", "usage", "epoch", r.epochID, "total", total, "err", err)
		return usageReportMaxBackoff
	}

	if resp.EpochID != r.epochID {
		slog.Info("DIAG: usage-reporter-epoch-change", "component", "usage", "from", r.epochID, "to", resp.EpochID)
		r.meter.Rebaseline()
		r.epochID = resp.EpochID
	}

	if r.sink != nil {
		r.sink.SetQuota(r.epochID, resp.QuotaTotal, resp.QuotaUsed)
	}

	slog.Info("DIAG: usage-reporter-cycle-ok", "component", "usage",
		"epoch", r.epochID, "cumulative", total, "verdict", resp.Verdict,
		"quotaUsed", resp.QuotaUsed, "quotaTotal", resp.QuotaTotal)
	r.seq++

	sleep := time.Duration(resp.NextReportInterval) * time.Second
	if sleep <= 0 {
		sleep = usageReportDefaultInterval
	}
	return sleep
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
