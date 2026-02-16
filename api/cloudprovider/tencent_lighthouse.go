package cloudprovider

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	lighthouse "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/lighthouse/v20200324"
	"github.com/wordgate/qtoolkit/log"
)

// TencentLighthouseProvider implements Provider for Tencent Cloud Lighthouse (International)
type TencentLighthouseProvider struct {
	secretId     string
	secretKey    string
	region       string
	client       *lighthouse.Client
	providerName string // ProviderTencentLighthouse or ProviderQCloudLighthouse
}

// NewTencentLighthouseProvider creates a new Tencent Lighthouse provider for international regions
func NewTencentLighthouseProvider(secretId, secretKey, region string) (*TencentLighthouseProvider, error) {
	return newTencentProvider(secretId, secretKey, region, ProviderTencentLighthouse)
}

// NewQCloudLighthouseProvider creates a new QCloud Lighthouse provider for domestic regions
func NewQCloudLighthouseProvider(secretId, secretKey, region string) (*TencentLighthouseProvider, error) {
	return newTencentProvider(secretId, secretKey, region, ProviderQCloudLighthouse)
}

func newTencentProvider(secretId, secretKey, region, providerName string) (*TencentLighthouseProvider, error) {
	credential := common.NewCredential(secretId, secretKey)
	cpf := profile.NewClientProfile()
	cpf.HttpProfile.Endpoint = "lighthouse.tencentcloudapi.com"

	client, err := lighthouse.NewClient(credential, region, cpf)
	if err != nil {
		return nil, fmt.Errorf("failed to create lighthouse client: %w", err)
	}

	return &TencentLighthouseProvider{
		secretId:     secretId,
		secretKey:    secretKey,
		region:       region,
		client:       client,
		providerName: providerName,
	}, nil
}

func (p *TencentLighthouseProvider) Name() string {
	return p.providerName
}

func (p *TencentLighthouseProvider) GetInstanceStatus(ctx context.Context, instanceID string) (*InstanceStatus, error) {
	request := lighthouse.NewDescribeInstancesRequest()
	request.InstanceIds = []*string{&instanceID}

	response, err := p.client.DescribeInstances(request)
	if err != nil {
		return nil, fmt.Errorf("failed to describe instances: %w", err)
	}

	if len(response.Response.InstanceSet) == 0 {
		return nil, fmt.Errorf("instance not found: %s", instanceID)
	}

	inst := response.Response.InstanceSet[0]

	// Get traffic package info
	trafficUsed, trafficTotal, trafficResetAt := p.getTrafficPackage(ctx, instanceID)

	// Parse expiry time
	var expiresAt time.Time
	if inst.ExpiredTime != nil {
		expiresAt, _ = time.Parse("2006-01-02T15:04:05Z", *inst.ExpiredTime)
	}

	status := &InstanceStatus{
		InstanceID:        *inst.InstanceId,
		Name:              stringValue(inst.InstanceName),
		IPAddress:         getFirstIP(inst.PublicAddresses),
		IPv6Address:       getFirstIP(inst.PublicIpv6Addresses),
		Region:            p.region,
		TrafficUsedBytes:  trafficUsed,
		TrafficTotalBytes: trafficTotal,
		TrafficResetAt:    trafficResetAt,
		ExpiresAt:         expiresAt,
		State:             strings.ToLower(stringValue(inst.InstanceState)),
	}

	return status, nil
}

func (p *TencentLighthouseProvider) getTrafficPackage(ctx context.Context, instanceID string) (used, total int64, resetAt time.Time) {
	request := lighthouse.NewDescribeInstancesTrafficPackagesRequest()
	request.InstanceIds = []*string{&instanceID}

	response, err := p.client.DescribeInstancesTrafficPackages(request)
	if err != nil {
		log.Warnf(ctx, "[TENCENT] Failed to get traffic packages: %v", err)
		return 0, 0, time.Time{}
	}

	for _, pkg := range response.Response.InstanceTrafficPackageSet {
		if pkg.InstanceId != nil && *pkg.InstanceId == instanceID {
			for _, traffic := range pkg.TrafficPackageSet {
				if traffic.TrafficUsed != nil {
					used += *traffic.TrafficUsed
				}
				if traffic.TrafficPackageTotal != nil {
					total += *traffic.TrafficPackageTotal
				}
				if traffic.Deadline != nil {
					resetAt, _ = time.Parse("2006-01-02T15:04:05Z", *traffic.Deadline)
				}
			}
			break
		}
	}

	return used, total, resetAt
}

