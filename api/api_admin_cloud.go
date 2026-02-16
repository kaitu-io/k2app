package center

import (
	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"

	"github.com/kaitu-io/k2app/api/cloudprovider"
)

// DataCloudInstance represents a cloud instance in API response.
// Note: Status field removed - instance online status is determined by associated SlaveNode existence.
type DataCloudInstance struct {
	ID             uint64  `json:"id"`
	Provider       string  `json:"provider"`
	AccountName    string  `json:"account_name"`
	InstanceID     string  `json:"instance_id"`
	Name           string  `json:"name"`
	IPAddress      string  `json:"ip_address"`
	IPv6Address    string  `json:"ipv6_address,omitempty"`
	Region         string  `json:"region"`
	TrafficUsedGB  float64 `json:"traffic_used_gb"`
	TrafficTotalGB float64 `json:"traffic_total_gb"`
	TrafficRatio   float64 `json:"traffic_ratio"`   // Pre-calculated traffic usage ratio (0-1)
	TrafficResetAt int64   `json:"traffic_reset_at"`
	ExpiresAt      int64   `json:"expires_at"`
	TimeRatio      float64 `json:"time_ratio"` // Pre-calculated billing cycle time ratio (0-1)
	LastSyncedAt   int64   `json:"last_synced_at"`
	SyncError      string  `json:"sync_error,omitempty"`
	NodeName       string  `json:"node_name,omitempty"`
}

// DataCloudAccount represents cloud account in API response (no secrets)
type DataCloudAccount struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Region   string `json:"region"`
}

func api_admin_list_cloud_instances(c *gin.Context) {
	log.Infof(c, "admin request to list cloud instances")
	pagination := PaginationFromRequest(c)

	var instances []CloudInstance
	query := db.Get().Model(&CloudInstance{})

	// Filter by provider
	if provider := c.Query("provider"); provider != "" {
		query = query.Where("provider = ?", provider)
	}

	// Filter by account
	if account := c.Query("account"); account != "" {
		query = query.Where("account_name = ?", account)
	}

	if err := query.Count(&pagination.Total).Error; err != nil {
		log.Errorf(c, "failed to count cloud instances: %v", err)
		Error(c, ErrorSystemError, "failed to count instances")
		return
	}

	if err := query.Order("id DESC").
		Offset(pagination.Offset()).
		Limit(pagination.PageSize).
		Find(&instances).Error; err != nil {
		log.Errorf(c, "failed to list cloud instances: %v", err)
		Error(c, ErrorSystemError, "failed to list instances")
		return
	}

	// Collect IPs for batch node lookup
	ips := make([]string, len(instances))
	for i, inst := range instances {
		ips[i] = inst.IPAddress
	}

	// Batch query nodes by IP
	var nodes []SlaveNode
	nodeMap := make(map[string]*SlaveNode)
	if len(ips) > 0 {
		db.Get().Where("ipv4 IN ?", ips).Find(&nodes)
		for i := range nodes {
			nodeMap[nodes[i].Ipv4] = &nodes[i]
		}
	}

	// Convert to response format
	items := make([]DataCloudInstance, len(instances))
	for i, inst := range instances {
		// Calculate traffic ratio (0-1)
		trafficRatio := 0.0
		if inst.TrafficTotalBytes > 0 {
			trafficRatio = float64(inst.TrafficUsedBytes) / float64(inst.TrafficTotalBytes)
			if trafficRatio > 1 {
				trafficRatio = 1
			}
		}

		// Determine billing cycle end and calculate time ratio
		billingCycleEndAt := inst.TrafficResetAt
		if billingCycleEndAt == 0 {
			billingCycleEndAt = inst.ExpiresAt
		}
		timeRatio := calculateTimeRatio(billingCycleEndAt)

		items[i] = DataCloudInstance{
			ID:             inst.ID,
			Provider:       inst.Provider,
			AccountName:    inst.AccountName,
			InstanceID:     inst.InstanceID,
			Name:           inst.Name,
			IPAddress:      inst.IPAddress,
			IPv6Address:    inst.IPv6Address,
			Region:         inst.Region,
			TrafficUsedGB:  float64(inst.TrafficUsedBytes) / (1024 * 1024 * 1024),
			TrafficTotalGB: float64(inst.TrafficTotalBytes) / (1024 * 1024 * 1024),
			TrafficRatio:   trafficRatio,
			TrafficResetAt: inst.TrafficResetAt,
			ExpiresAt:      inst.ExpiresAt,
			TimeRatio:      timeRatio,
			LastSyncedAt:   inst.LastSyncedAt,
			SyncError:      inst.SyncError,
		}

		// Add node info if exists
		if node, exists := nodeMap[inst.IPAddress]; exists {
			items[i].NodeName = node.Name
		}
	}

	log.Infof(c, "successfully listed %d cloud instances", len(instances))
	ListWithData(c, items, pagination)
}

