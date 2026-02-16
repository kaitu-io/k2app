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

// AlibabaSWASProvider implements Provider for Alibaba Cloud Simple Application Server (International)
type AlibabaSWASProvider struct {
	accessKeyID     string
	accessKeySecret string
	region          string
	client          *http.Client
}

// NewAlibabaSWASProvider creates a new Alibaba SWAS provider for international regions
func NewAlibabaSWASProvider(accessKeyID, accessKeySecret, region string) *AlibabaSWASProvider {
	return &AlibabaSWASProvider{
		accessKeyID:     accessKeyID,
		accessKeySecret: accessKeySecret,
		region:          region,
		client:          &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *AlibabaSWASProvider) Name() string {
	return ProviderAlibabaSWAS
}

func (p *AlibabaSWASProvider) GetInstanceStatus(ctx context.Context, instanceID string) (*InstanceStatus, error) {
	instanceInfo, err := p.listInstances(ctx, instanceID)
	if err != nil {
		return nil, err
	}
	if len(instanceInfo) == 0 {
		return nil, fmt.Errorf("instance not found: %s", instanceID)
	}

	inst := instanceInfo[0]

	trafficUsed, trafficTotal, err := p.getTrafficPackage(ctx, instanceID)
	if err != nil {
		log.Warnf(ctx, "[ALIBABA] Failed to get traffic info: %v", err)
	}

	return &InstanceStatus{
		InstanceID:        inst.InstanceID,
		Name:              inst.InstanceName,
		IPAddress:         inst.PublicIPAddress,
		IPv6Address:       inst.Ipv6Address,
		Region:            p.region,
		TrafficUsedBytes:  trafficUsed,
		TrafficTotalBytes: trafficTotal,
		TrafficResetAt:    time.Time{},
		ExpiresAt:         inst.ExpiredTime,
		State:             inst.Status,
	}, nil
}

type alibabaInstance struct {
	InstanceID      string
	InstanceName    string
	PublicIPAddress string
	Ipv6Address     string
	Status          string
	ExpiredTime     time.Time
}

func (p *AlibabaSWASProvider) listInstances(ctx context.Context, instanceID string) ([]alibabaInstance, error) {
	log.Infof(ctx, "[ALIBABA] ListInstances: region=%s, instanceID=%s", p.region, instanceID)

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
		log.Errorf(ctx, "[ALIBABA] ListInstances failed: %v", err)
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

	instances := make([]alibabaInstance, len(result.Instances))
	for i, inst := range result.Instances {
		expiredTime, _ := time.Parse("2006-01-02T15:04:05Z", inst.ExpiredTime)
		instances[i] = alibabaInstance{
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

func (p *AlibabaSWASProvider) getTrafficPackage(ctx context.Context, instanceID string) (used, total int64, err error) {
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

func (p *AlibabaSWASProvider) ListInstances(ctx context.Context) ([]*InstanceStatus, error) {
	instances, err := p.listInstances(ctx, "")
	if err != nil {
		return nil, err
	}

	statuses := make([]*InstanceStatus, 0, len(instances))
	for _, inst := range instances {
		status, err := p.GetInstanceStatus(ctx, inst.InstanceID)
		if err != nil {
			log.Warnf(ctx, "[ALIBABA] Failed to get status for %s: %v", inst.InstanceID, err)
			continue
		}
		statuses = append(statuses, status)
	}

	return statuses, nil
}

func (p *AlibabaSWASProvider) ChangeIP(ctx context.Context, instanceID string, opts ChangeIPOptions) (*OperationResult, error) {
	return nil, &NotSupportedError{Provider: ProviderAlibabaSWAS, Operation: "ChangeIP"}
}

func (p *AlibabaSWASProvider) CreateInstance(ctx context.Context, opts CreateInstanceOptions) (*OperationResult, error) {
	log.Infof(ctx, "[ALIBABA] Creating instance: region=%s, plan=%s, image=%s", opts.Region, opts.Plan, opts.ImageID)

	params := map[string]string{
		"Action":    "CreateInstances",
		"RegionId":  p.region,
		"PlanId":    opts.Plan,
		"ImageId":   opts.ImageID,
		"Period":    "1",
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

	log.Infof(ctx, "[ALIBABA] Instance created: %s", result.InstanceIds[0])

	return &OperationResult{
		Success: true,
		Message: "Instance created successfully",
		Data: map[string]any{
			"instance_id": result.InstanceIds[0],
		},
	}, nil
}

func (p *AlibabaSWASProvider) DeleteInstance(ctx context.Context, instanceID string) (*OperationResult, error) {
	log.Infof(ctx, "[ALIBABA] Deleting instance: %s", instanceID)

	params := map[string]string{
		"Action":      "DeleteInstances",
		"RegionId":    p.region,
		"InstanceIds": fmt.Sprintf(`["%s"]`, instanceID),
	}

	_, err := p.doRequest(ctx, params)
	if err != nil {
		return nil, err
	}

	log.Infof(ctx, "[ALIBABA] Instance deleted: %s", instanceID)

	return &OperationResult{
		Success: true,
		Message: "Instance deleted successfully",
	}, nil
}

func (p *AlibabaSWASProvider) doRequest(ctx context.Context, params map[string]string) ([]byte, error) {
	return p.doRequestWithRegion(ctx, params, p.region)
}

func (p *AlibabaSWASProvider) doRequestWithRegion(ctx context.Context, params map[string]string, region string) ([]byte, error) {
	params["Format"] = "JSON"
	params["Version"] = "2020-06-01"
	params["AccessKeyId"] = p.accessKeyID
	params["SignatureMethod"] = "HMAC-SHA1"
	params["SignatureVersion"] = "1.0"
	params["SignatureNonce"] = uuid.New().String()
	params["Timestamp"] = time.Now().UTC().Format("2006-01-02T15:04:05Z")

	signature := p.sign(params)
	params["Signature"] = signature

	values := url.Values{}
	for k, v := range params {
		values.Set(k, v)
	}

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

	var errResp struct {
		Code    string `json:"Code"`
		Message string `json:"Message"`
	}
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Code != "" {
		return nil, fmt.Errorf("alibaba API error: %s - %s", errResp.Code, errResp.Message)
	}

	return body, nil
}

func (p *AlibabaSWASProvider) sign(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var pairs []string
	for _, k := range keys {
		pairs = append(pairs, fmt.Sprintf("%s=%s",
			alibabaPercentEncode(k),
			alibabaPercentEncode(params[k])))
	}
	canonicalQuery := strings.Join(pairs, "&")

	stringToSign := fmt.Sprintf("GET&%s&%s",
		alibabaPercentEncode("/"),
		alibabaPercentEncode(canonicalQuery))

	h := hmac.New(sha1.New, []byte(p.accessKeySecret+"&"))
	h.Write([]byte(stringToSign))
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))

	return signature
}

func alibabaPercentEncode(s string) string {
	s = url.QueryEscape(s)
	s = strings.ReplaceAll(s, "+", "%20")
	s = strings.ReplaceAll(s, "*", "%2A")
	s = strings.ReplaceAll(s, "%7E", "~")
	return s
}

func (p *AlibabaSWASProvider) ListRegions(ctx context.Context) ([]RegionInfo, error) {
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
		unifiedRegion := GetRegionByProviderID(ProviderAlibabaSWAS, r.RegionId)
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

func (p *AlibabaSWASProvider) ListPlans(ctx context.Context, region string) ([]PlanInfo, error) {
	targetRegion := region
	if targetRegion == "" {
		targetRegion = p.region
	}

	params := map[string]string{
		"Action":   "ListPlans",
		"RegionId": targetRegion,
	}

	resp, err := p.doRequestWithRegion(ctx, params, targetRegion)
	if err != nil {
		return nil, err
	}

	var result struct {
		Plans []struct {
			PlanId      string  `json:"PlanId"`
			Core        int     `json:"Core"`
			Memory      int     `json:"Memory"`
			Bandwidth   int     `json:"Bandwidth"`
			Flow        int     `json:"Flow"`
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

func (p *AlibabaSWASProvider) ListImages(ctx context.Context, region string) ([]ImageInfo, error) {
	targetRegion := region
	if targetRegion == "" {
		targetRegion = p.region
	}

	params := map[string]string{
		"Action":   "ListImages",
		"RegionId": targetRegion,
	}

	resp, err := p.doRequestWithRegion(ctx, params, targetRegion)
	if err != nil {
		return nil, err
	}

	var result struct {
		Images []struct {
			ImageId     string `json:"ImageId"`
			ImageName   string `json:"ImageName"`
			ImageType   string `json:"ImageType"`
			OsType      string `json:"OsType"`
			Platform    string `json:"Platform"`
			Description string `json:"Description"`
		} `json:"Images"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	var images []ImageInfo
	for _, img := range result.Images {
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

// MultiRegionAlibabaSWASProvider manages Alibaba SWAS instances across all international regions
type MultiRegionAlibabaSWASProvider struct {
	accessKeyID     string
	accessKeySecret string
	providers       map[string]*AlibabaSWASProvider
}

// NewMultiRegionAlibabaSWASProvider creates a provider that manages instances across all Alibaba SWAS international regions
func NewMultiRegionAlibabaSWASProvider(accessKeyID, accessKeySecret string) (*MultiRegionAlibabaSWASProvider, error) {
	mp := &MultiRegionAlibabaSWASProvider{
		accessKeyID:     accessKeyID,
		accessKeySecret: accessKeySecret,
		providers:       make(map[string]*AlibabaSWASProvider),
	}

	for _, region := range GetAlibabaSWASRegions() {
		p := NewAlibabaSWASProvider(accessKeyID, accessKeySecret, region)
		mp.providers[region] = p
	}

	if len(mp.providers) == 0 {
		return nil, fmt.Errorf("failed to create any regional providers")
	}

	return mp, nil
}

func (mp *MultiRegionAlibabaSWASProvider) Name() string {
	return ProviderAlibabaSWAS
}

func (mp *MultiRegionAlibabaSWASProvider) getProviderForRegion(region string) *AlibabaSWASProvider {
	if p, ok := mp.providers[region]; ok {
		return p
	}

	alibabaRegion := GetProviderRegion(region, ProviderAlibabaSWAS)
	if alibabaRegion != "" {
		if p, ok := mp.providers[alibabaRegion]; ok {
			return p
		}
	}

	return nil
}

func (mp *MultiRegionAlibabaSWASProvider) GetInstanceStatus(ctx context.Context, instanceID string) (*InstanceStatus, error) {
	for _, p := range mp.providers {
		status, err := p.GetInstanceStatus(ctx, instanceID)
		if err == nil {
			return status, nil
		}
	}
	return nil, fmt.Errorf("instance not found: %s", instanceID)
}

func (mp *MultiRegionAlibabaSWASProvider) ListInstances(ctx context.Context) ([]*InstanceStatus, error) {
	var allStatuses []*InstanceStatus

	for region, p := range mp.providers {
		statuses, err := p.ListInstances(ctx)
		if err != nil {
			log.Warnf(ctx, "[ALIBABA] Failed to list instances in region %s: %v", region, err)
			continue
		}
		allStatuses = append(allStatuses, statuses...)
	}

	return allStatuses, nil
}

func (mp *MultiRegionAlibabaSWASProvider) ChangeIP(ctx context.Context, instanceID string, opts ChangeIPOptions) (*OperationResult, error) {
	for _, p := range mp.providers {
		_, err := p.GetInstanceStatus(ctx, instanceID)
		if err == nil {
			return p.ChangeIP(ctx, instanceID, opts)
		}
	}
	return nil, fmt.Errorf("instance not found: %s", instanceID)
}

func (mp *MultiRegionAlibabaSWASProvider) CreateInstance(ctx context.Context, opts CreateInstanceOptions) (*OperationResult, error) {
	p := mp.getProviderForRegion(opts.Region)
	if p == nil {
		return nil, fmt.Errorf("unknown region: %s", opts.Region)
	}
	return p.CreateInstance(ctx, opts)
}

func (mp *MultiRegionAlibabaSWASProvider) DeleteInstance(ctx context.Context, instanceID string) (*OperationResult, error) {
	for _, p := range mp.providers {
		_, err := p.GetInstanceStatus(ctx, instanceID)
		if err == nil {
			return p.DeleteInstance(ctx, instanceID)
		}
	}
	return nil, fmt.Errorf("instance not found: %s", instanceID)
}

func (mp *MultiRegionAlibabaSWASProvider) ListRegions(ctx context.Context) ([]RegionInfo, error) {
	for _, p := range mp.providers {
		return p.ListRegions(ctx)
	}
	return nil, fmt.Errorf("no regional providers available")
}

func (mp *MultiRegionAlibabaSWASProvider) ListPlans(ctx context.Context, region string) ([]PlanInfo, error) {
	p := mp.getProviderForRegion(region)
	if p == nil {
		for _, p := range mp.providers {
			return p.ListPlans(ctx, region)
		}
		return nil, fmt.Errorf("no regional providers available")
	}
	return p.ListPlans(ctx, region)
}

func (mp *MultiRegionAlibabaSWASProvider) ListImages(ctx context.Context, region string) ([]ImageInfo, error) {
	p := mp.getProviderForRegion(region)
	if p == nil {
		for _, p := range mp.providers {
			return p.ListImages(ctx, region)
		}
		return nil, fmt.Errorf("no regional providers available")
	}
	return p.ListImages(ctx, region)
}
