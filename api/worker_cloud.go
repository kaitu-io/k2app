package center

import (
	"context"
	"fmt"
	"time"

	hibikenAsynq "github.com/hibiken/asynq"
	"github.com/wordgate/qtoolkit/asynq"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/slack"
	"gorm.io/gorm"

	"github.com/kaitu-io/k2app/api/cloudprovider"
)

const (
	// Cron task: sync all cloud instance status
	TaskTypeCloudSyncAll = "cloud:sync:all"

	// Manual tasks: scheduled to run at night (UTC+8)
	TaskTypeCloudChangeIP = "cloud:change_ip"
	TaskTypeCloudCreate   = "cloud:create"
	TaskTypeCloudDelete   = "cloud:delete"
)

// Task payloads
type CloudSyncPayload struct {
	AccountName string `json:"account_name,omitempty"` // Empty = sync all accounts
}

type CloudChangeIPPayload struct {
	CloudInstanceID uint64 `json:"cloud_instance_id"`
	TargetRegion    string `json:"target_region,omitempty"` // For BandwagonHost
}

type CloudCreatePayload struct {
	AccountName string `json:"account_name"`
	Region      string `json:"region"`
	Plan        string `json:"plan"`
	ImageID     string `json:"image_id"`
	Name        string `json:"name"`
}

type CloudDeletePayload struct {
	CloudInstanceID uint64 `json:"cloud_instance_id"`
}

// accountToProviderConfig converts a CloudInstanceAccount to cloudprovider.ProviderConfig
func accountToProviderConfig(account *CloudInstanceAccount) cloudprovider.ProviderConfig {
	cfg := cloudprovider.ProviderConfig{
		Provider:        account.Provider,
		AccessKeyID:     account.AccessKeyID,
		AccessKeySecret: account.AccessKeySecret,
		SecretAccessKey: account.SecretAccessKey,
		Region:          account.Region,
		VEID:            account.VEID,
		APIKey:          account.APIKey,
	}

	// Convert Bandwagon instances
	for _, inst := range account.Instances {
		cfg.Instances = append(cfg.Instances, cloudprovider.BandwagonInstanceConfig{
			VEID:   inst.VEID,
			APIKey: inst.APIKey,
		})
	}

	return cfg
}

// sshExecBySlaveNodeIP executes an SSH command by looking up the SlaveNode by its IPv4 address.
// This allows cloudprovider to use the system's SSH keypair instead of per-instance credentials.
func sshExecBySlaveNodeIP(ctx context.Context, ip string, command string) (string, error) {
	var node SlaveNode
	if err := db.Get().Where("ipv4 = ?", ip).First(&node).Error; err != nil {
		return "", fmt.Errorf("slave node not found for IP %s: %w", ip, err)
	}

	result, err := node.SSHExec(ctx, command)
	if err != nil {
		return "", err
	}

	if result.ExitCode != 0 {
		return result.Stdout, fmt.Errorf("command exited with code %d: %s", result.ExitCode, result.Stderr)
	}

	return result.Stdout, nil
}

// RegisterCloudWorker registers cloud management task handlers and cron jobs
func RegisterCloudWorker() {
	cfg := ConfigCloudInstance()
	if !cfg.Sync.Enabled {
		log.Infof(context.Background(), "[CLOUD] Cloud sync is disabled")
		return
	}

	// Initialize SSH execution via SlaveNode lookup
	cloudprovider.SetSSHExecByIP(sshExecBySlaveNodeIP)

	// Register task handlers
	asynq.Handle(TaskTypeCloudSyncAll, handleCloudSyncAll)
	asynq.Handle(TaskTypeCloudChangeIP, withSlackNotify(handleCloudChangeIP))
	asynq.Handle(TaskTypeCloudCreate, withSlackNotify(handleCloudCreate))
	asynq.Handle(TaskTypeCloudDelete, withSlackNotify(handleCloudDelete))

	// Register cron for status sync
	asynq.Cron(cfg.Sync.Cron, TaskTypeCloudSyncAll, nil, hibikenAsynq.Unique(25*time.Minute))

	log.Infof(context.Background(), "[CLOUD] Cloud worker registered (cron: %s)", cfg.Sync.Cron)
}

// withSlackNotify wraps handler to send Slack notification on failure
func withSlackNotify(handler func(context.Context, []byte) error) func(context.Context, []byte) error {
	return func(ctx context.Context, payload []byte) error {
		err := handler(ctx, payload)
		if err != nil {
			sendCloudSlackNotification(ctx, "Cloud Task Failed", err.Error())
		}
		return err
	}
}

func sendCloudSlackNotification(ctx context.Context, title, message string) {
	text := fmt.Sprintf(":warning: *%s*\n\n%s\n\n_Time: %s_",
		title, message, time.Now().Format("2006-01-02 15:04:05 MST"))
	if err := slack.Send("cloud-alerts", text); err != nil {
		log.Errorf(ctx, "[CLOUD] Failed to send Slack notification: %v", err)
	}
}

