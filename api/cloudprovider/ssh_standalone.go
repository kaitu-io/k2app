package cloudprovider

import (
	"context"
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
)

const (
	// TrafficConfigPath is the path to store traffic configuration on slave nodes.
	// Located in ubuntu home for easier user access and maintenance.
	TrafficConfigPath = "/home/ubuntu/.kaitu/traffic-config.yaml"

	// TrafficConfigDir is the directory containing the config file
	TrafficConfigDir = "/home/ubuntu/.kaitu"

	// DefaultTrafficTotalBytes is the default traffic limit (2TB)
	DefaultTrafficTotalBytes = 2 * 1024 * 1024 * 1024 * 1024 // 2TB
)

// TrafficConfig stores traffic configuration for SSH standalone instances.
// This config is stored on each slave node at TrafficConfigPath in YAML format.
type TrafficConfig struct {
	// TrafficTotalBytes is the total traffic allowance in bytes (default: 2TB)
	TrafficTotalBytes int64 `yaml:"traffic_total_bytes"`

	// TrafficResetAt is the Unix timestamp when traffic tracking started
	TrafficResetAt int64 `yaml:"traffic_reset_at"`
}

// SSHStandaloneProvider manages hosts via SSH without cloud provider API.
// It automatically detects SlaveNodes that don't have a corresponding CloudInstance
// record and treats them as SSH standalone instances for traffic monitoring.
type SSHStandaloneProvider struct {
	db *gorm.DB
}

// slaveNodeRecord mirrors the center.SlaveNode model to avoid circular imports.
// Note: All nodes in DB are active by design - no IsAlive field needed.
type slaveNodeRecord struct {
	ID      uint64
	Ipv4    string
	Ipv6    string
	Name    string
	Country string
	Region  string
}

// NewSSHStandaloneProvider creates a new SSH standalone provider.
// All SlaveNodes without CloudInstance are automatically included.
func NewSSHStandaloneProvider(db *gorm.DB) *SSHStandaloneProvider {
	return &SSHStandaloneProvider{
		db: db,
	}
}

// Name returns the provider identifier.
func (p *SSHStandaloneProvider) Name() string {
	return ProviderSSHStandalone
}

// GetInstanceStatus retrieves current instance status including traffic.
// instanceID is the SlaveNode IPv4 address.
func (p *SSHStandaloneProvider) GetInstanceStatus(ctx context.Context, instanceID string) (*InstanceStatus, error) {
	record, err := p.getOrphanSlaveNode(instanceID)
	if err != nil {
		return nil, err
	}

	return p.fetchInstanceStatus(ctx, record)
}

// ListInstances lists all SlaveNodes that don't have a corresponding CloudInstance.
func (p *SSHStandaloneProvider) ListInstances(ctx context.Context) ([]*InstanceStatus, error) {
	records, err := p.listOrphanSlaveNodes()
	if err != nil {
		return nil, err
	}

	statuses := make([]*InstanceStatus, 0, len(records))
	for i := range records {
		status, err := p.fetchInstanceStatus(ctx, &records[i])
		if err != nil {
			// Create fallback status on error
			status = p.fallbackStatus(&records[i], err.Error())
		}
		statuses = append(statuses, status)
	}

	return statuses, nil
}

// listOrphanSlaveNodes returns SlaveNodes that have no corresponding CloudInstance.
// This uses a subquery: SELECT * FROM slave_nodes WHERE ipv4 NOT IN (SELECT ip_address FROM cloud_instances WHERE deleted_at IS NULL)
func (p *SSHStandaloneProvider) listOrphanSlaveNodes() ([]slaveNodeRecord, error) {
	var records []slaveNodeRecord

	// Subquery to get all IP addresses from cloud_instances
	subquery := p.db.Table("cloud_instances").
		Select("ip_address").
		Where("deleted_at IS NULL")

	// Query slave_nodes where ipv4 is NOT in the cloud_instances
	err := p.db.Table("slave_nodes").
		Where("deleted_at IS NULL").
		Where("ipv4 NOT IN (?)", subquery).
		Find(&records).Error

	if err != nil {
		return nil, fmt.Errorf("list orphan slave nodes: %w", err)
	}

	return records, nil
}

