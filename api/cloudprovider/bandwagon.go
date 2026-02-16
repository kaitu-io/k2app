package cloudprovider

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/strahe/bwh/pkg/client"
	"github.com/wordgate/qtoolkit/log"
)

// BandwagonProvider implements Provider for BandwagonHost (KiwiVM) using strahe/bwh SDK
type BandwagonProvider struct {
	veid   string
	apiKey string
	client *client.Client
}

// NewBandwagonProvider creates a new BandwagonHost provider
func NewBandwagonProvider(veid, apiKey string) *BandwagonProvider {
	return &BandwagonProvider{
		veid:   veid,
		apiKey: apiKey,
		client: client.NewClient(apiKey, veid),
	}
}

func (p *BandwagonProvider) Name() string {
	return ProviderBandwagon
}

func (p *BandwagonProvider) GetInstanceStatus(ctx context.Context, instanceID string) (*InstanceStatus, error) {
	info, err := p.client.GetServiceInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("bandwagon API request failed: %w", err)
	}

	// Extract IPv4 and IPv6 addresses
	ipv4Address := ""
	ipv6Address := ""
	for _, ip := range info.IPAddresses {
		if strings.Contains(ip, ":") {
			// IPv6 address or subnet
			if ipv6Address == "" {
				ipv6Address = ip
			}
		} else {
			// IPv4 address
			if ipv4Address == "" {
				ipv4Address = ip
			}
		}
	}

	// Calculate traffic with multiplier
	trafficUsed := info.DataCounter
	trafficTotal := info.PlanMonthlyData
	if info.MonthlyDataMultiplier > 1 {
		trafficUsed = trafficUsed * int64(info.MonthlyDataMultiplier)
		trafficTotal = trafficTotal * int64(info.MonthlyDataMultiplier)
	}

	// Determine state - for basic info we assume running unless suspended
	state := "running"
	if info.Suspended {
		state = "suspended"
	}

	return &InstanceStatus{
		InstanceID:        p.veid,
		Name:              info.Hostname,
		IPAddress:         ipv4Address,
		IPv6Address:       ipv6Address,
		Region:            info.NodeDatacenter,
		TrafficUsedBytes:  trafficUsed,
		TrafficTotalBytes: trafficTotal,
		TrafficResetAt:    time.Unix(info.DataNextReset, 0),
		ExpiresAt:         time.Time{}, // BandwagonHost doesn't expose expiry via API
		State:             state,
	}, nil
}

// GetLiveInstanceStatus gets real-time status including VPS state (running/stopped)
// Note: This call may take up to 15 seconds
func (p *BandwagonProvider) GetLiveInstanceStatus(ctx context.Context) (*InstanceStatus, error) {
	info, err := p.client.GetLiveServiceInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("bandwagon API request failed: %w", err)
	}

	// Extract IPv4 and IPv6 addresses
	ipv4Address := ""
	ipv6Address := ""
	for _, ip := range info.IPAddresses {
		if strings.Contains(ip, ":") {
			if ipv6Address == "" {
				ipv6Address = ip
			}
		} else {
			if ipv4Address == "" {
				ipv4Address = ip
			}
		}
	}

	// Calculate traffic with multiplier
	trafficUsed := info.DataCounter
	trafficTotal := info.PlanMonthlyData
	if info.MonthlyDataMultiplier > 1 {
		trafficUsed = trafficUsed * int64(info.MonthlyDataMultiplier)
		trafficTotal = trafficTotal * int64(info.MonthlyDataMultiplier)
	}

	// Use VeStatus from live info for actual state
	state := strings.ToLower(info.VeStatus)
	if state == "" {
		state = "unknown"
	}
	if info.Suspended {
		state = "suspended"
	}

	return &InstanceStatus{
		InstanceID:        p.veid,
		Name:              info.Hostname,
		IPAddress:         ipv4Address,
		IPv6Address:       ipv6Address,
		Region:            info.NodeDatacenter,
		TrafficUsedBytes:  trafficUsed,
		TrafficTotalBytes: trafficTotal,
		TrafficResetAt:    time.Unix(info.DataNextReset, 0),
		ExpiresAt:         time.Time{},
		State:             state,
	}, nil
}