func handleCloudSyncAll(ctx context.Context, payload []byte) error {
	var p CloudSyncPayload
	if len(payload) > 0 {
		if err := asynq.Unmarshal(payload, &p); err != nil {
			return fmt.Errorf("unmarshal payload failed: %w", err)
		}
	}

	log.Infof(ctx, "[CLOUD] Starting cloud sync: account=%s", p.AccountName)

	database := db.Get()

	// Always sync SSH standalone provider (auto-detects orphan SlaveNodes)
	if p.AccountName == "" || p.AccountName == "ssh_standalone" {
		if err := syncSSHStandalone(ctx, database); err != nil {
			log.Errorf(ctx, "[CLOUD] Failed to sync ssh_standalone: %v", err)
		}
	}

	// Sync configured API-based accounts
	accounts := ConfigCloudInstance().Accounts
	for _, account := range accounts {
		if p.AccountName != "" && account.Name != p.AccountName {
			continue
		}

		if err := syncAccount(ctx, database, account); err != nil {
			log.Errorf(ctx, "[CLOUD] Failed to sync account %s: %v", account.Name, err)
			// Continue with other accounts
		}
	}

	log.Infof(ctx, "[CLOUD] Cloud sync completed")
	return nil
}

// syncSSHStandalone syncs SSH standalone instances (SlaveNodes without CloudInstance)
func syncSSHStandalone(ctx context.Context, database *gorm.DB) error {
	log.Infof(ctx, "[CLOUD] Syncing ssh_standalone (auto-detecting orphan SlaveNodes)")

	provider := cloudprovider.NewSSHStandaloneProvider(database)

	instances, err := provider.ListInstances(ctx)
	if err != nil {
		return fmt.Errorf("failed to list instances: %w", err)
	}

	log.Infof(ctx, "[CLOUD] Found %d orphan SlaveNodes for ssh_standalone", len(instances))

	// Track synced instance IDs for orphan detection
	syncedIDs := make(map[string]bool)

	for _, inst := range instances {
		if err := upsertCloudInstance(ctx, CloudInstanceAccount{
			Provider: cloudprovider.ProviderSSHStandalone,
			Name:     "ssh_standalone",
		}, inst); err != nil {
			log.Errorf(ctx, "[CLOUD] Failed to upsert instance %s: %v", inst.InstanceID, err)
		}
		syncedIDs[inst.InstanceID] = true
	}

	// Orphan detection: mark instances that no longer exist as deleted
	if err := markOrphanedInstances(ctx, CloudInstanceAccount{
		Provider: cloudprovider.ProviderSSHStandalone,
		Name:     "ssh_standalone",
	}, syncedIDs); err != nil {
		log.Errorf(ctx, "[CLOUD] Failed to mark orphaned ssh_standalone instances: %v", err)
	}

	return nil
}

func syncAccount(ctx context.Context, database *gorm.DB, account CloudInstanceAccount) error {
	log.Infof(ctx, "[CLOUD] Syncing account: name=%s, provider=%s, region=%s",
		account.Name, account.Provider, account.Region)

	// API-based providers use the factory
	cfg := accountToProviderConfig(&account)
	log.Debugf(ctx, "[CLOUD] Provider config: provider=%s, region=%s, has_access_key=%v, has_secret=%v",
		cfg.Provider, cfg.Region, cfg.AccessKeyID != "", cfg.AccessKeySecret != "" || cfg.SecretAccessKey != "")

	provider, err := cloudprovider.NewProvider(cfg)
	if err != nil {
		return fmt.Errorf("failed to create provider: %w", err)
	}

	log.Infof(ctx, "[CLOUD] Listing instances for account: %s", account.Name)
	instances, err := provider.ListInstances(ctx)
	if err != nil {
		return fmt.Errorf("failed to list instances: %w", err)
	}

	log.Infof(ctx, "[CLOUD] Found %d instances for account: %s", len(instances), account.Name)

	// Track synced instance IDs for orphan detection
	syncedIDs := make(map[string]bool)

	for _, inst := range instances {
		if err := upsertCloudInstance(ctx, account, inst); err != nil {
			log.Errorf(ctx, "[CLOUD] Failed to upsert instance %s: %v", inst.InstanceID, err)
		}
		syncedIDs[inst.InstanceID] = true
	}

	// Orphan detection: mark instances that no longer exist as deleted
	if err := markOrphanedInstances(ctx, account, syncedIDs); err != nil {
		log.Errorf(ctx, "[CLOUD] Failed to mark orphaned instances: %v", err)
	}

	return nil
}