func (p *TencentLighthouseProvider) ListInstances(ctx context.Context) ([]*InstanceStatus, error) {
	log.Infof(ctx, "[TENCENT] ListInstances: region=%s", p.region)

	request := lighthouse.NewDescribeInstancesRequest()
	limit := int64(100)
	request.Limit = &limit

	response, err := p.client.DescribeInstances(request)
	if err != nil {
		return nil, fmt.Errorf("failed to list instances: %w", err)
	}

	var statuses []*InstanceStatus
	for _, inst := range response.Response.InstanceSet {
		status, err := p.GetInstanceStatus(ctx, *inst.InstanceId)
		if err != nil {
			log.Warnf(ctx, "[TENCENT] Failed to get status for %s: %v", *inst.InstanceId, err)
			continue
		}
		statuses = append(statuses, status)
	}

	return statuses, nil
}

func (p *TencentLighthouseProvider) ChangeIP(ctx context.Context, instanceID string, opts ChangeIPOptions) (*OperationResult, error) {
	return nil, &NotSupportedError{Provider: p.providerName, Operation: "ChangeIP"}
}

func (p *TencentLighthouseProvider) CreateInstance(ctx context.Context, opts CreateInstanceOptions) (*OperationResult, error) {
	log.Infof(ctx, "[TENCENT] Creating instance: region=%s, plan=%s, image=%s, name=%s",
		opts.Region, opts.Plan, opts.ImageID, opts.Name)

	request := lighthouse.NewCreateInstancesRequest()
	request.BundleId = &opts.Plan
	request.BlueprintId = &opts.ImageID
	request.InstanceName = &opts.Name

	count := uint64(1)
	request.InstanceCount = &count

	period := int64(1)
	request.InstanceChargePrepaid = &lighthouse.InstanceChargePrepaid{
		Period: &period,
	}

	response, err := p.client.CreateInstances(request)
	if err != nil {
		return nil, fmt.Errorf("failed to create instance: %w", err)
	}

	if len(response.Response.InstanceIdSet) == 0 {
		return nil, fmt.Errorf("no instance created")
	}

	instanceId := *response.Response.InstanceIdSet[0]
	log.Infof(ctx, "[TENCENT] Instance created: %s", instanceId)

	return &OperationResult{
		Success: true,
		Message: "Instance created successfully",
		Data: map[string]any{
			"instance_id": instanceId,
		},
	}, nil
}

func (p *TencentLighthouseProvider) DeleteInstance(ctx context.Context, instanceID string) (*OperationResult, error) {
	log.Infof(ctx, "[TENCENT] Deleting instance: %s", instanceID)

	request := lighthouse.NewTerminateInstancesRequest()
	request.InstanceIds = []*string{&instanceID}

	_, err := p.client.TerminateInstances(request)
	if err != nil {
		return nil, fmt.Errorf("failed to terminate instance: %w", err)
	}

	log.Infof(ctx, "[TENCENT] Instance deleted: %s", instanceID)

	return &OperationResult{
		Success: true,
		Message: "Instance deleted successfully",
	}, nil
}

func (p *TencentLighthouseProvider) ListRegions(ctx context.Context) ([]RegionInfo, error) {
	request := lighthouse.NewDescribeRegionsRequest()

	response, err := p.client.DescribeRegions(request)
	if err != nil {
		return nil, fmt.Errorf("failed to list regions: %w", err)
	}

	var regions []RegionInfo
	for _, r := range response.Response.RegionSet {
		regionId := stringValue(r.Region)
		unifiedRegion := GetRegionByProviderID(p.providerName, regionId)

		if unifiedRegion != nil {
			regions = append(regions, RegionInfo{
				Slug:       unifiedRegion.Slug,
				NameEN:     unifiedRegion.NameEN,
				NameZH:     unifiedRegion.NameZH,
				Country:    unifiedRegion.Country,
				ProviderID: regionId,
				Available:  true,
			})
		} else {
			regions = append(regions, RegionInfo{
				Slug:       regionId,
				NameEN:     stringValue(r.RegionName),
				NameZH:     stringValue(r.RegionName),
				Country:    "",
				ProviderID: regionId,
				Available:  true,
			})
		}
	}

	return regions, nil
}