func (p *BandwagonProvider) ListInstances(ctx context.Context) ([]*InstanceStatus, error) {
	// BandwagonHost: one VEID = one instance
	status, err := p.GetInstanceStatus(ctx, p.veid)
	if err != nil {
		return nil, err
	}
	return []*InstanceStatus{status}, nil
}

func (p *BandwagonProvider) ChangeIP(ctx context.Context, instanceID string, opts ChangeIPOptions) (*OperationResult, error) {
	if opts.TargetRegion == "" {
		return nil, fmt.Errorf("target_region is required for BandwagonHost migration")
	}

	log.Infof(ctx, "[BANDWAGON] Starting migration: veid=%s, target=%s", p.veid, opts.TargetRegion)

	// Step 1: Stop the instance
	if err := p.client.Stop(ctx); err != nil {
		return nil, fmt.Errorf("failed to stop instance: %w", err)
	}

	// Step 2: Wait for stopped state (poll for up to 2 minutes)
	if err := p.waitForState(ctx, "stopped", 2*time.Minute); err != nil {
		return nil, fmt.Errorf("instance did not stop in time: %w", err)
	}

	// Step 3: Initiate migration using SDK
	migrateResp, err := p.client.StartMigration(ctx, opts.TargetRegion)
	if err != nil {
		return nil, fmt.Errorf("migration failed: %w", err)
	}

	// Step 4: Wait for migration to complete (poll for up to 30 minutes)
	if err := p.waitForState(ctx, "running", 30*time.Minute); err != nil {
		log.Warnf(ctx, "[BANDWAGON] Migration may still be in progress: %v", err)
	}

	// Get new IP from migration response or fetch status
	newIP := ""
	if len(migrateResp.NewIPs) > 0 {
		newIP = migrateResp.NewIPs[0]
	} else {
		// Fallback: get new status
		status, err := p.GetInstanceStatus(ctx, p.veid)
		if err == nil {
			newIP = status.IPAddress
		}
	}

	log.Infof(ctx, "[BANDWAGON] Migration completed: new_ip=%s", newIP)

	return &OperationResult{
		Success: true,
		Message: "Migration completed successfully",
		Data: map[string]any{
			"new_ip": newIP,
			"region": opts.TargetRegion,
		},
	}, nil
}

func (p *BandwagonProvider) CreateInstance(ctx context.Context, opts CreateInstanceOptions) (*OperationResult, error) {
	return nil, &NotSupportedError{Provider: ProviderBandwagon, Operation: "CreateInstance"}
}

func (p *BandwagonProvider) DeleteInstance(ctx context.Context, instanceID string) (*OperationResult, error) {
	return nil, &NotSupportedError{Provider: ProviderBandwagon, Operation: "DeleteInstance"}
}

func (p *BandwagonProvider) ListRegions(ctx context.Context) ([]RegionInfo, error) {
	// Use SDK to get available migration locations
	locations, err := p.client.GetMigrateLocations(ctx)
	if err != nil {
		return nil, &NotSupportedError{Provider: ProviderBandwagon, Operation: "ListRegions"}
	}

	var regions []RegionInfo
	for _, loc := range locations.Locations {
		desc := loc
		if d, ok := locations.Descriptions[loc]; ok {
			desc = d
		}
		regions = append(regions, RegionInfo{
			Slug:       loc,
			NameEN:     desc,
			NameZH:     desc,
			Country:    "",
			ProviderID: loc,
			Available:  true,
		})
	}

	return regions, nil
}

func (p *BandwagonProvider) ListPlans(ctx context.Context, region string) ([]PlanInfo, error) {
	return nil, &NotSupportedError{Provider: ProviderBandwagon, Operation: "ListPlans"}
}

func (p *BandwagonProvider) ListImages(ctx context.Context, region string) ([]ImageInfo, error) {
	return nil, &NotSupportedError{Provider: ProviderBandwagon, Operation: "ListImages"}
}