// markOrphanedInstances marks instances as "deleted" if they no longer exist in the provider
func markOrphanedInstances(ctx context.Context, account CloudInstanceAccount, syncedIDs map[string]bool) error {
	// Find all active instances in DB for this account/region
	var dbInstances []CloudInstance
	query := db.Get().Where(&CloudInstance{
		Provider:    account.Provider,
		AccountName: account.Name,
	})

	// Only filter by region if it's specified (some providers sync all regions at once)
	if account.Region != "" {
		query = query.Where("region = ?", account.Region)
	}

	if err := query.Find(&dbInstances).Error; err != nil {
		return fmt.Errorf("failed to query instances: %w", err)
	}

	// Mark orphaned instances
	orphanCount := 0
	for _, dbInst := range dbInstances {
		if !syncedIDs[dbInst.InstanceID] {
			log.Infof(ctx, "[CLOUD] Instance %s no longer exists in provider, marking as deleted (account=%s, ip=%s)",
				dbInst.InstanceID, account.Name, dbInst.IPAddress)

			if err := db.Get().Model(&dbInst).Update("last_synced_at", time.Now().Unix()).Error; err != nil {
				log.Errorf(ctx, "[CLOUD] Failed to update last_synced_at for instance %s: %v", dbInst.InstanceID, err)
			}
			if err := db.Get().Delete(&dbInst).Error; err != nil {
				log.Errorf(ctx, "[CLOUD] Failed to mark instance %s as deleted: %v", dbInst.InstanceID, err)
			} else {
				orphanCount++
			}
		}
	}

	if orphanCount > 0 {
		log.Infof(ctx, "[CLOUD] Marked %d orphaned instances as deleted for account: %s", orphanCount, account.Name)
	}

	return nil
}

func upsertCloudInstance(ctx context.Context, account CloudInstanceAccount, status *cloudprovider.InstanceStatus) error {
	now := time.Now().Unix()

	instance := CloudInstance{
		Provider:          account.Provider,
		AccountName:       account.Name,
		InstanceID:        status.InstanceID,
		Name:              status.Name,
		IPAddress:         status.IPAddress,
		IPv6Address:       status.IPv6Address,
		Region:            status.Region,
		TrafficUsedBytes:  status.TrafficUsedBytes,
		TrafficTotalBytes: status.TrafficTotalBytes,
		TrafficResetAt:    status.TrafficResetAt.Unix(),
		ExpiresAt:         status.ExpiresAt.Unix(),
		LastSyncedAt:      now,
		SyncError:         "",
	}

	// Upsert using provider + instance_id as unique key
	err := db.Get().Where("provider = ? AND instance_id = ?", account.Provider, status.InstanceID).
		Assign(map[string]any{
			"account_name":        instance.AccountName,
			"name":                instance.Name,
			"ip_address":          instance.IPAddress,
			"ipv6_address":        instance.IPv6Address,
			"region":              instance.Region,
			"traffic_used_bytes":  instance.TrafficUsedBytes,
			"traffic_total_bytes": instance.TrafficTotalBytes,
			"traffic_reset_at":    instance.TrafficResetAt,
			"expires_at":          instance.ExpiresAt,
			"last_synced_at":      instance.LastSyncedAt,
			"sync_error":          "",
		}).FirstOrCreate(&instance).Error

	if err != nil {
		return err
	}

	log.Debugf(ctx, "[CLOUD] Synced instance: %s/%s, name=%s, ip=%s, ipv6=%s, traffic=%d/%d",
		account.Name, status.InstanceID, status.Name, status.IPAddress, status.IPv6Address,
		status.TrafficUsedBytes, status.TrafficTotalBytes)

	return nil
}

func handleCloudChangeIP(ctx context.Context, payload []byte) error {
	var p CloudChangeIPPayload
	if err := asynq.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("unmarshal payload failed: %w", err)
	}

	log.Infof(ctx, "[CLOUD] Starting IP change: instance_id=%d", p.CloudInstanceID)

	// Get instance from DB
	var instance CloudInstance
	if err := db.Get().First(&instance, p.CloudInstanceID).Error; err != nil {
		return fmt.Errorf("instance not found: %w", err)
	}

	// Get account config
	account := ConfigCloudInstanceAccountByName(instance.AccountName)
	if account == nil {
		return fmt.Errorf("account not found: %s", instance.AccountName)
	}

	// Create provider
	provider, err := cloudprovider.NewProvider(accountToProviderConfig(account))
	if err != nil {
		return fmt.Errorf("failed to create provider: %w", err)
	}

	// Execute IP change
	result, err := provider.ChangeIP(ctx, instance.InstanceID, cloudprovider.ChangeIPOptions{
		TargetRegion: p.TargetRegion,
	})
	if err != nil {
		// Update sync error
		db.Get().Model(&instance).Update("sync_error", err.Error())
		return err
	}

	// Update instance with new IP
	if newIP, ok := result.Data["new_ip"].(string); ok && newIP != "" {
		db.Get().Model(&instance).Updates(map[string]any{
			"ip_address":     newIP,
			"last_synced_at": time.Now().Unix(),
			"sync_error":     "",
		})
	}

	log.Infof(ctx, "[CLOUD] IP change completed: %s", result.Message)

	// Send success notification
	sendCloudSlackNotification(ctx, "IP Change Completed",
		fmt.Sprintf("Instance: %s\nNew IP: %v", instance.InstanceID, result.Data["new_ip"]))

	return nil
}

