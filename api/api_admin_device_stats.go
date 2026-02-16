package center

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
)

// api_admin_get_device_statistics returns aggregated device statistics
// GET /app/devices/statistics
func api_admin_get_device_statistics(c *gin.Context) {
	var result DeviceStatisticsResponse

	// Get total device count
	db.Get().Model(&Device{}).Count(&result.TotalDevices)

	// Get platform breakdown
	type platformResult struct {
		Platform string
		Count    int64
	}
	var platformCounts []platformResult
	db.Get().Model(&Device{}).
		Select("COALESCE(NULLIF(app_platform, ''), 'unknown') as platform, COUNT(*) as count").
		Group("COALESCE(NULLIF(app_platform, ''), 'unknown')").
		Order("count DESC").
		Find(&platformCounts)

	// Convert to response format and calculate aggregates
	desktopPlatforms := map[string]bool{"darwin": true, "windows": true, "linux": true}
	mobilePlatforms := map[string]bool{"ios": true, "android": true}

	for _, pc := range platformCounts {
		result.ByPlatform = append(result.ByPlatform, PlatformCount{
			Platform: pc.Platform,
			Count:    pc.Count,
		})

		if pc.Platform == "unknown" || pc.Platform == "" {
			result.UnknownDevices += pc.Count
		} else if desktopPlatforms[pc.Platform] {
			result.DesktopDevices += pc.Count
		} else if mobilePlatforms[pc.Platform] {
			result.MobileDevices += pc.Count
		}
	}

	// Get version breakdown (top 10)
	type versionResult struct {
		Version string
		Count   int64
	}
	var versionCounts []versionResult
	db.Get().Model(&Device{}).
		Select("COALESCE(NULLIF(app_version, ''), 'unknown') as version, COUNT(*) as count").
		Group("COALESCE(NULLIF(app_version, ''), 'unknown')").
		Order("count DESC").
		Limit(10).
		Find(&versionCounts)

	for _, vc := range versionCounts {
		result.ByVersion = append(result.ByVersion, VersionCount{
			Version: vc.Version,
			Count:   vc.Count,
		})
	}

	// Get architecture breakdown
	type archResult struct {
		Arch  string
		Count int64
	}
	var archCounts []archResult
	db.Get().Model(&Device{}).
		Select("COALESCE(NULLIF(app_arch, ''), 'unknown') as arch, COUNT(*) as count").
		Group("COALESCE(NULLIF(app_arch, ''), 'unknown')").
		Order("count DESC").
		Find(&archCounts)

	for _, ac := range archCounts {
		result.ByArch = append(result.ByArch, ArchCount{
			Arch:  ac.Arch,
			Count: ac.Count,
		})
	}

	// Get OS version breakdown (top 10)
	type osVersionResult struct {
		OSVersion string
		Count     int64
	}
	var osVersionCounts []osVersionResult
	db.Get().Model(&Device{}).
		Select("COALESCE(NULLIF(os_version, ''), 'unknown') as os_version, COUNT(*) as count").
		Group("COALESCE(NULLIF(os_version, ''), 'unknown')").
		Order("count DESC").
		Limit(10).
		Find(&osVersionCounts)

	for _, ovc := range osVersionCounts {
		result.ByOSVersion = append(result.ByOSVersion, OSVersionCount{
			OSVersion: ovc.OSVersion,
			Count:     ovc.Count,
		})
	}

	// Get device model breakdown (top 10)
	type deviceModelResult struct {
		DeviceModel string
		Count       int64
	}
	var deviceModelCounts []deviceModelResult
	db.Get().Model(&Device{}).
		Select("COALESCE(NULLIF(device_model, ''), 'unknown') as device_model, COUNT(*) as count").
		Group("COALESCE(NULLIF(device_model, ''), 'unknown')").
		Order("count DESC").
		Limit(10).
		Find(&deviceModelCounts)

	for _, dmc := range deviceModelCounts {
		result.ByDeviceModel = append(result.ByDeviceModel, DeviceModelCount{
			DeviceModel: dmc.DeviceModel,
			Count:       dmc.Count,
		})
	}

	// Get active device counts
	now := time.Now().Unix()
	h24Ago := now - 24*60*60
	d7Ago := now - 7*24*60*60
	d30Ago := now - 30*24*60*60

	db.Get().Model(&Device{}).Where("token_last_used_at >= ?", h24Ago).Count(&result.Active24h)
	db.Get().Model(&Device{}).Where("token_last_used_at >= ?", d7Ago).Count(&result.Active7d)
	db.Get().Model(&Device{}).Where("token_last_used_at >= ?", d30Ago).Count(&result.Active30d)

	Success(c, &result)
}

// api_admin_get_active_devices returns paginated list of active devices
// GET /app/devices/active
func api_admin_get_active_devices(c *gin.Context) {
	// Parse query parameters
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	period := c.Query("period") // 24h, 7d, 30d

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	// Determine time filter
	now := time.Now().Unix()
	var sinceTime int64
	switch period {
	case "24h":
		sinceTime = now - 24*60*60
	case "7d":
		sinceTime = now - 7*24*60*60
	default:
		sinceTime = now - 30*24*60*60 // Default to 30 days
	}

	// Build query
	query := db.Get().Model(&Device{}).
		Where("token_last_used_at >= ?", sinceTime)

	// Get total count
	var total int64
	query.Count(&total)

	// Get paginated results with user info
	type deviceWithUser struct {
		Device
		UserEmail string
		UserUUID  string
	}

	var devices []deviceWithUser
	offset := (page - 1) * pageSize

	db.Get().Table("devices").
		Select("devices.*, users.uuid as user_uuid, login_identifies.index_id as user_email").
		Joins("LEFT JOIN users ON users.id = devices.user_id").
		Joins("LEFT JOIN login_identifies ON login_identifies.user_id = users.id AND login_identifies.type = 'email'").
		Where("devices.token_last_used_at >= ?", sinceTime).
		Order("devices.token_last_used_at DESC").
		Offset(offset).
		Limit(pageSize).
		Find(&devices)

	// Build response
	items := make([]ActiveDeviceItem, len(devices))
	for i, d := range devices {
		platform := d.AppPlatform
		if platform == "" {
			platform = "unknown"
		}
		version := d.AppVersion
		if version == "" {
			version = "unknown"
		}
		arch := d.AppArch
		if arch == "" {
			arch = "unknown"
		}

		items[i] = ActiveDeviceItem{
			UDID:            d.UDID,
			UserEmail:       d.UserEmail,
			UserUUID:        d.UserUUID,
			AppPlatform:     platform,
			AppVersion:      version,
			AppArch:         arch,
			OSVersion:       d.OSVersion,
			DeviceModel:     d.DeviceModel,
			TokenLastUsedAt: d.TokenLastUsedAt,
			CreatedAt:       d.CreatedAt.Unix(),
		}
	}

	result := ActiveDevicesResponse{
		Items: items,
		Pagination: Pagination{
			Page:     page,
			PageSize: pageSize,
			Total:    total,
		},
	}
	Success(c, &result)
}