// waitForState polls until instance reaches target state
func (p *BandwagonProvider) waitForState(ctx context.Context, targetState string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		// Use live status for accurate state checking
		status, err := p.GetLiveInstanceStatus(ctx)
		if err != nil {
			log.Warnf(ctx, "[BANDWAGON] Error checking status: %v", err)
		} else if status.State == targetState {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Second):
		}
	}
	return fmt.Errorf("timeout waiting for state: %s", targetState)
}

// BandwagonInstanceConfig holds config for a single Bandwagon instance
type BandwagonInstanceConfig struct {
	VEID   string
	APIKey string
}

// MultiBandwagonProvider manages multiple Bandwagon instances under one account
type MultiBandwagonProvider struct {
	instances []*BandwagonProvider
	veidMap   map[string]*BandwagonProvider
}

// NewMultiBandwagonProvider creates a provider that manages multiple Bandwagon instances
func NewMultiBandwagonProvider(configs []BandwagonInstanceConfig) *MultiBandwagonProvider {
	mp := &MultiBandwagonProvider{
		instances: make([]*BandwagonProvider, 0, len(configs)),
		veidMap:   make(map[string]*BandwagonProvider),
	}
	for _, cfg := range configs {
		if cfg.VEID != "" && cfg.APIKey != "" {
			p := NewBandwagonProvider(cfg.VEID, cfg.APIKey)
			mp.instances = append(mp.instances, p)
			mp.veidMap[cfg.VEID] = p
		}
	}
	return mp
}

func (mp *MultiBandwagonProvider) Name() string {
	return ProviderBandwagon
}

func (mp *MultiBandwagonProvider) GetInstanceStatus(ctx context.Context, instanceID string) (*InstanceStatus, error) {
	p, ok := mp.veidMap[instanceID]
	if !ok {
		return nil, fmt.Errorf("instance not found: %s", instanceID)
	}
	return p.GetInstanceStatus(ctx, instanceID)
}

func (mp *MultiBandwagonProvider) ListInstances(ctx context.Context) ([]*InstanceStatus, error) {
	var allStatuses []*InstanceStatus
	for _, p := range mp.instances {
		statuses, err := p.ListInstances(ctx)
		if err != nil {
			log.Warnf(ctx, "[BANDWAGON] Failed to list instances for veid=%s: %v", p.veid, err)
			continue
		}
		allStatuses = append(allStatuses, statuses...)
	}
	return allStatuses, nil
}

func (mp *MultiBandwagonProvider) ChangeIP(ctx context.Context, instanceID string, opts ChangeIPOptions) (*OperationResult, error) {
	p, ok := mp.veidMap[instanceID]
	if !ok {
		return nil, fmt.Errorf("instance not found: %s", instanceID)
	}
	return p.ChangeIP(ctx, instanceID, opts)
}

func (mp *MultiBandwagonProvider) CreateInstance(ctx context.Context, opts CreateInstanceOptions) (*OperationResult, error) {
	return nil, &NotSupportedError{Provider: ProviderBandwagon, Operation: "CreateInstance"}
}

func (mp *MultiBandwagonProvider) DeleteInstance(ctx context.Context, instanceID string) (*OperationResult, error) {
	return nil, &NotSupportedError{Provider: ProviderBandwagon, Operation: "DeleteInstance"}
}

func (mp *MultiBandwagonProvider) ListRegions(ctx context.Context) ([]RegionInfo, error) {
	// Use the first provider to get regions
	if len(mp.instances) > 0 {
		return mp.instances[0].ListRegions(ctx)
	}
	return nil, &NotSupportedError{Provider: ProviderBandwagon, Operation: "ListRegions"}
}

func (mp *MultiBandwagonProvider) ListPlans(ctx context.Context, region string) ([]PlanInfo, error) {
	return nil, &NotSupportedError{Provider: ProviderBandwagon, Operation: "ListPlans"}
}

func (mp *MultiBandwagonProvider) ListImages(ctx context.Context, region string) ([]ImageInfo, error) {
	return nil, &NotSupportedError{Provider: ProviderBandwagon, Operation: "ListImages"}
}
