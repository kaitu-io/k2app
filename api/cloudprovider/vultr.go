package cloudprovider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const vultrAPIBase = "https://api.vultr.com/v2"

// VultrProvider implements Provider for Vultr (sync only).
//
// Vultr 的流量额度是全账号共享的池：只计出站，所有实例的额度与每账号赠送额度合并。
// 所以每台实例只上报自己的当月出站量，额度恒为 0（无单台额度）：单台的
// allowed_bandwidth 按开机小时折算（10/27 开的机器只有 277 GB），拿它算比例会把
// 一台远未超额的新机器显示成告急。账号池的已用/总量由 AccountPool 单独给出。
type VultrProvider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// NewVultrProvider creates a new Vultr provider
func NewVultrProvider(apiKey string) *VultrProvider {
	return &VultrProvider{
		apiKey:  apiKey,
		baseURL: vultrAPIBase,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *VultrProvider) Name() string {
	return ProviderVultr
}

type vultrInstance struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	MainIP   string `json:"main_ip"`
	V6MainIP string `json:"v6_main_ip"`
	Region   string `json:"region"`
	Status   string `json:"status"`
}

type vultrBandwidthMonth struct {
	GBOut                     int64 `json:"gb_out"`
	FreeBandwidthCredits      int64 `json:"free_bandwidth_credits"`
	PurchasedBandwidthCredits int64 `json:"purchased_bandwidth_credits"`
	InstanceBandwidthCredits  int64 `json:"instance_bandwidth_credits"`
}

func (p *VultrProvider) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("vultr API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read vultr response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("vultr API %s: status %d", path, resp.StatusCode)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("parse vultr response %s: %w", path, err)
	}
	return nil
}

// AccountPool returns the account pool's outbound usage and allowance in bytes.
// Allowance uses the month's projection, which already counts every instance's
// hours through month end; month-to-date would understate it until the last day.
func (p *VultrProvider) AccountPool(ctx context.Context) (*AccountPool, error) {
	var resp struct {
		Bandwidth struct {
			CurrentMonthToDate    vultrBandwidthMonth `json:"current_month_to_date"`
			CurrentMonthProjected vultrBandwidthMonth `json:"current_month_projected"`
		} `json:"bandwidth"`
	}
	if err := p.get(ctx, "/account/bandwidth", &resp); err != nil {
		return nil, err
	}
	month := resp.Bandwidth.CurrentMonthProjected
	totalGB := month.InstanceBandwidthCredits + month.FreeBandwidthCredits + month.PurchasedBandwidthCredits
	return &AccountPool{
		UsedBytes:  resp.Bandwidth.CurrentMonthToDate.GBOut << 30,
		TotalBytes: totalGB << 30,
		ResetAt:    vultrNextReset(time.Now()),
	}, nil
}

func vultrNextReset(now time.Time) time.Time {
	now = now.UTC()
	return time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC)
}

// monthOutbound sums one instance's outbound bytes for now's calendar month (UTC).
// The endpoint returns a rolling day window, so days from last month are skipped.
func (p *VultrProvider) monthOutbound(ctx context.Context, instanceID string, now time.Time) (int64, error) {
	var resp struct {
		Bandwidth map[string]struct {
			OutgoingBytes int64 `json:"outgoing_bytes"`
		} `json:"bandwidth"`
	}
	if err := p.get(ctx, "/instances/"+instanceID+"/bandwidth?date_range=31", &resp); err != nil {
		return 0, err
	}
	month := now.UTC().Format("2006-01-")
	var used int64
	for day, usage := range resp.Bandwidth {
		if strings.HasPrefix(day, month) {
			used += usage.OutgoingBytes
		}
	}
	return used, nil
}

func (p *VultrProvider) ListInstances(ctx context.Context) ([]*InstanceStatus, error) {
	var resp struct {
		Instances []vultrInstance `json:"instances"`
	}
	if err := p.get(ctx, "/instances?per_page=500", &resp); err != nil {
		return nil, err
	}

	now := time.Now()
	resetAt := vultrNextReset(now)

	statuses := make([]*InstanceStatus, 0, len(resp.Instances))
	for _, inst := range resp.Instances {
		// One failed lookup fails the whole list: the sync then keeps last known
		// figures instead of overwriting this instance with 0.
		used, err := p.monthOutbound(ctx, inst.ID, now)
		if err != nil {
			return nil, err
		}
		statuses = append(statuses, &InstanceStatus{
			InstanceID:       inst.ID,
			Name:             inst.Label,
			IPAddress:        inst.MainIP,
			IPv6Address:      inst.V6MainIP,
			Region:           inst.Region,
			TrafficUsedBytes: used,
			TrafficResetAt:   resetAt,
			State:            inst.Status,
		})
	}
	return statuses, nil
}

func (p *VultrProvider) GetInstanceStatus(ctx context.Context, instanceID string) (*InstanceStatus, error) {
	statuses, err := p.ListInstances(ctx)
	if err != nil {
		return nil, err
	}
	for _, status := range statuses {
		if status.InstanceID == instanceID {
			return status, nil
		}
	}
	return nil, fmt.Errorf("vultr instance not found: %s", instanceID)
}

func (p *VultrProvider) ChangeIP(ctx context.Context, instanceID string, opts ChangeIPOptions) (*OperationResult, error) {
	return nil, &NotSupportedError{Provider: ProviderVultr, Operation: "ChangeIP"}
}

func (p *VultrProvider) CreateInstance(ctx context.Context, opts CreateInstanceOptions) (*OperationResult, error) {
	return nil, &NotSupportedError{Provider: ProviderVultr, Operation: "CreateInstance"}
}

func (p *VultrProvider) DeleteInstance(ctx context.Context, instanceID string) (*OperationResult, error) {
	return nil, &NotSupportedError{Provider: ProviderVultr, Operation: "DeleteInstance"}
}

func (p *VultrProvider) ListRegions(ctx context.Context) ([]RegionInfo, error) {
	return []RegionInfo{}, nil
}

func (p *VultrProvider) ListPlans(ctx context.Context, region string) ([]PlanInfo, error) {
	return []PlanInfo{}, nil
}

func (p *VultrProvider) ListImages(ctx context.Context, region string) ([]ImageInfo, error) {
	return []ImageInfo{}, nil
}
