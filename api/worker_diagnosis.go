package center

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	hibikenAsynq "github.com/hibiken/asynq"
	"github.com/wordgate/qtoolkit/asynq"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm/clause"
)

// Task type constants for route diagnosis
const (
	TaskTypeDiagnosisOutbound = "diagnosis:outbound" // Outbound diagnosis (client → slave via Alibaba probes)
	TaskTypeDiagnosisAll      = "diagnosis:all"      // Run diagnosis for all nodes
)

// DiagnosisOutboundPayload payload for outbound diagnosis task
type DiagnosisOutboundPayload struct {
	IP string `json:"ip"` // IPv4 address to diagnose
}

// DiagnosisAllPayload payload for batch diagnosis task
type DiagnosisAllPayload struct {
	// Empty - diagnoses all active nodes
}

// RegisterDiagnosisWorker registers diagnosis task handlers and cron jobs
func RegisterDiagnosisWorker() {
	cfg := configDiagnosis()
	if !cfg.Enabled {
		log.Infof(context.Background(), "[DIAGNOSIS] Route diagnosis is disabled")
		return
	}

	// Register task handlers
	asynq.Handle(TaskTypeDiagnosisOutbound, handleDiagnosisOutbound)
	asynq.Handle(TaskTypeDiagnosisAll, handleDiagnosisAll)

	// Register cron job for scheduled diagnosis (default: weekly)
	// Unique(25*time.Hour) prevents duplicate cron runs within the same day
	asynq.Cron(cfg.Cron, TaskTypeDiagnosisAll, nil, hibikenAsynq.Unique(25*time.Hour))

	log.Infof(context.Background(), "[DIAGNOSIS] Route diagnosis worker registered (cron: %s)", cfg.Cron)
}

// handleDiagnosisOutbound handles outbound diagnosis for a single IP
func handleDiagnosisOutbound(ctx context.Context, payload []byte) error {
	var p DiagnosisOutboundPayload
	if err := asynq.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("unmarshal payload failed: %w", err)
	}

	taskID, _ := hibikenAsynq.GetTaskID(ctx)
	log.Infof(ctx, "[DIAGNOSIS] Starting outbound diagnosis: taskId=%s, ip=%s", taskID, p.IP)

	// Get Aliyun CMS credentials for site monitoring
	aliyunCfg := configAliyunCMS()
	if aliyunCfg.AccessKeyID == "" || aliyunCfg.AccessKeySecret == "" {
		log.Errorf(ctx, "[DIAGNOSIS] Aliyun credentials not configured")
		return fmt.Errorf("aliyun credentials not configured")
	}

	// Run diagnosis
	results, err := RunNodeDiagnosis(ctx, p.IP, aliyunCfg.AccessKeyID, aliyunCfg.AccessKeySecret)
	if err != nil {
		log.Errorf(ctx, "[DIAGNOSIS] Outbound diagnosis failed: ip=%s, error=%v", p.IP, err)
		return fmt.Errorf("outbound diagnosis failed: %w", err)
	}

	// Aggregate results into route matrix
	routeMap := AggregateRouteInfo(results)
	routeMapJSON, _ := json.Marshal(routeMap)

	// Upsert diagnosis result
	info := IPRouteInfo{
		IP:           p.IP,
		Direction:    DiagnosisDirectionOutbound,
		RouteMatrix:  string(routeMapJSON),
		ProbeCount:   len(DiagnosisProbes),
		SuccessCount: len(results),
		DiagnosedAt:  time.Now(),
	}

	err = db.Get().Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "ip"}, {Name: "direction"}},
		DoUpdates: clause.AssignmentColumns([]string{"route_matrix", "probe_count", "success_count", "diagnosed_at", "updated_at"}),
	}).Create(&info).Error

	if err != nil {
		log.Errorf(ctx, "[DIAGNOSIS] Failed to save diagnosis result: ip=%s, error=%v", p.IP, err)
		return fmt.Errorf("failed to save diagnosis result: %w", err)
	}

	log.Infof(ctx, "[DIAGNOSIS] Outbound diagnosis completed: ip=%s, probes=%d, success=%d",
		p.IP, len(DiagnosisProbes), len(results))

	return nil
}