func api_admin_get_cloud_instance(c *gin.Context) {
	id := c.Param("id")
	log.Infof(c, "admin request to get cloud instance %s", id)

	var instance CloudInstance
	if err := db.Get().First(&instance, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			Error(c, ErrorNotFound, "instance not found")
		} else {
			log.Errorf(c, "failed to get cloud instance: %v", err)
			Error(c, ErrorSystemError, "failed to get instance")
		}
		return
	}

	// Get associated node
	var node SlaveNode
	nodeExists := db.Get().Where("ipv4 = ?", instance.IPAddress).First(&node).Error == nil

	// Calculate traffic ratio (0-1)
	trafficRatio := 0.0
	if instance.TrafficTotalBytes > 0 {
		trafficRatio = float64(instance.TrafficUsedBytes) / float64(instance.TrafficTotalBytes)
		if trafficRatio > 1 {
			trafficRatio = 1
		}
	}

	// Determine billing cycle end and calculate time ratio
	billingCycleEndAt := instance.TrafficResetAt
	if billingCycleEndAt == 0 {
		billingCycleEndAt = instance.ExpiresAt
	}
	timeRatio := calculateTimeRatio(billingCycleEndAt)

	result := DataCloudInstance{
		ID:             instance.ID,
		Provider:       instance.Provider,
		AccountName:    instance.AccountName,
		InstanceID:     instance.InstanceID,
		Name:           instance.Name,
		IPAddress:      instance.IPAddress,
		IPv6Address:    instance.IPv6Address,
		Region:         instance.Region,
		TrafficUsedGB:  float64(instance.TrafficUsedBytes) / (1024 * 1024 * 1024),
		TrafficTotalGB: float64(instance.TrafficTotalBytes) / (1024 * 1024 * 1024),
		TrafficRatio:   trafficRatio,
		TrafficResetAt: instance.TrafficResetAt,
		ExpiresAt:      instance.ExpiresAt,
		TimeRatio:      timeRatio,
		LastSyncedAt:   instance.LastSyncedAt,
		SyncError:      instance.SyncError,
	}

	if nodeExists {
		result.NodeName = node.Name
	}

	Success(c, &result)
}

func api_admin_sync_all_cloud_instances(c *gin.Context) {
	log.Infof(c, "admin request to sync all cloud instances")

	// Empty AccountName = sync all accounts
	payload := CloudSyncPayload{}
	taskID, err := enqueueCloudSync(c, payload)
	if err != nil {
		log.Errorf(c, "failed to enqueue sync task: %v", err)
		Error(c, ErrorSystemError, "failed to trigger sync")
		return
	}

	Success(c, &map[string]string{"task_id": taskID})
}

// enqueueCloudSync enqueues a cloud sync task for immediate execution
func enqueueCloudSync(c *gin.Context, payload CloudSyncPayload) (string, error) {
	info, err := enqueueCloudTask(TaskTypeCloudSyncAll, payload)
	if err != nil {
		return "", err
	}
	return info, nil
}

// CloudChangeIPRequest represents request to change IP
type CloudChangeIPRequest struct {
	TargetRegion string `json:"target_region,omitempty"` // For BandwagonHost
}

