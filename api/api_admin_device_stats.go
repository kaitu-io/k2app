package center

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"gorm.io/gorm"
)

// statsAccumulator threads the first DB error across a sequence of queries.
// Each helper short-circuits once err is set, so callers can chain queries
// and check err once at the end instead of guarding every call site.
type statsAccumulator struct {
	err error
}

func (s *statsAccumulator) count(q *gorm.DB, out *int64) {
	if s.err != nil {
		return
	}
	if e := q.Count(out).Error; e != nil {
		s.err = e
	}
}

func (s *statsAccumulator) find(q *gorm.DB, out any) {
	if s.err != nil {
		return
	}
	if e := q.Find(out).Error; e != nil {
		s.err = e
	}
}

// api_admin_get_device_statistics returns aggregated device statistics.
// App devices (is_gateway=false) and routers (is_gateway=true) are reported in
// mutually exclusive buckets: Desktop+Mobile+Unknown+Router == TotalDevices.
// GET /app/devices/statistics
func api_admin_get_device_statistics(c *gin.Context) {
	var result DeviceStatisticsResponse
	var s statsAccumulator

	// Grand total
	s.count(db.Get().Model(&Device{}), &result.TotalDevices)

	// Router count
	s.count(db.Get().Model(&Device{}).Where("is_gateway = ?", true), &result.RouterDevices)

	// App-device aggregations
	collectPlatformCounts(&s, false, &result.ByPlatform, &result.DesktopDevices, &result.MobileDevices, &result.UnknownDevices)
	collectVersionCounts(&s, false, &result.ByVersion)
	collectArchCounts(&s, false, &result.ByArch)
	collectOSVersionCounts(&s, false, &result.ByOSVersion)
	collectDeviceModelCounts(&s, false, &result.ByDeviceModel)

	// Router-device aggregations (desktop/mobile/unknown counters discarded — routers don't fit those buckets)
	var routerDesktopUnused, routerMobileUnused, routerUnknownUnused int64
	collectPlatformCounts(&s, true, &result.RouterByPlatform, &routerDesktopUnused, &routerMobileUnused, &routerUnknownUnused)
	collectVersionCounts(&s, true, &result.RouterByVersion)
	collectArchCounts(&s, true, &result.RouterByArch)
	collectOSVersionCounts(&s, true, &result.RouterByOSVersion)
	collectDeviceModelCounts(&s, true, &result.RouterByDeviceModel)

	// Active counts (split by gateway flag)
	now := time.Now().Unix()
	h24Ago := now - 24*60*60
	d7Ago := now - 7*24*60*60
	d30Ago := now - 30*24*60*60

	s.count(db.Get().Model(&Device{}).Where("is_gateway = ? AND token_last_used_at >= ?", false, h24Ago), &result.Active24h)
	s.count(db.Get().Model(&Device{}).Where("is_gateway = ? AND token_last_used_at >= ?", false, d7Ago), &result.Active7d)
	s.count(db.Get().Model(&Device{}).Where("is_gateway = ? AND token_last_used_at >= ?", false, d30Ago), &result.Active30d)

	s.count(db.Get().Model(&Device{}).Where("is_gateway = ? AND token_last_used_at >= ?", true, h24Ago), &result.ActiveRouter24h)
	s.count(db.Get().Model(&Device{}).Where("is_gateway = ? AND token_last_used_at >= ?", true, d7Ago), &result.ActiveRouter7d)
	s.count(db.Get().Model(&Device{}).Where("is_gateway = ? AND token_last_used_at >= ?", true, d30Ago), &result.ActiveRouter30d)

	if s.err != nil {
		Error(c, ErrorSystemError, "device statistics query failed: "+s.err.Error())
		return
	}

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
func collectPlatformCounts(s *statsAccumulator, isRouter bool, out *[]PlatformCount, desktop, mobile, unknown *int64) {
	type row struct {
		Platform string
		Count    int64
	}
	var rows []row
	s.find(db.Get().Model(&Device{}).
		Select("COALESCE(NULLIF(app_platform, ''), 'unknown') as platform, COUNT(*) as count").
		Where("is_gateway = ?", isRouter).
		Group("COALESCE(NULLIF(app_platform, ''), 'unknown')").
		Order("count DESC"), &rows)

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

func collectVersionCounts(s *statsAccumulator, isRouter bool, out *[]VersionCount) {
	type row struct {
		Version string
		Count   int64
	}
	var rows []row
	s.find(db.Get().Model(&Device{}).
		Select("COALESCE(NULLIF(app_version, ''), 'unknown') as version, COUNT(*) as count").
		Where("is_gateway = ?", isRouter).
		Group("COALESCE(NULLIF(app_version, ''), 'unknown')").
		Order("count DESC").
		Limit(10), &rows)
	for _, r := range rows {
		*out = append(*out, VersionCount{Version: r.Version, Count: r.Count})
	}
}

func collectArchCounts(s *statsAccumulator, isRouter bool, out *[]ArchCount) {
	type row struct {
		Arch  string
		Count int64
	}
	var rows []row
	s.find(db.Get().Model(&Device{}).
		Select("COALESCE(NULLIF(app_arch, ''), 'unknown') as arch, COUNT(*) as count").
		Where("is_gateway = ?", isRouter).
		Group("COALESCE(NULLIF(app_arch, ''), 'unknown')").
		Order("count DESC"), &rows)
	for _, r := range rows {
		*out = append(*out, ArchCount{Arch: r.Arch, Count: r.Count})
	}
}

func collectOSVersionCounts(s *statsAccumulator, isRouter bool, out *[]OSVersionCount) {
	type row struct {
		OSVersion string
		Count     int64
	}
	var rows []row
	s.find(db.Get().Model(&Device{}).
		Select("COALESCE(NULLIF(os_version, ''), 'unknown') as os_version, COUNT(*) as count").
		Where("is_gateway = ?", isRouter).
		Group("COALESCE(NULLIF(os_version, ''), 'unknown')").
		Order("count DESC").
		Limit(10), &rows)
	for _, r := range rows {
		*out = append(*out, OSVersionCount{OSVersion: r.OSVersion, Count: r.Count})
	}
}

func collectDeviceModelCounts(s *statsAccumulator, isRouter bool, out *[]DeviceModelCount) {
	type row struct {
		DeviceModel string
		Count       int64
	}
	var rows []row
	s.find(db.Get().Model(&Device{}).
		Select("COALESCE(NULLIF(device_model, ''), 'unknown') as device_model, COUNT(*) as count").
		Where("is_gateway = ?", isRouter).
		Group("COALESCE(NULLIF(device_model, ''), 'unknown')").
		Order("count DESC").
		Limit(10), &rows)
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

	var s statsAccumulator

	// Count query: model-level, no joins.
	countQ := db.Get().Model(&Device{}).Where("token_last_used_at >= ?", sinceTime)
	switch deviceType {
	case "app":
		countQ = countQ.Where("is_gateway = ?", false)
	case "router":
		countQ = countQ.Where("is_gateway = ?", true)
	}
	var total int64
	s.count(countQ, &total)

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
	s.find(listQ.Order("devices.token_last_used_at DESC").Offset(offset).Limit(pageSize), &devices)

	if s.err != nil {
		Error(c, ErrorSystemError, "active devices query failed: "+s.err.Error())
		return
	}

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