func (p *TencentLighthouseProvider) ListPlans(ctx context.Context, region string) ([]PlanInfo, error) {
	request := lighthouse.NewDescribeBundlesRequest()

	response, err := p.client.DescribeBundles(request)
	if err != nil {
		return nil, fmt.Errorf("failed to list bundles: %w", err)
	}

	var plans []PlanInfo
	for _, bundle := range response.Response.BundleSet {
		if bundle.SupportLinuxUnixPlatform == nil || !*bundle.SupportLinuxUnixPlatform {
			continue
		}

		var transferGB int64
		if bundle.MonthlyTraffic != nil {
			transferGB = *bundle.MonthlyTraffic
		} else if bundle.InternetMaxBandwidthOut != nil {
			transferGB = int64(*bundle.InternetMaxBandwidthOut) * 1024 // Estimate based on bandwidth
		}

		plans = append(plans, PlanInfo{
			ID:           stringValue(bundle.BundleId),
			Name:         stringValue(bundle.BundleDisplayLabel),
			CPU:          int(int64Value(bundle.CPU)),
			MemoryMB:     int(int64Value(bundle.Memory)) * 1024,
			StorageGB:    int(int64Value(bundle.SystemDiskSize)),
			TransferTB:   float64(transferGB) / 1024,
			PriceMonthly: float64Value(bundle.Price.InstancePrice.OriginalBundlePrice),
		})
	}

	return plans, nil
}

func (p *TencentLighthouseProvider) ListImages(ctx context.Context, region string) ([]ImageInfo, error) {
	request := lighthouse.NewDescribeBlueprintsRequest()

	// Only get public system images
	blueprintType := "PURE_OS"
	request.Filters = []*lighthouse.Filter{
		{
			Name:   strPtr("blueprint-type"),
			Values: []*string{&blueprintType},
		},
	}

	response, err := p.client.DescribeBlueprints(request)
	if err != nil {
		return nil, fmt.Errorf("failed to list blueprints: %w", err)
	}

	var images []ImageInfo
	for _, bp := range response.Response.BlueprintSet {
		osType := "linux"
		if bp.OsName != nil && strings.Contains(strings.ToLower(*bp.OsName), "windows") {
			osType = "windows"
		}

		images = append(images, ImageInfo{
			ID:          stringValue(bp.BlueprintId),
			Name:        stringValue(bp.DisplayTitle),
			OS:          osType,
			Platform:    stringValue(bp.Platform),
			Description: stringValue(bp.Description),
		})
	}

	return images, nil
}

// Helper functions
func stringValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func int64Value(i *int64) int64 {
	if i == nil {
		return 0
	}
	return *i
}

func float64Value(f *float64) float64 {
	if f == nil {
		return 0
	}
	return *f
}

func strPtr(s string) *string {
	return &s
}

func getFirstIP(ips []*string) string {
	if len(ips) > 0 && ips[0] != nil {
		return *ips[0]
	}
	return ""
}

// MultiRegionTencentLighthouseProvider manages Tencent Lighthouse instances across all regions
type MultiRegionTencentLighthouseProvider struct {
	secretId     string
	secretKey    string
	providers    map[string]*TencentLighthouseProvider
	providerName string
}

// NewMultiRegionTencentLighthouseProvider creates a provider for all Tencent international regions
func NewMultiRegionTencentLighthouseProvider(secretId, secretKey string) (*MultiRegionTencentLighthouseProvider, error) {
	return newMultiRegionTencentProvider(secretId, secretKey, ProviderTencentLighthouse, GetTencentLighthouseRegions())
}

// NewMultiRegionQCloudLighthouseProvider creates a provider for all QCloud domestic regions
func NewMultiRegionQCloudLighthouseProvider(secretId, secretKey string) (*MultiRegionTencentLighthouseProvider, error) {
	return newMultiRegionTencentProvider(secretId, secretKey, ProviderQCloudLighthouse, GetQCloudLighthouseRegions())
}