func api_admin_change_ip_cloud_instance(c *gin.Context) {
	id := c.Param("id")
	log.Infof(c, "admin request to change IP for cloud instance %s", id)

	var req CloudChangeIPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// Optional body, ignore error
	}

	var instance CloudInstance
	if err := db.Get().First(&instance, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			Error(c, ErrorNotFound, "instance not found")
		} else {
			Error(c, ErrorSystemError, "failed to get instance")
		}
		return
	}

	payload := CloudChangeIPPayload{
		CloudInstanceID: instance.ID,
		TargetRegion:    req.TargetRegion,
	}

	taskID, err := ScheduleCloudTask(TaskTypeCloudChangeIP, payload)
	if err != nil {
		log.Errorf(c, "failed to schedule change IP task: %v", err)
		Error(c, ErrorSystemError, "failed to schedule task")
		return
	}

	Success(c, &map[string]string{"task_id": taskID})
}

// CloudCreateRequest represents request to create instance
type CloudCreateRequest struct {
	AccountName string `json:"account_name" binding:"required"`
	Region      string `json:"region" binding:"required"`
	Plan        string `json:"plan" binding:"required"`
	ImageID     string `json:"image_id" binding:"required"`
	Name        string `json:"name" binding:"required"`
}

func api_admin_create_cloud_instance(c *gin.Context) {
	log.Infof(c, "admin request to create cloud instance")

	var req CloudCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	// Verify account exists and get provider type
	account := ConfigCloudInstanceAccountByName(req.AccountName)
	if account == nil {
		Error(c, ErrorNotFound, "account not found")
		return
	}

	// SSH standalone provider doesn't support CreateInstance
	if account.Provider == cloudprovider.ProviderSSHStandalone {
		Error(c, ErrorNotSupported, "ssh_standalone provider doesn't support creating instances; instances are auto-detected from SlaveNodes")
		return
	}

	// For other providers, use the async task queue
	payload := CloudCreatePayload{
		AccountName: req.AccountName,
		Region:      req.Region,
		Plan:        req.Plan,
		ImageID:     req.ImageID,
		Name:        req.Name,
	}

	taskID, err := ScheduleCloudTask(TaskTypeCloudCreate, payload)
	if err != nil {
		log.Errorf(c, "failed to schedule create task: %v", err)
		Error(c, ErrorSystemError, "failed to schedule task")
		return
	}

	Success(c, &map[string]string{"task_id": taskID})
}

func api_admin_delete_cloud_instance(c *gin.Context) {
	id := c.Param("id")
	log.Infof(c, "admin request to delete cloud instance %s", id)

	var instance CloudInstance
	if err := db.Get().First(&instance, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			Error(c, ErrorNotFound, "instance not found")
		} else {
			Error(c, ErrorSystemError, "failed to get instance")
		}
		return
	}

	// SSH standalone provider doesn't support DeleteInstance
	if instance.Provider == cloudprovider.ProviderSSHStandalone {
		Error(c, ErrorNotSupported, "ssh_standalone provider doesn't support deleting instances; delete the SlaveNode instead")
		return
	}

	// For other providers, use the async task queue
	payload := CloudDeletePayload{
		CloudInstanceID: instance.ID,
	}

	taskID, err := ScheduleCloudTask(TaskTypeCloudDelete, payload)
	if err != nil {
		log.Errorf(c, "failed to schedule delete task: %v", err)
		Error(c, ErrorSystemError, "failed to schedule task")
		return
	}

	Success(c, &map[string]string{"task_id": taskID})
}

func api_admin_list_cloud_accounts(c *gin.Context) {
	log.Infof(c, "admin request to list cloud accounts")

	// Start with built-in ssh_standalone account (always available)
	items := []DataCloudAccount{
		{
			Name:     "ssh_standalone",
			Provider: cloudprovider.ProviderSSHStandalone,
			Region:   "",
		},
	}

	// Add configured accounts
	accounts := ConfigCloudInstance().Accounts
	for _, acc := range accounts {
		items = append(items, DataCloudAccount{
			Name:     acc.Name,
			Provider: acc.Provider,
			Region:   acc.Region,
		})
	}

	ListWithData(c, items, &Pagination{Total: int64(len(items))})
}

