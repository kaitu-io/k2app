package center

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
)

// api_admin_get_device_statistics returns aggregated device statistics.
// App devices (is_gateway=false) and routers (is_gateway=true) are reported in
// mutually exclusive buckets: Desktop+Mobile+Unknown+Router == TotalDevices.
// GET /app/devices/statistics
func api_admin_get_device_statistics(c *gin.Context) {
	var result DeviceStatisticsResponse

	// Grand total
	db.Get().Model(&Device{}).Count(&result.TotalDevices)

	// Router count
	db.Get().Model(&Device{}).Where("is_gateway = ?", true).Count(&result.RouterDevices)

	// App-device aggregations
	collectPlatformCounts(false, &result.ByPlatform, &result.DesktopDevices, &result.MobileDevices, &result.UnknownDevices)
	collectVersionCounts(false, &result.ByVersion)
	collectArchCounts(false, &result.ByArch)
	collectOSVersionCounts(false, &result.ByOSVersion)
	collectDeviceModelCounts(false, &result.ByDeviceModel)

	// Router-device aggregations (desktop/mobile/unknown counters discarded — routers don't fit those buckets)
	var routerDesktopUnused, routerMobileUnused, routerUnknownUnused int64
	collectPlatformCounts(true, &result.RouterByPlatform, &routerDesktopUnused, &routerMobileUnused, &routerUnknownUnused)
	collectVersionCounts(true, &result.RouterByVersion)
	collectArchCounts(true, &result.RouterByArch)
	collectOSVersionCounts(true, &result.RouterByOSVersion)
	collectDeviceModelCounts(true, &result.RouterByDeviceModel)

	// Active counts (split by gateway flag)
	now := time.Now().Unix()
	h24Ago := now - 24*60*60
	d7Ago := now - 7*24*60*60
	d30Ago := now - 30*24*60*60

	db.Get().Model(&Device{}).Where("is_gateway = ? AND token_last_used_at >= ?", false, h24Ago).Count(&result.Active24h)
	db.Get().Model(&Device{}).Where("is_gateway = ? AND token_last_used_at >= ?", false, d7Ago).Count(&result.Active7d)
	db.Get().Model(&Device{}).Where("is_gateway = ? AND token_last_used_at >= ?", false, d30Ago).Count(&result.Active30d)

	db.Get().Model(&Device{}).Where("is_gateway = ? AND token_last_used_at >= ?", true, h24Ago).Count(&result.ActiveRouter24h)
	db.Get().Model(&Device{}).Where("is_gateway = ? AND token_last_used_at >= ?", true, d7Ago).Count(&result.ActiveRouter7d)
	db.Get().Model(&Device{}).Where("is_gateway = ? AND token_last_used_at >= ?", true, d30Ago).Count(&result.ActiveRouter30d)

	// Ensure non-nil slices for stable JSON output
	if result.ByPlatform == nil {
		result.ByPlatform = []PlatformCount{}
	}
	if result.ByVersion == nil {
		result.ByVersion = []VersionCount{}
	}
	if result.ByArch == nil {
		result.ByArch = []ArchCount{}
	}
	if result.ByOSVersion == nil {
		result.ByOSVersion = []OSVersionCount{}
	}
	if result.ByDeviceModel == nil {
		result.ByDeviceModel = []DeviceModelCount{}
	}
	if result.RouterByPlatform == nil {
		result.RouterByPlatform = []PlatformCount{}
	}
	if result.RouterByVersion == nil {
		result.RouterByVersion = []VersionCount{}
	}
	if result.RouterByArch == nil {
		result.RouterByArch = []ArchCount{}
	}
	if result.RouterByOSVersion == nil {
		result.RouterByOSVersion = []OSVersionCount{}
	}
	if result.RouterByDeviceModel == nil {
		result.RouterByDeviceModel = []DeviceModelCount{}
	}

	Success(c, &result)
}

// collectPlatformCounts groups devices by app_platform for is_gateway = isRouter,
// appending into out and incrementing the desktop/mobile/unknown summary counters.
func collectPlatformCounts(isRouter bool, out *[]PlatformCount, desktop, mobile, unknown *int64) {
	type row struct {
		Platform string
		Count    int64
	}
	var rows []row
	db.Get().Model(&Device{}).
		Select("COALESCE(NULLIF(app_platform, ''), 'unknown') as platform, COUNT(*) as count").
		Where("is_gateway = ?", isRouter).
		Group("COALESCE(NULLIF(app_platform, ''), 'unknown')").
		Order("count DESC").
		Find(&rows)

	desktopPlatforms := map[string]bool{"darwin": true, "windows": true, "linux": true}
	mobilePlatforms := map[string]bool{"ios": true, "android": true}

	for _, r := range rows {
		*out = append(*out, PlatformCount{Platform: r.Platform, Count: r.Count})
		switch {
		case r.Platform == "unknown":
			*unknown += r.Count
		case desktopPlatforms[r.Platform]:
			*desktop += r.Count
		case mobilePlatforms[r.Platform]:
			*mobile += r.Count
		default:
			// Treat any unrecognised non-empty platform as unknown for summary tally
			*unknown += r.Count
		}
	}
}

