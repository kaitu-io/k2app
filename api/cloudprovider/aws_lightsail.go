package cloudprovider

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/lightsail"
	"github.com/aws/aws-sdk-go-v2/service/lightsail/types"
	"github.com/wordgate/qtoolkit/log"
)

// AWSLightsailProvider implements Provider for AWS Lightsail
type AWSLightsailProvider struct {
	region string
	client *lightsail.Client
}

// NewAWSLightsailProvider creates a new AWS Lightsail provider
func NewAWSLightsailProvider(accessKeyID, secretAccessKey, region string) (*AWSLightsailProvider, error) {
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			accessKeyID,
			secretAccessKey,
			"",
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return &AWSLightsailProvider{
		region: region,
		client: lightsail.NewFromConfig(cfg),
	}, nil
}

func (p *AWSLightsailProvider) Name() string {
	return ProviderAWSLightsail
}

func (p *AWSLightsailProvider) GetInstanceStatus(ctx context.Context, instanceID string) (*InstanceStatus, error) {
	// Get instance info
	result, err := p.client.GetInstance(ctx, &lightsail.GetInstanceInput{
		InstanceName: aws.String(instanceID),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get instance: %w", err)
	}

	inst := result.Instance
	if inst == nil {
		return nil, fmt.Errorf("instance not found: %s", instanceID)
	}

	// Get traffic metrics for current month
	trafficUsed, trafficTotal := p.getTrafficMetrics(ctx, instanceID, inst)

	// Extract IPv4 address
	ipAddress := ""
	if inst.PublicIpAddress != nil {
		ipAddress = *inst.PublicIpAddress
	}

	// Extract IPv6 address (first one if available)
	ipv6Address := ""
	if len(inst.Ipv6Addresses) > 0 {
		ipv6Address = inst.Ipv6Addresses[0]
	}

	region := ""
	if inst.Location != nil {
		region = string(inst.Location.RegionName)
	}

	state := "unknown"
	if inst.State != nil && inst.State.Name != nil {
		state = *inst.State.Name
	}

	// AWS Lightsail resets on 1st of each month
	now := time.Now().UTC()
	nextMonth := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC)

	return &InstanceStatus{
		InstanceID:        instanceID,
		Name:              instanceID, // Lightsail uses instance name as ID
		IPAddress:         ipAddress,
		IPv6Address:       ipv6Address,
		Region:            region,
		TrafficUsedBytes:  trafficUsed,
		TrafficTotalBytes: trafficTotal,
		TrafficResetAt:    nextMonth,
		ExpiresAt:         time.Time{}, // Lightsail is on-demand, no expiry
		State:             state,
	}, nil
}

func (p *AWSLightsailProvider) getTrafficMetrics(ctx context.Context, instanceID string, inst *types.Instance) (used, total int64) {
	// Get monthly transfer allowance from bundle
	if inst.Networking != nil && inst.Networking.MonthlyTransfer != nil && inst.Networking.MonthlyTransfer.GbPerMonthAllocated != nil {
		total = int64(*inst.Networking.MonthlyTransfer.GbPerMonthAllocated) * 1024 * 1024 * 1024
	}

	// Calculate start of current month
	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	// Get NetworkOut metrics
	metrics, err := p.client.GetInstanceMetricData(ctx, &lightsail.GetInstanceMetricDataInput{
		InstanceName: aws.String(instanceID),
		MetricName:   types.InstanceMetricNameNetworkOut,
		StartTime:    aws.Time(monthStart),
		EndTime:      aws.Time(now),
		Period:       aws.Int32(86400), // Daily
		Unit:         types.MetricUnitBytes,
		Statistics:   []types.MetricStatistic{types.MetricStatisticSum},
	})
	if err != nil {
		log.Warnf(ctx, "[AWS] Failed to get metrics: %v", err)
		return 0, total
	}

	for _, dp := range metrics.MetricData {
		if dp.Sum != nil {
			used += int64(*dp.Sum)
		}
	}

	return used, total
}

func (p *AWSLightsailProvider) ListInstances(ctx context.Context) ([]*InstanceStatus, error) {
	result, err := p.client.GetInstances(ctx, &lightsail.GetInstancesInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to list instances: %w", err)
	}

	statuses := make([]*InstanceStatus, 0, len(result.Instances))
	for _, inst := range result.Instances {
		if inst.Name == nil {
			continue
		}
		status, err := p.GetInstanceStatus(ctx, *inst.Name)
		if err != nil {
			log.Warnf(ctx, "[AWS] Failed to get status for %s: %v", *inst.Name, err)
			continue
		}
		statuses = append(statuses, status)
	}

	return statuses, nil
}