// api_admin_list_cloud_regions returns available regions for a provider
func api_admin_list_cloud_regions(c *gin.Context) {
	provider := c.Query("provider")
	accountName := c.Query("account")

	log.Infof(c, "admin request to list cloud regions: provider=%s, account=%s", provider, accountName)

	// If account specified, use that account's provider
	var account *CloudInstanceAccount
	if accountName != "" {
		account = ConfigCloudInstanceAccountByName(accountName)
		if account == nil {
			Error(c, ErrorNotFound, "account not found")
			return
		}
		provider = account.Provider
	}

	// If no provider specified, return unified region list
	if provider == "" {
		regions := cloudprovider.AllRegions
		items := make([]cloudprovider.RegionInfo, len(regions))
		for i, r := range regions {
			items[i] = cloudprovider.RegionInfo{
				Slug:      r.Slug,
				NameEN:    r.NameEN,
				NameZH:    r.NameZH,
				Country:   r.Country,
				Available: true,
			}
		}
		ListWithData(c, items, &Pagination{Total: int64(len(items))})
		return
	}

	// Get regions from provider
	if account == nil {
		// Find first account for this provider
		for i := range ConfigCloudInstance().Accounts {
			if ConfigCloudInstance().Accounts[i].Provider == provider {
				account = &ConfigCloudInstance().Accounts[i]
				break
			}
		}
	}

	if account == nil {
		// No account configured, return static regions from registry
		regions := cloudprovider.ListRegionsForProvider(provider)
		items := make([]cloudprovider.RegionInfo, len(regions))
		for i, r := range regions {
			providerID := r.Providers[provider]
			items[i] = cloudprovider.RegionInfo{
				Slug:       r.Slug,
				NameEN:     r.NameEN,
				NameZH:     r.NameZH,
				Country:    r.Country,
				ProviderID: providerID,
				Available:  true,
			}
		}
		ListWithData(c, items, &Pagination{Total: int64(len(items))})
		return
	}

	// Create provider and query regions
	p, err := cloudprovider.NewProvider(accountToProviderConfig(account))
	if err != nil {
		log.Errorf(c, "failed to create provider: %v", err)
		Error(c, ErrorSystemError, "failed to create provider")
		return
	}

	regions, err := p.ListRegions(c)
	if err != nil {
		if cloudprovider.IsNotSupported(err) {
			// Fall back to static regions from registry
			staticRegions := cloudprovider.ListRegionsForProvider(provider)
			items := make([]cloudprovider.RegionInfo, len(staticRegions))
			for i, r := range staticRegions {
				providerID := r.Providers[provider]
				items[i] = cloudprovider.RegionInfo{
					Slug:       r.Slug,
					NameEN:     r.NameEN,
					NameZH:     r.NameZH,
					Country:    r.Country,
					ProviderID: providerID,
					Available:  true,
				}
			}
			ListWithData(c, items, &Pagination{Total: int64(len(items))})
			return
		}
		log.Errorf(c, "failed to list regions: %v", err)
		Error(c, ErrorSystemError, "failed to list regions")
		return
	}

	ListWithData(c, regions, &Pagination{Total: int64(len(regions))})
}

// api_admin_list_cloud_plans returns available plans for a provider/region
func api_admin_list_cloud_plans(c *gin.Context) {
	accountName := c.Query("account")
	region := c.Query("region")

	log.Infof(c, "admin request to list cloud plans: account=%s, region=%s", accountName, region)

	if accountName == "" {
		Error(c, ErrorInvalidArgument, "account is required")
		return
	}

	account := ConfigCloudInstanceAccountByName(accountName)
	if account == nil {
		Error(c, ErrorNotFound, "account not found")
		return
	}

	p, err := cloudprovider.NewProvider(accountToProviderConfig(account))
	if err != nil {
		log.Errorf(c, "failed to create provider: %v", err)
		Error(c, ErrorSystemError, "failed to create provider")
		return
	}

	plans, err := p.ListPlans(c, region)
	if err != nil {
		if cloudprovider.IsNotSupported(err) {
			Error(c, ErrorNotSupported, "provider does not support listing plans")
			return
		}
		log.Errorf(c, "failed to list plans: %v", err)
		Error(c, ErrorSystemError, "failed to list plans")
		return
	}

	ListWithData(c, plans, &Pagination{Total: int64(len(plans))})
}