// getOrphanSlaveNode returns a SlaveNode by IP if it has no CloudInstance.
func (p *SSHStandaloneProvider) getOrphanSlaveNode(ip string) (*slaveNodeRecord, error) {
	var record slaveNodeRecord

	// First check if this SlaveNode exists
	err := p.db.Table("slave_nodes").
		Where("ipv4 = ? AND deleted_at IS NULL", ip).
		First(&record).Error
	if err != nil {
		return nil, fmt.Errorf("slave node not found: %s", ip)
	}

	// Check if there's a CloudInstance for this IP
	var count int64
	p.db.Table("cloud_instances").
		Where("ip_address = ? AND deleted_at IS NULL", ip).
		Count(&count)

	if count > 0 {
		return nil, fmt.Errorf("slave node %s has a cloud instance (not an orphan)", ip)
	}

	return &record, nil
}

// SSHExecByIPFunc is a function type for executing SSH commands on a node by its IP.
// This allows dependency injection of the SlaveNode.SSHExec method lookup.
type SSHExecByIPFunc func(ctx context.Context, ip string, command string) (stdout string, err error)

// sshExecByIP is the injected function for SSH execution via SlaveNode lookup.
var sshExecByIP SSHExecByIPFunc

// SetSSHExecByIP sets the SSH execution function that looks up SlaveNode by IP.
// This should be called during center initialization.
func SetSSHExecByIP(fn SSHExecByIPFunc) {
	sshExecByIP = fn
}

// fetchTrafficConfig reads the traffic config from slave node via SSH.
// If the config doesn't exist, it creates one with default values.
func (p *SSHStandaloneProvider) fetchTrafficConfig(ctx context.Context, ip string) (*TrafficConfig, error) {
	if sshExecByIP == nil {
		return nil, fmt.Errorf("SSH execution function not configured")
	}

	// Try to read existing config
	cmd := fmt.Sprintf("cat %s 2>/dev/null", TrafficConfigPath)
	stdout, err := sshExecByIP(ctx, ip, cmd)

	if err == nil && stdout != "" {
		// Parse existing config
		var config TrafficConfig
		if yamlErr := yaml.Unmarshal([]byte(stdout), &config); yamlErr == nil {
			return &config, nil
		}
		// Invalid YAML, will recreate
	}

	// Config doesn't exist or is invalid - create default config
	now := time.Now()
	defaultConfig := TrafficConfig{
		TrafficTotalBytes: DefaultTrafficTotalBytes,
		TrafficResetAt:    now.Unix(),
	}

	// Create config file on remote host with YAML format and comments
	configContent := generateTrafficConfigYAML(&defaultConfig)
	createCmd := fmt.Sprintf("mkdir -p %s && cat > %s << 'EOF'\n%sEOF", TrafficConfigDir, TrafficConfigPath, configContent)
	_, err = sshExecByIP(ctx, ip, createCmd)
	if err != nil {
		// Failed to create, return default config anyway (will retry next sync)
		return &defaultConfig, nil
	}

	return &defaultConfig, nil
}

// generateTrafficConfigYAML generates YAML content with comments for traffic config.
func generateTrafficConfigYAML(config *TrafficConfig) string {
	resetTime := time.Unix(config.TrafficResetAt, 0).Format("2006-01-02 15:04:05")
	totalGB := float64(config.TrafficTotalBytes) / (1024 * 1024 * 1024)

	return fmt.Sprintf(`# Kaitu Traffic Configuration
# This file is managed by kaitu-center, but you can manually edit it.
# Changes will be preserved during sync.

# Total traffic allowance in bytes (default: 2TB = 2199023255552)
# Current value: %.2f GB
traffic_total_bytes: %d

# Unix timestamp when traffic tracking started
# Human readable: %s
traffic_reset_at: %d
`, totalGB, config.TrafficTotalBytes, resetTime, config.TrafficResetAt)
}

// UpdateTrafficConfig updates the traffic config on a slave node via SSH.
func (p *SSHStandaloneProvider) UpdateTrafficConfig(ctx context.Context, ip string, config *TrafficConfig) error {
	if sshExecByIP == nil {
		return fmt.Errorf("SSH execution function not configured")
	}

	configContent := generateTrafficConfigYAML(config)
	cmd := fmt.Sprintf("mkdir -p %s && cat > %s << 'EOF'\n%sEOF", TrafficConfigDir, TrafficConfigPath, configContent)
	_, err := sshExecByIP(ctx, ip, cmd)
	if err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	return nil
}