// handleDiagnosisAll handles batch diagnosis for all active nodes
func handleDiagnosisAll(ctx context.Context, _ []byte) error {
	taskID, _ := hibikenAsynq.GetTaskID(ctx)
	log.Infof(ctx, "[DIAGNOSIS] Starting batch diagnosis for all nodes: taskId=%s", taskID)

	// Get all nodes (all nodes in DB are active by design)
	var nodes []SlaveNode
	err := db.Get().Find(&nodes).Error
	if err != nil {
		log.Errorf(ctx, "[DIAGNOSIS] Failed to get nodes: %v", err)
		return fmt.Errorf("failed to get nodes: %w", err)
	}

	if len(nodes) == 0 {
		log.Infof(ctx, "[DIAGNOSIS] No active nodes found, skipping")
		return nil
	}

	log.Infof(ctx, "[DIAGNOSIS] Found %d active nodes, enqueueing diagnosis tasks", len(nodes))

	// Enqueue outbound diagnosis for each node's IP
	successCount := 0
	for _, node := range nodes {
		if _, err := EnqueueDiagnosisOutbound(ctx, node.Ipv4); err != nil {
			log.Warnf(ctx, "[DIAGNOSIS] Failed to enqueue diagnosis for IP %s: %v", node.Ipv4, err)
			continue
		}
		successCount++
	}

	log.Infof(ctx, "[DIAGNOSIS] Batch diagnosis initiated: total=%d, enqueued=%d", len(nodes), successCount)
	return nil
}

// EnqueueDiagnosisOutbound enqueues an outbound diagnosis task for an IP
func EnqueueDiagnosisOutbound(ctx context.Context, ip string) (string, error) {
	cfg := configDiagnosis()
	if !cfg.Enabled {
		log.Debugf(ctx, "[DIAGNOSIS] Route diagnosis is disabled, skipping ip=%s", ip)
		return "", nil
	}

	payload := DiagnosisOutboundPayload{
		IP: ip,
	}

	// Use unique option to prevent duplicate tasks for the same IP
	// 3 minutes window: covers ~60s detection time + buffer
	// After task completes (success), unique key is released immediately
	// After task fails, client can retry after 3 minutes
	info, err := asynq.Enqueue(TaskTypeDiagnosisOutbound, payload, hibikenAsynq.Unique(3*time.Minute))
	if err != nil {
		// ErrDuplicateTask means task is already enqueued - this is expected
		// Client will retry and eventually get data
		if err == hibikenAsynq.ErrDuplicateTask {
			log.Debugf(ctx, "[DIAGNOSIS] Task already enqueued for IP %s, skipping", ip)
			return "", nil
		}
		return "", fmt.Errorf("enqueue diagnosis task failed: %w", err)
	}

	log.Infof(ctx, "[DIAGNOSIS] Outbound diagnosis task enqueued: taskId=%s, ip=%s", info.ID, ip)
	return info.ID, nil
}

// SaveInboundDiagnosis saves inbound diagnosis result from Slave
func SaveInboundDiagnosis(ctx context.Context, ip string, routeMatrix map[string]string, probeCount, successCount int) error {
	routeMapJSON, _ := json.Marshal(routeMatrix)

	info := IPRouteInfo{
		IP:           ip,
		Direction:    DiagnosisDirectionInbound,
		RouteMatrix:  string(routeMapJSON),
		ProbeCount:   probeCount,
		SuccessCount: successCount,
		DiagnosedAt:  time.Now(),
	}

	err := db.Get().Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "ip"}, {Name: "direction"}},
		DoUpdates: clause.AssignmentColumns([]string{"route_matrix", "probe_count", "success_count", "diagnosed_at", "updated_at"}),
	}).Create(&info).Error

	if err != nil {
		log.Errorf(ctx, "[DIAGNOSIS] Failed to save inbound diagnosis: ip=%s, error=%v", ip, err)
		return fmt.Errorf("failed to save inbound diagnosis: %w", err)
	}

	log.Infof(ctx, "[DIAGNOSIS] Inbound diagnosis saved: ip=%s, probes=%d, success=%d",
		ip, probeCount, successCount)
	return nil
}