func newMultiRegionTencentProvider(secretId, secretKey, providerName string, regions []string) (*MultiRegionTencentLighthouseProvider, error) {
	mp := &MultiRegionTencentLighthouseProvider{
		secretId:     secretId,
		secretKey:    secretKey,
		providers:    make(map[string]*TencentLighthouseProvider),
		providerName: providerName,
	}

	for _, region := range regions {
		p, err := newTencentProvider(secretId, secretKey, region, providerName)
		if err != nil {
			continue
		}
		mp.providers[region] = p
	}

	if len(mp.providers) == 0 {
		return nil, fmt.Errorf("failed to create any regional providers")
	}

	return mp, nil
}

func (mp *MultiRegionTencentLighthouseProvider) Name() string {
	return mp.providerName
}

func (mp *MultiRegionTencentLighthouseProvider) getProviderForRegion(region string) *TencentLighthouseProvider {
	if p, ok := mp.providers[region]; ok {
		return p
	}

	providerRegion := GetProviderRegion(region, mp.providerName)
	if providerRegion != "" {
		if p, ok := mp.providers[providerRegion]; ok {
			return p
		}
	}

	return nil
}

func (mp *MultiRegionTencentLighthouseProvider) GetInstanceStatus(ctx context.Context, instanceID string) (*InstanceStatus, error) {
	for _, p := range mp.providers {
		status, err := p.GetInstanceStatus(ctx, instanceID)
		if err == nil {
			return status, nil
		}
	}
	return nil, fmt.Errorf("instance not found: %s", instanceID)
}

func (mp *MultiRegionTencentLighthouseProvider) ListInstances(ctx context.Context) ([]*InstanceStatus, error) {
	var allStatuses []*InstanceStatus

	for region, p := range mp.providers {
		statuses, err := p.ListInstances(ctx)
		if err != nil {
			log.Warnf(ctx, "[TENCENT] Failed to list instances in region %s: %v", region, err)
			continue
		}
		allStatuses = append(allStatuses, statuses...)
	}

	return allStatuses, nil
}

func (mp *MultiRegionTencentLighthouseProvider) ChangeIP(ctx context.Context, instanceID string, opts ChangeIPOptions) (*OperationResult, error) {
	return nil, &NotSupportedError{Provider: mp.providerName, Operation: "ChangeIP"}
}

func (mp *MultiRegionTencentLighthouseProvider) CreateInstance(ctx context.Context, opts CreateInstanceOptions) (*OperationResult, error) {
	p := mp.getProviderForRegion(opts.Region)
	if p == nil {
		return nil, fmt.Errorf("unknown region: %s", opts.Region)
	}
	return p.CreateInstance(ctx, opts)
}

func (mp *MultiRegionTencentLighthouseProvider) DeleteInstance(ctx context.Context, instanceID string) (*OperationResult, error) {
	for _, p := range mp.providers {
		_, err := p.GetInstanceStatus(context.Background(), instanceID)
		if err == nil {
			return p.DeleteInstance(ctx, instanceID)
		}
	}
	return nil, fmt.Errorf("instance not found: %s", instanceID)
}

func (mp *MultiRegionTencentLighthouseProvider) ListRegions(ctx context.Context) ([]RegionInfo, error) {
	for _, p := range mp.providers {
		return p.ListRegions(ctx)
	}
	return nil, fmt.Errorf("no regional providers available")
}

func (mp *MultiRegionTencentLighthouseProvider) ListPlans(ctx context.Context, region string) ([]PlanInfo, error) {
	p := mp.getProviderForRegion(region)
	if p == nil {
		for _, p := range mp.providers {
			return p.ListPlans(ctx, region)
		}
		return nil, fmt.Errorf("no regional providers available")
	}
	return p.ListPlans(ctx, region)
}

func (mp *MultiRegionTencentLighthouseProvider) ListImages(ctx context.Context, region string) ([]ImageInfo, error) {
	p := mp.getProviderForRegion(region)
	if p == nil {
		for _, p := range mp.providers {
			return p.ListImages(ctx, region)
		}
		return nil, fmt.Errorf("no regional providers available")
	}
	return p.ListImages(ctx, region)
}
