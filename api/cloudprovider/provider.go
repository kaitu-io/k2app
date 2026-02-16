// Package cloudprovider provides unified interface for cloud VPS management
package cloudprovider

import (
	"context"
	"fmt"
	"time"
)

// Provider names
const (
	ProviderAliyunSWAS        = "aliyun_swas"        // Alibaba Cloud Domestic regions
	ProviderAlibabaSWAS       = "alibaba_swas"       // Alibaba Cloud International regions
	ProviderAWSLightsail      = "aws_lightsail"
	ProviderBandwagon         = "bandwagon"
	ProviderTencentLighthouse = "tencent_lighthouse" // Tencent Cloud International regions
	ProviderQCloudLighthouse  = "qcloud_lighthouse"  // Tencent Cloud Domestic regions
	ProviderSSHStandalone     = "ssh_standalone"     // SSH-only hosts without cloud API
)

// InstanceStatus represents current state of a cloud instance
type InstanceStatus struct {
	InstanceID        string
	Name              string    // Instance name/hostname from provider
	IPAddress         string    // IPv4 address
	IPv6Address       string    // IPv6 address (if available)
	Region            string
	TrafficUsedBytes  int64
	TrafficTotalBytes int64
	TrafficResetAt    time.Time // Next traffic reset date
	ExpiresAt         time.Time // Instance expiration (zero for auto-renew)
	State             string    // running, stopped, migrating, etc.
}

// OperationResult represents result of a cloud operation
type OperationResult struct {
	Success bool
	Message string
	Data    map[string]interface{} // e.g., {"new_ip": "1.2.3.4", "task_id": "xxx"}
}

// ChangeIPOptions contains options for IP change operation
type ChangeIPOptions struct {
	TargetRegion string // For BandwagonHost: target datacenter code
}

// CreateInstanceOptions contains options for instance creation
type CreateInstanceOptions struct {
	Region  string
	Plan    string // Instance plan/bundle ID
	ImageID string // OS image ID
	Name    string // Instance name
}

// RegionInfo describes an available region (returned by ListRegions)
type RegionInfo struct {
	Slug       string `json:"slug"`       // Unified slug: "us-virginia"
	NameEN     string `json:"nameEn"`     // English: "US East (Virginia)"
	NameZH     string `json:"nameZh"`     // Chinese: "美国东部（弗吉尼亚）"
	Country    string `json:"country"`    // Country code: "US"
	ProviderID string `json:"providerId"` // Provider-specific ID: "us-east-1"
	Available  bool   `json:"available"`  // Can create new instances
}

// PlanInfo describes an instance plan/bundle
type PlanInfo struct {
	ID           string  `json:"id"`           // Provider-specific plan ID
	Name         string  `json:"name"`         // Display name
	CPU          int     `json:"cpu"`          // vCPU count
	MemoryMB     int     `json:"memoryMb"`     // Memory in MB
	StorageGB    int     `json:"storageGb"`    // Storage in GB
	TransferTB   float64 `json:"transferTb"`   // Monthly transfer in TB
	PriceMonthly float64 `json:"priceMonthly"` // Monthly price in USD
}

// ImageInfo describes an OS image
type ImageInfo struct {
	ID          string `json:"id"`          // Provider-specific image ID
	Name        string `json:"name"`        // Display name (e.g., "Ubuntu 22.04 LTS")
	OS          string `json:"os"`          // OS type (linux, windows)
	Platform    string `json:"platform"`    // Platform (ubuntu, centos, debian, windows)
	Description string `json:"description"` // Description
}

// Provider defines the interface for cloud provider operations
type Provider interface {
	// Name returns the provider identifier
	Name() string

	// GetInstanceStatus retrieves current instance status including traffic
	GetInstanceStatus(ctx context.Context, instanceID string) (*InstanceStatus, error)

	// ListInstances lists all instances under this account
	ListInstances(ctx context.Context) ([]*InstanceStatus, error)

	// ChangeIP changes the instance IP address
	// Returns NotSupportedError if provider doesn't support this
	ChangeIP(ctx context.Context, instanceID string, opts ChangeIPOptions) (*OperationResult, error)

	// CreateInstance creates a new instance
	// Returns NotSupportedError if provider doesn't support this
	CreateInstance(ctx context.Context, opts CreateInstanceOptions) (*OperationResult, error)

	// DeleteInstance deletes/terminates an instance
	// Returns NotSupportedError if provider doesn't support this
	DeleteInstance(ctx context.Context, instanceID string) (*OperationResult, error)

	// ListRegions returns available regions for this provider
	// Returns empty slice if provider doesn't support multiple regions (e.g., Bandwagon)
	ListRegions(ctx context.Context) ([]RegionInfo, error)

	// ListPlans returns available instance plans/bundles
	// region can be empty for providers that have global plans
	ListPlans(ctx context.Context, region string) ([]PlanInfo, error)

	// ListImages returns available OS images
	// region can be empty for providers that have global images
	ListImages(ctx context.Context, region string) ([]ImageInfo, error)
}

// NotSupportedError indicates the operation is not supported by this provider
type NotSupportedError struct {
	Provider  string
	Operation string
}

func (e *NotSupportedError) Error() string {
	return fmt.Sprintf("%s does not support %s operation", e.Provider, e.Operation)
}

// IsNotSupported checks if error is NotSupportedError
func IsNotSupported(err error) bool {
	_, ok := err.(*NotSupportedError)
	return ok
}
