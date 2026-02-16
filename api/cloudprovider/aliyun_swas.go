package cloudprovider

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/wordgate/qtoolkit/log"
)

// AliyunSWASProvider implements Provider for Alibaba Cloud Simple Application Server
type AliyunSWASProvider struct {
	accessKeyID     string
	accessKeySecret string
	region          string
	client          *http.Client
}

// NewAliyunSWASProvider creates a new Aliyun SWAS provider
func NewAliyunSWASProvider(accessKeyID, accessKeySecret, region string) *AliyunSWASProvider {
	return &AliyunSWASProvider{
		accessKeyID:     accessKeyID,
		accessKeySecret: accessKeySecret,
		region:          region,
		client:          &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *AliyunSWASProvider) Name() string {
	return ProviderAliyunSWAS
}

func (p *AliyunSWASProvider) GetInstanceStatus(ctx context.Context, instanceID string) (*InstanceStatus, error) {
	// Get instance basic info
	instanceInfo, err := p.listInstances(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	if len(instanceInfo) == 0 {
		return nil, fmt.Errorf("instance not found: %s", instanceID)
	}

	inst := instanceInfo[0]

	// Get traffic package info
	trafficUsed, trafficTotal, err := p.getTrafficPackage(ctx, instanceID)
	if err != nil {
		log.Warnf(ctx, "[ALIYUN] Failed to get traffic info: %v", err)
	}

	return &InstanceStatus{
		InstanceID:        inst.InstanceID,
		Name:              inst.InstanceName,
		IPAddress:         inst.PublicIPAddress,
		IPv6Address:       inst.Ipv6Address,
		Region:            p.region,
		TrafficUsedBytes:  trafficUsed,
		TrafficTotalBytes: trafficTotal,
		TrafficResetAt:    time.Time{}, // SWAS uses expiry-based billing
		ExpiresAt:         inst.ExpiredTime,
		State:             inst.Status,
	}, nil
}

type aliyunInstance struct {
	InstanceID      string
	InstanceName    string
	PublicIPAddress string
	Ipv6Address     string
	Status          string
	ExpiredTime     time.Time
}

func (p *AliyunSWASProvider) listInstances(ctx context.Context, instanceID string) ([]aliyunInstance, error) {
	log.Infof(ctx, "[ALIYUN] ListInstances: region=%s, instanceID=%s", p.region, instanceID)

	params := map[string]string{
		"Action":     "ListInstances",
		"RegionId":   p.region,
		"PageSize":   "100",
		"PageNumber": "1",
	}
	if instanceID != "" {
		params["InstanceIds"] = fmt.Sprintf(`["%s"]`, instanceID)
	}

	resp, err := p.doRequest(ctx, params)
	if err != nil {
		log.Errorf(ctx, "[ALIYUN] ListInstances failed: %v", err)
		return nil, err
	}

	var result struct {
		Instances []struct {
			InstanceId      string `json:"InstanceId"`
			InstanceName    string `json:"InstanceName"`
			PublicIpAddress string `json:"PublicIpAddress"`
			Ipv6Address     string `json:"Ipv6Address"`
			Status          string `json:"Status"`
			ExpiredTime     string `json:"ExpiredTime"`
		} `json:"Instances"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	instances := make([]aliyunInstance, len(result.Instances))
	for i, inst := range result.Instances {
		expiredTime, _ := time.Parse("2006-01-02T15:04:05Z", inst.ExpiredTime)
		instances[i] = aliyunInstance{
			InstanceID:      inst.InstanceId,
			InstanceName:    inst.InstanceName,
			PublicIPAddress: inst.PublicIpAddress,
			Ipv6Address:     inst.Ipv6Address,
			Status:          strings.ToLower(inst.Status),
			ExpiredTime:     expiredTime,
		}
	}

	return instances, nil
}

func (p *AliyunSWASProvider) getTrafficPackage(ctx context.Context, instanceID string) (used, total int64, err error) {
	params := map[string]string{
		"Action":      "ListInstancesTrafficPackages",
		"RegionId":    p.region,
		"InstanceIds": fmt.Sprintf(`["%s"]`, instanceID),
	}

	resp, err := p.doRequest(ctx, params)
	if err != nil {
		return 0, 0, err
	}

	var result struct {
		InstanceTrafficPackageUsages []struct {
			InstanceId          string `json:"InstanceId"`
			TrafficUsed         int64  `json:"TrafficUsed"`
			TrafficPackageTotal int64  `json:"TrafficPackageTotal"`
		} `json:"InstanceTrafficPackageUsages"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return 0, 0, fmt.Errorf("failed to parse response: %w", err)
	}

	for _, pkg := range result.InstanceTrafficPackageUsages {
		if pkg.InstanceId == instanceID {
			return pkg.TrafficUsed, pkg.TrafficPackageTotal, nil
		}
	}

	return 0, 0, nil
}

func (p *AliyunSWASProvider) ListInstances(ctx context.Context) ([]*InstanceStatus, error) {
	instances, err := p.listInstances(ctx, "")
	if err != nil {
		return nil, err
	}

	statuses := make([]*InstanceStatus, 0, len(instances))
	for _, inst := range instances {
		status, err := p.GetInstanceStatus(ctx, inst.InstanceID)
		if err != nil {
			log.Warnf(ctx, "[ALIYUN] Failed to get status for %s: %v", inst.InstanceID, err)
			continue
		}
		statuses = append(statuses, status)
	}

	return statuses, nil
}

func (p *AliyunSWASProvider) ChangeIP(ctx context.Context, instanceID string, opts ChangeIPOptions) (*OperationResult, error) {
	// SWAS doesn't support direct IP change
	return nil, &NotSupportedError{Provider: ProviderAliyunSWAS, Operation: "ChangeIP"}
}

func (p *AliyunSWASProvider) CreateInstance(ctx context.Context, opts CreateInstanceOptions) (*OperationResult, error) {
	log.Infof(ctx, "[ALIYUN] Creating instance: region=%s, plan=%s, image=%s", opts.Region, opts.Plan, opts.ImageID)

	params := map[string]string{
		"Action":    "CreateInstances",
		"RegionId":  p.region,
		"PlanId":    opts.Plan,
		"ImageId":   opts.ImageID,
		"Period":    "1", // 1 month
		"AutoRenew": "true",
	}

	resp, err := p.doRequest(ctx, params)
	if err != nil {
		return nil, err
	}

	var result struct {
		InstanceIds []string `json:"InstanceIds"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(result.InstanceIds) == 0 {
		return nil, fmt.Errorf("no instance created")
	}

	log.Infof(ctx, "[ALIYUN] Instance created: %s", result.InstanceIds[0])

	return &OperationResult{
		Success: true,
		Message: "Instance created successfully",
		Data: map[string]any{
			"instance_id": result.InstanceIds[0],
		},
	}, nil
}

func (p *AliyunSWASProvider) DeleteInstance(ctx context.Context, instanceID string) (*OperationResult, error) {
	log.Infof(ctx, "[ALIYUN] Deleting instance: %s", instanceID)

	params := map[string]string{
		"Action":      "DeleteInstances",
		"RegionId":    p.region,
		"InstanceIds": fmt.Sprintf(`["%s"]`, instanceID),
	}

	_, err := p.doRequest(ctx, params)
	if err != nil {
		return nil, err
	}

	log.Infof(ctx, "[ALIYUN] Instance deleted: %s", instanceID)

	return &OperationResult{
		Success: true,
		Message: "Instance deleted successfully",
	}, nil
}

// doRequest performs signed API request to Aliyun using the provider's configured region
func (p *AliyunSWASProvider) doRequest(ctx context.Context, params map[string]string) ([]byte, error) {
	return p.doRequestWithRegion(ctx, params, p.region)
}

// doRequestWithRegion performs signed API request to Aliyun using a specific region endpoint
func (p *AliyunSWASProvider) doRequestWithRegion(ctx context.Context, params map[string]string, region string) ([]byte, error) {
	// Add common parameters
	params["Format"] = "JSON"
	params["Version"] = "2020-06-01"
	params["AccessKeyId"] = p.accessKeyID
	params["SignatureMethod"] = "HMAC-SHA1"
	params["SignatureVersion"] = "1.0"
	params["SignatureNonce"] = uuid.New().String()
	params["Timestamp"] = time.Now().UTC().Format("2006-01-02T15:04:05Z")

	// Calculate signature
	signature := p.sign(params)
	params["Signature"] = signature

	// Build query string
	values := url.Values{}
	for k, v := range params {
		values.Set(k, v)
	}

	// Use the specified region for the endpoint
	endpoint := fmt.Sprintf("https://swas.%s.aliyuncs.com", region)
	reqURL := fmt.Sprintf("%s/?%s", endpoint, values.Encode())
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Check for API error
	var errResp struct {
		Code    string `json:"Code"`
		Message string `json:"Message"`
	}
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Code != "" {
		return nil, fmt.Errorf("aliyun API error: %s - %s", errResp.Code, errResp.Message)
	}

	return body, nil
}

func (p *AliyunSWASProvider) sign(params map[string]string) string {
	// Sort keys
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Build canonical query string
	var pairs []string
	for _, k := range keys {
		pairs = append(pairs, fmt.Sprintf("%s=%s",
			percentEncode(k),
			percentEncode(params[k])))
	}
	canonicalQuery := strings.Join(pairs, "&")

	// Build string to sign
	stringToSign := fmt.Sprintf("GET&%s&%s",
		percentEncode("/"),
		percentEncode(canonicalQuery))

	// Calculate HMAC-SHA1
	h := hmac.New(sha1.New, []byte(p.accessKeySecret+"&"))
	h.Write([]byte(stringToSign))
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))

	return signature
}

func percentEncode(s string) string {
	s = url.QueryEscape(s)
	s = strings.ReplaceAll(s, "+", "%20")
	s = strings.ReplaceAll(s, "*", "%2A")
	s = strings.ReplaceAll(s, "%7E", "~")
	return s
}

func (p *AliyunSWASProvider) ListRegions(ctx context.Context) ([]RegionInfo, error) {
	params := map[string]string{
		"Action": "ListRegions",
	}

	resp, err := p.doRequest(ctx, params)
	if err != nil {
		return nil, err
	}

	var result struct {
		Regions []struct {
			RegionId       string `json:"RegionId"`
			LocalName      string `json:"LocalName"`
			RegionEndpoint string `json:"RegionEndpoint"`
		} `json:"Regions"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	var regions []RegionInfo
	for _, r := range result.Regions {
		// Map to unified region info
		unifiedRegion := GetRegionByProviderID(ProviderAliyunSWAS, r.RegionId)
		if unifiedRegion != nil {
			regions = append(regions, RegionInfo{
				Slug:       unifiedRegion.Slug,
				NameEN:     unifiedRegion.NameEN,
				NameZH:     unifiedRegion.NameZH,
				Country:    unifiedRegion.Country,
				ProviderID: r.RegionId,
				Available:  true,
			})
		} else {
			// Unknown region, use Aliyun local name
			regions = append(regions, RegionInfo{
				Slug:       r.RegionId,
				NameEN:     r.LocalName,
				NameZH:     r.LocalName,
				Country:    "",
				ProviderID: r.RegionId,
				Available:  true,
			})
		}
	}

	return regions, nil
}

func (p *AliyunSWASProvider) ListPlans(ctx context.Context, region string) ([]PlanInfo, error) {
	targetRegion := region
	if targetRegion == "" {
		targetRegion = p.region
	}

	params := map[string]string{
		"Action":   "ListPlans",
		"RegionId": targetRegion,
	}

	// Use the target region's endpoint for region-specific API calls
	resp, err := p.doRequestWithRegion(ctx, params, targetRegion)
	if err != nil {
		return nil, err
	}

	var result struct {
		Plans []struct {
			PlanId      string  `json:"PlanId"`
			Core        int     `json:"Core"`
			Memory      int     `json:"Memory"` // GB
			Bandwidth   int     `json:"Bandwidth"`
			Flow        int     `json:"Flow"` // GB
			DiskSize    int     `json:"DiskSize"`
			OriginPrice float64 `json:"OriginPrice"`
			Currency    string  `json:"Currency"`
		} `json:"Plans"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	var plans []PlanInfo
	for _, pl := range result.Plans {
		plans = append(plans, PlanInfo{
			ID:           pl.PlanId,
			Name:         fmt.Sprintf("%d Core / %d GB RAM", pl.Core, pl.Memory),
			CPU:          pl.Core,
			MemoryMB:     pl.Memory * 1024,
			StorageGB:    pl.DiskSize,
			TransferTB:   float64(pl.Flow) / 1024,
			PriceMonthly: pl.OriginPrice,
		})
	}

	return plans, nil
}

func (p *AliyunSWASProvider) ListImages(ctx context.Context, region string) ([]ImageInfo, error) {
	targetRegion := region
	if targetRegion == "" {
		targetRegion = p.region
	}

	params := map[string]string{
		"Action":   "ListImages",
		"RegionId": targetRegion,
	}

	// Use the target region's endpoint for region-specific API calls
	resp, err := p.doRequestWithRegion(ctx, params, targetRegion)
	if err != nil {
		return nil, err
	}

	var result struct {
		Images []struct {
			ImageId     string `json:"ImageId"`
			ImageName   string `json:"ImageName"`
			ImageType   string `json:"ImageType"` // system, app, custom
			OsType      string `json:"OsType"`    // linux, windows
			Platform    string `json:"Platform"`
			Description string `json:"Description"`
		} `json:"Images"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	var images []ImageInfo
	for _, img := range result.Images {
		// Filter system images only
		if img.ImageType != "system" {
			continue
		}

		images = append(images, ImageInfo{
			ID:          img.ImageId,
			Name:        img.ImageName,
			OS:          strings.ToLower(img.OsType),
			Platform:    img.Platform,
			Description: img.Description,
		})
	}

	return images, nil
}

// MultiRegionAliyunSWASProvider manages Aliyun SWAS instances across all regions
type MultiRegionAliyunSWASProvider struct {
	accessKeyID     string
	accessKeySecret string
	providers       map[string]*AliyunSWASProvider // region -> provider
}

// NewMultiRegionAliyunSWASProvider creates a provider that manages instances across all Aliyun SWAS regions
func NewMultiRegionAliyunSWASProvider(accessKeyID, accessKeySecret string) (*MultiRegionAliyunSWASProvider, error) {
	mp := &MultiRegionAliyunSWASProvider{
		accessKeyID:     accessKeyID,
		accessKeySecret: accessKeySecret,
		providers:       make(map[string]*AliyunSWASProvider),
	}

	// Create a provider for each known Aliyun SWAS region
	for _, region := range GetAliyunSWASRegions() {
		p := NewAliyunSWASProvider(accessKeyID, accessKeySecret, region)
		mp.providers[region] = p
	}

	if len(mp.providers) == 0 {
		return nil, fmt.Errorf("failed to create any regional providers")
	}

	return mp, nil
}

func (mp *MultiRegionAliyunSWASProvider) Name() string {
	return ProviderAliyunSWAS
}

func (mp *MultiRegionAliyunSWASProvider) getProviderForRegion(region string) *AliyunSWASProvider {
	// Try direct match
	if p, ok := mp.providers[region]; ok {
		return p
	}

	// Try to find by unified slug
	aliyunRegion := GetProviderRegion(region, ProviderAliyunSWAS)
	if aliyunRegion != "" {
		if p, ok := mp.providers[aliyunRegion]; ok {
			return p
		}
	}

	return nil
}

func (mp *MultiRegionAliyunSWASProvider) GetInstanceStatus(ctx context.Context, instanceID string) (*InstanceStatus, error) {
	// Need to search across all regions
	for _, p := range mp.providers {
		status, err := p.GetInstanceStatus(ctx, instanceID)
		if err == nil {
			return status, nil
		}
	}
	return nil, fmt.Errorf("instance not found: %s", instanceID)
}

func (mp *MultiRegionAliyunSWASProvider) ListInstances(ctx context.Context) ([]*InstanceStatus, error) {
	var allStatuses []*InstanceStatus

	for region, p := range mp.providers {
		statuses, err := p.ListInstances(ctx)
		if err != nil {
			log.Warnf(ctx, "[ALIYUN] Failed to list instances in region %s: %v", region, err)
			continue
		}
		allStatuses = append(allStatuses, statuses...)
	}

	return allStatuses, nil
}

func (mp *MultiRegionAliyunSWASProvider) ChangeIP(ctx context.Context, instanceID string, opts ChangeIPOptions) (*OperationResult, error) {
	// Find the instance first
	for _, p := range mp.providers {
		_, err := p.GetInstanceStatus(ctx, instanceID)
		if err == nil {
			return p.ChangeIP(ctx, instanceID, opts)
		}
	}
	return nil, fmt.Errorf("instance not found: %s", instanceID)
}

func (mp *MultiRegionAliyunSWASProvider) CreateInstance(ctx context.Context, opts CreateInstanceOptions) (*OperationResult, error) {
	p := mp.getProviderForRegion(opts.Region)
	if p == nil {
		return nil, fmt.Errorf("unknown region: %s", opts.Region)
	}
	return p.CreateInstance(ctx, opts)
}

func (mp *MultiRegionAliyunSWASProvider) DeleteInstance(ctx context.Context, instanceID string) (*OperationResult, error) {
	// Find the instance first
	for _, p := range mp.providers {
		_, err := p.GetInstanceStatus(ctx, instanceID)
		if err == nil {
			return p.DeleteInstance(ctx, instanceID)
		}
	}
	return nil, fmt.Errorf("instance not found: %s", instanceID)
}

func (mp *MultiRegionAliyunSWASProvider) ListRegions(ctx context.Context) ([]RegionInfo, error) {
	// Use any provider to get regions (they all have access to the same API)
	for _, p := range mp.providers {
		return p.ListRegions(ctx)
	}
	return nil, fmt.Errorf("no regional providers available")
}

func (mp *MultiRegionAliyunSWASProvider) ListPlans(ctx context.Context, region string) ([]PlanInfo, error) {
	p := mp.getProviderForRegion(region)
	if p == nil {
		// Use any provider if region not specified (plans are region-specific)
		for _, p := range mp.providers {
			return p.ListPlans(ctx, region)
		}
		return nil, fmt.Errorf("no regional providers available")
	}
	return p.ListPlans(ctx, region)
}

func (mp *MultiRegionAliyunSWASProvider) ListImages(ctx context.Context, region string) ([]ImageInfo, error) {
	p := mp.getProviderForRegion(region)
	if p == nil {
		// Use any provider if region not specified (images are region-specific)
		for _, p := range mp.providers {
			return p.ListImages(ctx, region)
		}
		return nil, fmt.Errorf("no regional providers available")
	}
	return p.ListImages(ctx, region)
}