func collectVersionCounts(isRouter bool, out *[]VersionCount) {
	type row struct {
		Version string
		Count   int64
	}
	var rows []row
	db.Get().Model(&Device{}).
		Select("COALESCE(NULLIF(app_version, ''), 'unknown') as version, COUNT(*) as count").
		Where("is_gateway = ?", isRouter).
		Group("COALESCE(NULLIF(app_version, ''), 'unknown')").
		Order("count DESC").
		Limit(10).
		Find(&rows)
	for _, r := range rows {
		*out = append(*out, VersionCount{Version: r.Version, Count: r.Count})
	}
}

func collectArchCounts(isRouter bool, out *[]ArchCount) {
	type row struct {
		Arch  string
		Count int64
	}
	var rows []row
	db.Get().Model(&Device{}).
		Select("COALESCE(NULLIF(app_arch, ''), 'unknown') as arch, COUNT(*) as count").
		Where("is_gateway = ?", isRouter).
		Group("COALESCE(NULLIF(app_arch, ''), 'unknown')").
		Order("count DESC").
		Find(&rows)
	for _, r := range rows {
		*out = append(*out, ArchCount{Arch: r.Arch, Count: r.Count})
	}
}

func collectOSVersionCounts(isRouter bool, out *[]OSVersionCount) {
	type row struct {
		OSVersion string
		Count     int64
	}
	var rows []row
	db.Get().Model(&Device{}).
		Select("COALESCE(NULLIF(os_version, ''), 'unknown') as os_version, COUNT(*) as count").
		Where("is_gateway = ?", isRouter).
		Group("COALESCE(NULLIF(os_version, ''), 'unknown')").
		Order("count DESC").
		Limit(10).
		Find(&rows)
	for _, r := range rows {
		*out = append(*out, OSVersionCount{OSVersion: r.OSVersion, Count: r.Count})
	}
}

func collectDeviceModelCounts(isRouter bool, out *[]DeviceModelCount) {
	type row struct {
		DeviceModel string
		Count       int64
	}
	var rows []row
	db.Get().Model(&Device{}).
		Select("COALESCE(NULLIF(device_model, ''), 'unknown') as device_model, COUNT(*) as count").
		Where("is_gateway = ?", isRouter).
		Group("COALESCE(NULLIF(device_model, ''), 'unknown')").
		Order("count DESC").
		Limit(10).
		Find(&rows)
	for _, r := range rows {
		*out = append(*out, DeviceModelCount{DeviceModel: r.DeviceModel, Count: r.Count})
	}
}

// api_admin_get_active_devices returns paginated list of active devices.
// Query params: page, pageSize, period (24h|7d|30d), type (app|router|all, default all)
// GET /app/devices/active
func api_admin_get_active_devices(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	period := c.Query("period")
	deviceType := c.DefaultQuery("type", "all")

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	now := time.Now().Unix()
	var sinceTime int64
	switch period {
	case "24h":
		sinceTime = now - 24*60*60
	case "7d":
		sinceTime = now - 7*24*60*60
	default:
		sinceTime = now - 30*24*60*60
	}

	type deviceWithUser struct {
		Device
		UserEmail string
		UserUUID  string
	}

	// Count query: model-level, no joins.
	countQ := db.Get().Model(&Device{}).Where("token_last_used_at >= ?", sinceTime)
	switch deviceType {
	case "app":
		countQ = countQ.Where("is_gateway = ?", false)
	case "router":
		countQ = countQ.Where("is_gateway = ?", true)
	}
	var total int64
	countQ.Count(&total)

	// List query: joins to surface email + uuid.
	listQ := db.Get().Table("devices").
		Select("devices.*, users.uuid as user_uuid, login_identifies.index_id as user_email").
		Joins("LEFT JOIN users ON users.id = devices.user_id").
		Joins("LEFT JOIN login_identifies ON login_identifies.user_id = users.id AND login_identifies.type = 'email'").
		Where("devices.token_last_used_at >= ?", sinceTime)
	switch deviceType {
	case "app":
		listQ = listQ.Where("devices.is_gateway = ?", false)
	case "router":
		listQ = listQ.Where("devices.is_gateway = ?", true)
	}

	var devices []deviceWithUser
	offset := (page - 1) * pageSize
	listQ.Order("devices.token_last_used_at DESC").Offset(offset).Limit(pageSize).Find(&devices)

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
			IsGateway:       d.IsGateway,
			TokenLastUsedAt: d.TokenLastUsedAt,
			CreatedAt:       d.CreatedAt.Unix(),
		}
	}

	Success(c, &ActiveDevicesResponse{
		Items: items,
		Pagination: Pagination{
			Page:     page,
			PageSize: pageSize,
			Total:    total,
		},
	})
}