func (p *AWSLightsailProvider) ChangeIP(ctx context.Context, instanceID string, opts ChangeIPOptions) (*OperationResult, error) {
	log.Infof(ctx, "[AWS] Starting IP change: instance=%s", instanceID)

	// Step 1: Get current static IP (if any)
	instance, err := p.client.GetInstance(ctx, &lightsail.GetInstanceInput{
		InstanceName: aws.String(instanceID),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get instance: %w", err)
	}

	// Step 2: Detach and release current static IP if exists
	if instance.Instance != nil && instance.Instance.IsStaticIp != nil && *instance.Instance.IsStaticIp {
		staticIPs, err := p.client.GetStaticIps(ctx, &lightsail.GetStaticIpsInput{})
		if err != nil {
			log.Warnf(ctx, "[AWS] Failed to get static IPs: %v", err)
		} else if staticIPs != nil {
			for _, ip := range staticIPs.StaticIps {
				if ip.AttachedTo != nil && *ip.AttachedTo == instanceID && ip.Name != nil {
					log.Infof(ctx, "[AWS] Detaching static IP: %s", *ip.Name)

					_, detachErr := p.client.DetachStaticIp(ctx, &lightsail.DetachStaticIpInput{
						StaticIpName: ip.Name,
					})
					if detachErr != nil {
						return nil, fmt.Errorf("failed to detach static IP: %w", detachErr)
					}

					_, releaseErr := p.client.ReleaseStaticIp(ctx, &lightsail.ReleaseStaticIpInput{
						StaticIpName: ip.Name,
					})
					if releaseErr != nil {
						log.Warnf(ctx, "[AWS] Failed to release static IP: %v", releaseErr)
					}
					break
				}
			}
		}
	}

	// Step 3: Allocate new static IP
	newIPName := fmt.Sprintf("%s-ip-%d", instanceID, time.Now().Unix())
	log.Infof(ctx, "[AWS] Allocating new static IP: %s", newIPName)

	_, err = p.client.AllocateStaticIp(ctx, &lightsail.AllocateStaticIpInput{
		StaticIpName: aws.String(newIPName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to allocate static IP: %w", err)
	}

	// Step 4: Attach new static IP
	_, err = p.client.AttachStaticIp(ctx, &lightsail.AttachStaticIpInput{
		StaticIpName: aws.String(newIPName),
		InstanceName: aws.String(instanceID),
	})
	if err != nil {
		// Try to clean up allocated IP
		_, cleanupErr := p.client.ReleaseStaticIp(ctx, &lightsail.ReleaseStaticIpInput{
			StaticIpName: aws.String(newIPName),
		})
		if cleanupErr != nil {
			log.Warnf(ctx, "[AWS] Failed to clean up allocated IP %s: %v", newIPName, cleanupErr)
		}
		return nil, fmt.Errorf("failed to attach static IP: %w", err)
	}

	// Get the new IP address
	staticIP, getIPErr := p.client.GetStaticIp(ctx, &lightsail.GetStaticIpInput{
		StaticIpName: aws.String(newIPName),
	})
	newIP := ""
	if getIPErr != nil {
		log.Warnf(ctx, "[AWS] Failed to retrieve new static IP: %v", getIPErr)
	} else if staticIP != nil && staticIP.StaticIp != nil && staticIP.StaticIp.IpAddress != nil {
		newIP = *staticIP.StaticIp.IpAddress
	}

	log.Infof(ctx, "[AWS] IP change completed: new_ip=%s", newIP)

	return &OperationResult{
		Success: true,
		Message: "IP changed successfully",
		Data: map[string]any{
			"new_ip":       newIP,
			"static_ip_id": newIPName,
		},
	}, nil
}

func (p *AWSLightsailProvider) CreateInstance(ctx context.Context, opts CreateInstanceOptions) (*OperationResult, error) {
	log.Infof(ctx, "[AWS] Creating instance: name=%s, region=%s, plan=%s", opts.Name, opts.Region, opts.Plan)

	result, err := p.client.CreateInstances(ctx, &lightsail.CreateInstancesInput{
		InstanceNames:    []string{opts.Name},
		AvailabilityZone: aws.String(opts.Region),
		BlueprintId:      aws.String(opts.ImageID),
		BundleId:         aws.String(opts.Plan),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create instance: %w", err)
	}

	if len(result.Operations) == 0 {
		return nil, fmt.Errorf("no operations returned")
	}

	log.Infof(ctx, "[AWS] Instance creation initiated: %s", opts.Name)

	return &OperationResult{
		Success: true,
		Message: "Instance creation initiated",
		Data: map[string]any{
			"instance_name": opts.Name,
		},
	}, nil
}

func (p *AWSLightsailProvider) DeleteInstance(ctx context.Context, instanceID string) (*OperationResult, error) {
	log.Infof(ctx, "[AWS] Deleting instance: %s", instanceID)

	_, err := p.client.DeleteInstance(ctx, &lightsail.DeleteInstanceInput{
		InstanceName: aws.String(instanceID),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to delete instance: %w", err)
	}

	log.Infof(ctx, "[AWS] Instance deleted: %s", instanceID)

	return &OperationResult{
		Success: true,
		Message: "Instance deleted successfully",
	}, nil
}

func (p *AWSLightsailProvider) ListRegions(ctx context.Context) ([]RegionInfo, error) {
	result, err := p.client.GetRegions(ctx, &lightsail.GetRegionsInput{
		IncludeAvailabilityZones: aws.Bool(false),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get regions: %w", err)
	}

	var regions []RegionInfo
	for _, r := range result.Regions {
		awsRegion := string(r.Name)
		if awsRegion == "" {
			continue
		}

		// Map to unified region info
		unifiedRegion := GetRegionByProviderID(ProviderAWSLightsail, awsRegion)
		if unifiedRegion != nil {
			regions = append(regions, RegionInfo{
				Slug:       unifiedRegion.Slug,
				NameEN:     unifiedRegion.NameEN,
				NameZH:     unifiedRegion.NameZH,
				Country:    unifiedRegion.Country,
				ProviderID: awsRegion,
				Available:  true,
			})
		} else {
			// Unknown region, use AWS display name
			displayName := awsRegion
			if r.DisplayName != nil {
				displayName = *r.DisplayName
			}
			regions = append(regions, RegionInfo{
				Slug:       awsRegion,
				NameEN:     displayName,
				NameZH:     displayName,
				Country:    "",
				ProviderID: awsRegion,
				Available:  true,
			})
		}
	}

	return regions, nil
}

func (p *AWSLightsailProvider) ListPlans(ctx context.Context, region string) ([]PlanInfo, error) {
	result, err := p.client.GetBundles(ctx, &lightsail.GetBundlesInput{
		IncludeInactive: aws.Bool(false),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get bundles: %w", err)
	}

	var plans []PlanInfo
	for _, b := range result.Bundles {
		if b.BundleId == nil {
			continue
		}

		// Filter by platform (Linux only for now)
		if b.SupportedPlatforms != nil {
			hasLinux := false
			for _, platform := range b.SupportedPlatforms {
				if platform == types.InstancePlatformLinuxUnix {
					hasLinux = true
					break
				}
			}
			if !hasLinux {
				continue
			}
		}

		plan := PlanInfo{
			ID: *b.BundleId,
		}

		if b.Name != nil {
			plan.Name = *b.Name
		}
		if b.CpuCount != nil {
			plan.CPU = int(*b.CpuCount)
		}
		if b.RamSizeInGb != nil {
			plan.MemoryMB = int(*b.RamSizeInGb * 1024)
		}
		if b.DiskSizeInGb != nil {
			plan.StorageGB = int(*b.DiskSizeInGb)
		}
		if b.TransferPerMonthInGb != nil {
			plan.TransferTB = float64(*b.TransferPerMonthInGb) / 1024
		}
		if b.Price != nil {
			plan.PriceMonthly = float64(*b.Price)
		}

		plans = append(plans, plan)
	}

	return plans, nil
}

func (p *AWSLightsailProvider) ListImages(ctx context.Context, region string) ([]ImageInfo, error) {
	result, err := p.client.GetBlueprints(ctx, &lightsail.GetBlueprintsInput{
		IncludeInactive: aws.Bool(false),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get blueprints: %w", err)
	}

	var images []ImageInfo
	for _, b := range result.Blueprints {
		if b.BlueprintId == nil {
			continue
		}

		// Filter OS blueprints (skip app blueprints)
		if b.Type != types.BlueprintTypeOs {
			continue
		}

		image := ImageInfo{
			ID: *b.BlueprintId,
		}

		if b.Name != nil {
			image.Name = *b.Name
		}
		if b.Platform != "" {
			if b.Platform == types.InstancePlatformLinuxUnix {
				image.OS = "linux"
			} else {
				image.OS = "windows"
			}
		}
		if b.Group != nil {
			image.Platform = *b.Group
		}
		if b.Description != nil {
			image.Description = *b.Description
		}

		images = append(images, image)
	}

	return images, nil
}

// MultiRegionAWSLightsailProvider manages AWS Lightsail instances across all regions
type MultiRegionAWSLightsailProvider struct {
	accessKeyID     string
	secretAccessKey string
	providers       map[string]*AWSLightsailProvider // region -> provider
}

// NewMultiRegionAWSLightsailProvider creates a provider that manages instances across all AWS regions
func NewMultiRegionAWSLightsailProvider(accessKeyID, secretAccessKey string) (*MultiRegionAWSLightsailProvider, error) {
	mp := &MultiRegionAWSLightsailProvider{
		accessKeyID:     accessKeyID,
		secretAccessKey: secretAccessKey,
		providers:       make(map[string]*AWSLightsailProvider),
	}

	// Create a provider for each known AWS Lightsail region
	for _, awsRegion := range GetAWSLightsailRegions() {
		p, err := NewAWSLightsailProvider(accessKeyID, secretAccessKey, awsRegion)
		if err != nil {
			log.Warnf(context.Background(), "[AWS] Failed to create provider for region %s: %v", awsRegion, err)
			continue
		}
		mp.providers[awsRegion] = p
	}

	if len(mp.providers) == 0 {
		return nil, fmt.Errorf("failed to create any regional providers")
	}

	return mp, nil
}

func (mp *MultiRegionAWSLightsailProvider) Name() string {
	return ProviderAWSLightsail
}

func (mp *MultiRegionAWSLightsailProvider) getProviderForRegion(region string) *AWSLightsailProvider {
	// Try direct match
	if p, ok := mp.providers[region]; ok {
		return p
	}

	// Try to find by unified slug
	awsRegion := GetProviderRegion(region, ProviderAWSLightsail)
	if awsRegion != "" {
		if p, ok := mp.providers[awsRegion]; ok {
			return p
		}
	}

	return nil
}

func (mp *MultiRegionAWSLightsailProvider) GetInstanceStatus(ctx context.Context, instanceID string) (*InstanceStatus, error) {
	// Need to search across all regions
	for _, p := range mp.providers {
		status, err := p.GetInstanceStatus(ctx, instanceID)
		if err == nil {
			return status, nil
		}
	}
	return nil, fmt.Errorf("instance not found: %s", instanceID)
}

func (mp *MultiRegionAWSLightsailProvider) ListInstances(ctx context.Context) ([]*InstanceStatus, error) {
	var allStatuses []*InstanceStatus

	for region, p := range mp.providers {
		statuses, err := p.ListInstances(ctx)
		if err != nil {
			log.Warnf(ctx, "[AWS] Failed to list instances in region %s: %v", region, err)
			continue
		}
		allStatuses = append(allStatuses, statuses...)
	}

	return allStatuses, nil
}

func (mp *MultiRegionAWSLightsailProvider) ChangeIP(ctx context.Context, instanceID string, opts ChangeIPOptions) (*OperationResult, error) {
	// Find the instance first
	for _, p := range mp.providers {
		_, err := p.GetInstanceStatus(ctx, instanceID)
		if err == nil {
			return p.ChangeIP(ctx, instanceID, opts)
		}
	}
	return nil, fmt.Errorf("instance not found: %s", instanceID)
}

func (mp *MultiRegionAWSLightsailProvider) CreateInstance(ctx context.Context, opts CreateInstanceOptions) (*OperationResult, error) {
	p := mp.getProviderForRegion(opts.Region)
	if p == nil {
		return nil, fmt.Errorf("unknown region: %s", opts.Region)
	}
	return p.CreateInstance(ctx, opts)
}

func (mp *MultiRegionAWSLightsailProvider) DeleteInstance(ctx context.Context, instanceID string) (*OperationResult, error) {
	// Find the instance first
	for _, p := range mp.providers {
		_, err := p.GetInstanceStatus(ctx, instanceID)
		if err == nil {
			return p.DeleteInstance(ctx, instanceID)
		}
	}
	return nil, fmt.Errorf("instance not found: %s", instanceID)
}

func (mp *MultiRegionAWSLightsailProvider) ListRegions(ctx context.Context) ([]RegionInfo, error) {
	// Use any provider to get regions (they all return the same list)
	for _, p := range mp.providers {
		return p.ListRegions(ctx)
	}
	return nil, fmt.Errorf("no regional providers available")
}

func (mp *MultiRegionAWSLightsailProvider) ListPlans(ctx context.Context, region string) ([]PlanInfo, error) {
	p := mp.getProviderForRegion(region)
	if p == nil {
		// Use any provider if region not specified (plans are global)
		for _, p := range mp.providers {
			return p.ListPlans(ctx, region)
		}
		return nil, fmt.Errorf("no regional providers available")
	}
	return p.ListPlans(ctx, region)
}

func (mp *MultiRegionAWSLightsailProvider) ListImages(ctx context.Context, region string) ([]ImageInfo, error) {
	p := mp.getProviderForRegion(region)
	if p == nil {
		// Use any provider if region not specified (images are mostly global)
		for _, p := range mp.providers {
			return p.ListImages(ctx, region)
		}
		return nil, fmt.Errorf("no regional providers available")
	}
	return p.ListImages(ctx, region)
}