// fetchInstanceStatus connects via SSH to get real-time traffic stats.
func (p *SSHStandaloneProvider) fetchInstanceStatus(ctx context.Context, record *slaveNodeRecord) (*InstanceStatus, error) {
	if sshExecByIP == nil {
		return nil, fmt.Errorf("SSH execution function not configured")
	}

	// Fetch traffic config (creates default if not exists)
	trafficConfig, configErr := p.fetchTrafficConfig(ctx, record.Ipv4)
	if configErr != nil {
		// Use defaults if config fetch fails
		trafficConfig = &TrafficConfig{
			TrafficTotalBytes: DefaultTrafficTotalBytes,
			TrafficResetAt:    time.Now().Unix(),
		}
	}

	// Use eth0 as default interface
	iface := "eth0"

	cmd := fmt.Sprintf("vnstat -i %s --json m 2>/dev/null", iface)
	stdout, err := sshExecByIP(ctx, record.Ipv4, cmd)

	var trafficStats *TrafficStats
	var lastError string

	if err != nil {
		lastError = err.Error()
	} else {
		trafficStats, err = parseVnstatJSON([]byte(stdout), iface, time.Now())
		if err != nil {
			lastError = err.Error()
		}
	}

	var usedBytes int64
	if trafficStats != nil {
		usedBytes = trafficStats.UsedBytes
	}

	state := "running" // All nodes in DB are active by design
	if lastError != "" {
		state = "error"
	}

	return &InstanceStatus{
		InstanceID:        record.Ipv4, // Use IPv4 as instance ID
		Name:              record.Name,
		IPAddress:         record.Ipv4,
		IPv6Address:       record.Ipv6,
		Region:            record.Region,
		TrafficUsedBytes:  usedBytes,
		TrafficTotalBytes: trafficConfig.TrafficTotalBytes,
		TrafficResetAt:    time.Unix(trafficConfig.TrafficResetAt, 0),
		ExpiresAt:         time.Time{},
		State:             state,
	}, nil
}

// fallbackStatus creates a status when SSH connection fails.
func (p *SSHStandaloneProvider) fallbackStatus(record *slaveNodeRecord, _ string) *InstanceStatus {
	return &InstanceStatus{
		InstanceID:        record.Ipv4,
		Name:              record.Name,
		IPAddress:         record.Ipv4,
		IPv6Address:       record.Ipv6,
		Region:            record.Region,
		TrafficUsedBytes:  0,
		TrafficTotalBytes: DefaultTrafficTotalBytes,
		TrafficResetAt:    time.Time{},
		ExpiresAt:         time.Time{},
		State:             "error",
	}
}

// CreateInstance is not supported - SlaveNodes are managed separately.
func (p *SSHStandaloneProvider) CreateInstance(ctx context.Context, opts CreateInstanceOptions) (*OperationResult, error) {
	return nil, &NotSupportedError{Operation: "CreateInstance", Provider: ProviderSSHStandalone}
}

// DeleteInstance is not supported - SlaveNodes are managed separately.
func (p *SSHStandaloneProvider) DeleteInstance(ctx context.Context, instanceID string) (*OperationResult, error) {
	return nil, &NotSupportedError{Operation: "DeleteInstance", Provider: ProviderSSHStandalone}
}

// ChangeIP is not supported for standalone instances.
func (p *SSHStandaloneProvider) ChangeIP(ctx context.Context, instanceID string, opts ChangeIPOptions) (*OperationResult, error) {
	return nil, &NotSupportedError{Operation: "ChangeIP", Provider: ProviderSSHStandalone}
}

// ListRegions returns empty list - standalone instances don't have regions.
func (p *SSHStandaloneProvider) ListRegions(ctx context.Context) ([]RegionInfo, error) {
	return []RegionInfo{}, nil
}

// ListPlans returns empty list - standalone instances don't have plans.
func (p *SSHStandaloneProvider) ListPlans(ctx context.Context, region string) ([]PlanInfo, error) {
	return []PlanInfo{}, nil
}

// ListImages returns empty list - standalone instances don't have images.
func (p *SSHStandaloneProvider) ListImages(ctx context.Context, region string) ([]ImageInfo, error) {
	return []ImageInfo{}, nil
}
