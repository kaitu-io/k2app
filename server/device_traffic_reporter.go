package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"time"
)

// device_traffic_reporter.go ships per-device byte DELTAS to Center for
// user-level accounting (spec 2026-07-22). Fully decoupled from the
// node-level usage/cutoff loop (usage_reporter.go): its own endpoint, its own
// cadence, no verdict handling. Loss tolerance: a crash drops at most one
// un-reported cycle; duplication is prevented by the immutable-batch
// (boot_id, batch_seq) idempotency contract with Center.

const (
	deviceReportInterval    = 60 * time.Second
	deviceReportHTTPTimeout = 10 * time.Second
	deviceReportMaxBackoff  = 30 * time.Second
)

// DeviceTrafficRequest / DeviceTrafficItem — JSON tags MUST match Center's
// center.DeviceTrafficRequest exactly (api/slave_api_device_traffic.go).
type DeviceTrafficRequest struct {
	BootID   string              `json:"boot_id"`
	BatchSeq int64               `json:"batch_seq"`
	Ts       int64               `json:"ts"`
	Devices  []DeviceTrafficItem `json:"devices"`
}

type DeviceTrafficItem struct {
	UDID string `json:"udid"`
	Rx   int64  `json:"rx"`
	Tx   int64  `json:"tx"`
}

type deviceTrafficSource interface {
	TakeDeviceTraffic() map[string]DeviceDelta
}

// deviceBatch is IMMUTABLE once created: retries resend it verbatim. Merging
// new deltas into an unacked batch would double-count when the previous send
// actually landed but the ack was lost.
type deviceBatch struct {
	bootID string
	seq    int64
	items  []DeviceTrafficItem
}

type deviceTrafficReporter struct {
	src        deviceTrafficSource
	centerURL  string
	ipv4       string
	secret     string // never log
	httpClient *http.Client

	// single-goroutine state (Run loop only):
	bootID  string
	seq     int64
	pending *deviceBatch
}

func newDeviceTrafficReporter(src deviceTrafficSource, centerURL, ipv4, secret string) *deviceTrafficReporter {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return &deviceTrafficReporter{
		src:        src,
		centerURL:  centerURL,
		ipv4:       ipv4,
		secret:     secret,
		httpClient: &http.Client{Timeout: deviceReportHTTPTimeout},
		bootID:     hex.EncodeToString(b),
	}
}

// Run drives the loop until ctx is cancelled.
func (r *deviceTrafficReporter) Run(ctx context.Context) {
	slog.Info("DIAG: device-traffic-reporter-start", "center", r.centerURL, "ipv4", r.ipv4, "boot", r.bootID)
	sleep := deviceReportInterval
	for {
		select {
		case <-ctx.Done():
			slog.Info("DIAG: device-traffic-reporter-stop")
			return
		case <-time.After(sleep):
		}
		if err := r.runOnce(ctx); err != nil {
			slog.Warn("DIAG: device-traffic-cycle-fail", "seq", r.seq, "err", err)
			sleep = deviceReportMaxBackoff
		} else {
			sleep = deviceReportInterval
		}
	}
}

// runOnce sends the pending batch (retry) or drains a fresh one. Exported for
// tests via same-package access.
func (r *deviceTrafficReporter) runOnce(ctx context.Context) error {
	if r.pending == nil {
		snap := r.src.TakeDeviceTraffic()
		if len(snap) == 0 {
			return nil
		}
		items := make([]DeviceTrafficItem, 0, len(snap))
		for udid, d := range snap {
			items = append(items, DeviceTrafficItem{UDID: udid, Rx: d.Rx, Tx: d.Tx})
		}
		sort.Slice(items, func(i, j int) bool { return items[i].UDID < items[j].UDID })
		r.seq++
		r.pending = &deviceBatch{bootID: r.bootID, seq: r.seq, items: items}
	}
	if err := r.post(ctx, r.pending); err != nil {
		return err
	}
	r.pending = nil
	return nil
}

func (r *deviceTrafficReporter) post(ctx context.Context, b *deviceBatch) error {
	body, err := json.Marshal(DeviceTrafficRequest{
		BootID:   b.bootID,
		BatchSeq: b.seq,
		Ts:       time.Now().Unix(),
		Devices:  b.items,
	})
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.centerURL+"/slave/device-traffic", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Basic "+base64StdEncode([]byte(r.ipv4+":"+r.secret)))

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("POST center: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("center HTTP %d", resp.StatusCode)
	}
	var env struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return fmt.Errorf("decode envelope: %w", err)
	}
	if env.Code != 0 {
		return fmt.Errorf("center device-traffic failed: code=%d message=%s", env.Code, env.Message)
	}
	return nil
}