func handleCloudCreate(ctx context.Context, payload []byte) error {
	var p CloudCreatePayload
	if err := asynq.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("unmarshal payload failed: %w", err)
	}

	log.Infof(ctx, "[CLOUD] Creating instance: account=%s, name=%s", p.AccountName, p.Name)

	account := ConfigCloudInstanceAccountByName(p.AccountName)
	if account == nil {
		return fmt.Errorf("account not found: %s", p.AccountName)
	}

	provider, err := cloudprovider.NewProvider(accountToProviderConfig(account))
	if err != nil {
		return fmt.Errorf("failed to create provider: %w", err)
	}

	result, err := provider.CreateInstance(ctx, cloudprovider.CreateInstanceOptions{
		Region:  p.Region,
		Plan:    p.Plan,
		ImageID: p.ImageID,
		Name:    p.Name,
	})
	if err != nil {
		return err
	}

	log.Infof(ctx, "[CLOUD] Instance created: %s", result.Message)

	// Send success notification
	sendCloudSlackNotification(ctx, "Instance Created",
		fmt.Sprintf("Account: %s\nInstance: %v", p.AccountName, result.Data))

	// Schedule sync task in 5 minutes to refresh instance status
	_, err = asynq.Enqueue(TaskTypeCloudSyncAll, CloudSyncPayload{
		AccountName: p.AccountName,
	}, hibikenAsynq.ProcessIn(5*time.Minute))
	if err != nil {
		log.Errorf(ctx, "[CLOUD] Failed to schedule sync task: %v", err)
	}

	return nil
}

func handleCloudDelete(ctx context.Context, payload []byte) error {
	var p CloudDeletePayload
	if err := asynq.Unmarshal(payload, &p); err != nil {
		return fmt.Errorf("unmarshal payload failed: %w", err)
	}

	log.Infof(ctx, "[CLOUD] Deleting instance: id=%d", p.CloudInstanceID)

	var instance CloudInstance
	if err := db.Get().First(&instance, p.CloudInstanceID).Error; err != nil {
		return fmt.Errorf("instance not found: %w", err)
	}

	account := ConfigCloudInstanceAccountByName(instance.AccountName)
	if account == nil {
		return fmt.Errorf("account not found: %s", instance.AccountName)
	}

	provider, err := cloudprovider.NewProvider(accountToProviderConfig(account))
	if err != nil {
		return fmt.Errorf("failed to create provider: %w", err)
	}

	result, err := provider.DeleteInstance(ctx, instance.InstanceID)
	if err != nil {
		return err
	}

	// Soft delete from DB
	db.Get().Delete(&instance)

	log.Infof(ctx, "[CLOUD] Instance deleted: %s", result.Message)

	// Send success notification
	sendCloudSlackNotification(ctx, "Instance Deleted",
		fmt.Sprintf("Account: %s\nInstance: %s", instance.AccountName, instance.InstanceID))

	return nil
}

// ScheduleCloudTask schedules a task to run at next UTC+8 2:00 AM
func ScheduleCloudTask(taskType string, payload any) (string, error) {
	loc, _ := time.LoadLocation("Asia/Shanghai")
	now := time.Now().In(loc)

	// Calculate next 2:00 AM
	next := time.Date(now.Year(), now.Month(), now.Day(), 2, 0, 0, 0, loc)
	if now.After(next) {
		next = next.Add(24 * time.Hour)
	}

	delay := time.Until(next)

	info, err := asynq.Enqueue(taskType, payload, hibikenAsynq.ProcessIn(delay))
	if err != nil {
		return "", err
	}

	log.Infof(context.Background(), "[CLOUD] Task scheduled: type=%s, id=%s, execute_at=%s",
		taskType, info.ID, next.Format("2006-01-02 15:04:05"))

	return info.ID, nil
}

// ScheduleCloudTaskImmediate enqueues a cloud task for immediate execution
func ScheduleCloudTaskImmediate(taskType string, payload any) (string, error) {
	info, err := asynq.Enqueue(taskType, payload)
	if err != nil {
		return "", err
	}

	log.Infof(context.Background(), "[CLOUD] Task enqueued for immediate execution: type=%s, id=%s",
		taskType, info.ID)

	return info.ID, nil
}