// api_admin_list_cloud_images returns available OS images for a provider/region
func api_admin_list_cloud_images(c *gin.Context) {
	accountName := c.Query("account")
	region := c.Query("region")

	log.Infof(c, "admin request to list cloud images: account=%s, region=%s", accountName, region)

	if accountName == "" {
		Error(c, ErrorInvalidArgument, "account is required")
		return
	}

	account := ConfigCloudInstanceAccountByName(accountName)
	if account == nil {
		Error(c, ErrorNotFound, "account not found")
		return
	}

	p, err := cloudprovider.NewProvider(accountToProviderConfig(account))
	if err != nil {
		log.Errorf(c, "failed to create provider: %v", err)
		Error(c, ErrorSystemError, "failed to create provider")
		return
	}

	images, err := p.ListImages(c, region)
	if err != nil {
		if cloudprovider.IsNotSupported(err) {
			Error(c, ErrorNotSupported, "provider does not support listing images")
			return
		}
		log.Errorf(c, "failed to list images: %v", err)
		Error(c, ErrorSystemError, "failed to list images")
		return
	}

	ListWithData(c, images, &Pagination{Total: int64(len(images))})
}

// CloudUpdateTrafficConfigRequest represents request to update traffic config
type CloudUpdateTrafficConfigRequest struct {
	TrafficTotalGB float64 `json:"traffic_total_gb" binding:"required,min=0"`
}

// api_admin_update_traffic_config updates traffic config for an SSH standalone instance
func api_admin_update_traffic_config(c *gin.Context) {
	id := c.Param("id")
	log.Infof(c, "admin request to update traffic config for cloud instance %s", id)

	var req CloudUpdateTrafficConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	var instance CloudInstance
	if err := db.Get().First(&instance, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			Error(c, ErrorNotFound, "instance not found")
		} else {
			Error(c, ErrorSystemError, "failed to get instance")
		}
		return
	}

	// Only SSH standalone provider supports updating traffic config
	if instance.Provider != cloudprovider.ProviderSSHStandalone {
		Error(c, ErrorNotSupported, "only ssh_standalone provider supports updating traffic config")
		return
	}

	// Convert GB to bytes
	trafficTotalBytes := int64(req.TrafficTotalGB * 1024 * 1024 * 1024)

	// Create provider and update config on remote host
	provider := cloudprovider.NewSSHStandaloneProvider(db.Get())

	// Keep existing reset time, only update total
	config := &cloudprovider.TrafficConfig{
		TrafficTotalBytes: trafficTotalBytes,
		TrafficResetAt:    instance.TrafficResetAt,
	}

	if err := provider.UpdateTrafficConfig(c, instance.IPAddress, config); err != nil {
		log.Errorf(c, "failed to update traffic config: %v", err)
		Error(c, ErrorSystemError, "failed to update traffic config on remote host")
		return
	}

	// Update local database
	if err := db.Get().Model(&instance).Updates(map[string]any{
		"traffic_total_bytes": trafficTotalBytes,
	}).Error; err != nil {
		log.Errorf(c, "failed to update database: %v", err)
		Error(c, ErrorSystemError, "updated remote but failed to update database")
		return
	}

	Success(c, &map[string]any{
		"traffic_total_gb": req.TrafficTotalGB,
		"message":          "traffic config updated",
	})
}

// enqueueCloudTask enqueues a cloud task for immediate execution
func enqueueCloudTask(taskType string, payload any) (string, error) {
	return ScheduleCloudTaskImmediate(taskType, payload)
}
