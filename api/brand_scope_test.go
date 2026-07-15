package center

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	db "github.com/wordgate/qtoolkit/db"
)

// 真 MySQL：plans 品牌隔离
func TestPlansBrandScoped(t *testing.T) {
	skipIfNoConfig(t)
	require.NoError(t, Migrate())

	pK := Plan{PID: "t-brand-k", Label: "k", Price: 100, Month: 1, Product: ProductApp, Brand: string(BrandKaitu), IsActive: BoolPtr(true)}
	pO := Plan{PID: "t-brand-o", Label: "o", Price: 900, Month: 1, Product: ProductApp, Brand: string(BrandOverleap), IsActive: BoolPtr(true)}
	require.NoError(t, db.Get().Create(&pK).Error)
	require.NoError(t, db.Get().Create(&pO).Error)
	t.Cleanup(func() {
		db.Get().Unscoped().Where("pid IN ?", []string{"t-brand-k", "t-brand-o"}).Delete(&Plan{})
	})

	var kaituPlans []Plan
	require.NoError(t, db.Get().Scopes(ScopeBrand(BrandKaitu)).Where("pid LIKE ?", "t-brand-%").Find(&kaituPlans).Error)
	require.Len(t, kaituPlans, 1)
	assert.Equal(t, "t-brand-k", kaituPlans[0].PID)

	var overleapPlans []Plan
	require.NoError(t, db.Get().Scopes(ScopeBrand(BrandOverleap)).Where("pid LIKE ?", "t-brand-%").Find(&overleapPlans).Error)
	require.Len(t, overleapPlans, 1)
	assert.Equal(t, "t-brand-o", overleapPlans[0].PID)
}

// 真 MySQL：授权码 (LicenseKey) 品牌隔离 —— overleap 用户不应能兑换 kaitu 品牌的授权码，反之亦然。
func TestLicenseKeyBrandScoped(t *testing.T) {
	skipIfNoConfig(t)
	require.NoError(t, Migrate())

	kK := LicenseKey{UUID: "t-lk-uuid-k", Code: "TBRANDK1", PlanDays: 30, ExpiresAt: 9999999999, Brand: string(BrandKaitu)}
	kO := LicenseKey{UUID: "t-lk-uuid-o", Code: "TBRANDO1", PlanDays: 30, ExpiresAt: 9999999999, Brand: string(BrandOverleap)}
	require.NoError(t, db.Get().Create(&kK).Error)
	require.NoError(t, db.Get().Create(&kO).Error)
	t.Cleanup(func() {
		db.Get().Unscoped().Where("code IN ?", []string{"TBRANDK1", "TBRANDO1"}).Delete(&LicenseKey{})
	})

	var kaituKeys []LicenseKey
	require.NoError(t, db.Get().Scopes(ScopeBrand(BrandKaitu)).Where("code IN ?", []string{"TBRANDK1", "TBRANDO1"}).Find(&kaituKeys).Error)
	require.Len(t, kaituKeys, 1)
	assert.Equal(t, "TBRANDK1", kaituKeys[0].Code)

	var overleapKeys []LicenseKey
	require.NoError(t, db.Get().Scopes(ScopeBrand(BrandOverleap)).Where("code IN ?", []string{"TBRANDK1", "TBRANDO1"}).Find(&overleapKeys).Error)
	require.Len(t, overleapKeys, 1)
	assert.Equal(t, "TBRANDO1", overleapKeys[0].Code)
}

// 真 MySQL：优惠活动 (Campaign) 品牌隔离
func TestCampaignBrandScoped(t *testing.T) {
	skipIfNoConfig(t)
	require.NoError(t, Migrate())

	cK := Campaign{Code: "TBRANDCK", Name: "k", Type: CampaignTypeDiscount, Value: 10, StartAt: 0, EndAt: 9999999999, MatcherType: "all", Brand: string(BrandKaitu), IsActive: BoolPtr(true)}
	cO := Campaign{Code: "TBRANDCO", Name: "o", Type: CampaignTypeDiscount, Value: 10, StartAt: 0, EndAt: 9999999999, MatcherType: "all", Brand: string(BrandOverleap), IsActive: BoolPtr(true)}
	require.NoError(t, db.Get().Create(&cK).Error)
	require.NoError(t, db.Get().Create(&cO).Error)
	t.Cleanup(func() {
		db.Get().Unscoped().Where("code IN ?", []string{"TBRANDCK", "TBRANDCO"}).Delete(&Campaign{})
	})

	got := getCampaignByCode(t.Context(), "TBRANDCK", BrandKaitu)
	require.NotNil(t, got)
	assert.Equal(t, "TBRANDCK", got.Code)

	// kaitu 品牌请求拿不到 overleap 的活动码
	assert.Nil(t, getCampaignByCode(t.Context(), "TBRANDCO", BrandKaitu))
	// overleap 品牌请求能拿到自己的
	got2 := getCampaignByCode(t.Context(), "TBRANDCO", BrandOverleap)
	require.NotNil(t, got2)
	assert.Equal(t, "TBRANDCO", got2.Code)
}

// 真 MySQL：公告 (Announcement) 品牌隔离
func TestAnnouncementsBrandScoped(t *testing.T) {
	skipIfNoConfig(t)
	require.NoError(t, Migrate())

	aK := Announcement{Message: "t-brand-k-msg", IsActive: BoolPtr(true), Brand: string(BrandKaitu)}
	aO := Announcement{Message: "t-brand-o-msg", IsActive: BoolPtr(true), Brand: string(BrandOverleap)}
	require.NoError(t, db.Get().Create(&aK).Error)
	require.NoError(t, db.Get().Create(&aO).Error)
	t.Cleanup(func() {
		db.Get().Unscoped().Where("message IN ?", []string{"t-brand-k-msg", "t-brand-o-msg"}).Delete(&Announcement{})
	})

	kaituList := getActiveAnnouncements(BrandKaitu, "")
	foundK, foundO := false, false
	for _, a := range kaituList {
		if a.Message == "t-brand-k-msg" {
			foundK = true
		}
		if a.Message == "t-brand-o-msg" {
			foundO = true
		}
	}
	assert.True(t, foundK, "kaitu brand should see its own announcement")
	assert.False(t, foundO, "kaitu brand should not see overleap announcement")

	overleapList := getActiveAnnouncements(BrandOverleap, "")
	foundK, foundO = false, false
	for _, a := range overleapList {
		if a.Message == "t-brand-k-msg" {
			foundK = true
		}
		if a.Message == "t-brand-o-msg" {
			foundO = true
		}
	}
	assert.False(t, foundK, "overleap brand should not see kaitu announcement")
	assert.True(t, foundO, "overleap brand should see its own announcement")
}

// ===================== Review-fix coverage (Task 5 round 2) =====================

// brandTestGinContext builds a gin test context whose Host resolves to the given brand.
func brandTestGinContext(t *testing.T, method, path, host string, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader([]byte{})
	} else {
		reader = bytes.NewReader(body)
	}
	c.Request = httptest.NewRequest(method, path, reader)
	c.Request.Host = host
	c.Request.Header.Set("Content-Type", "application/json")
	return c, w
}

// 真 MySQL：/api/tiers 公开端点品牌隔离（Critical fix：loadTiersWithPlans 曾无 ScopeBrand）
func TestTiersBrandScoped(t *testing.T) {
	skipIfNoConfig(t)
	require.NoError(t, Migrate())

	pK := Plan{PID: "t-tier-k", Tier: TierBasic, Label: "k", Price: 100, Month: 1, Product: ProductApp, Brand: string(BrandKaitu), IsActive: BoolPtr(true)}
	pO := Plan{PID: "t-tier-o", Tier: TierBasic, Label: "o", Price: 900, Month: 1, Product: ProductApp, Brand: string(BrandOverleap), IsActive: BoolPtr(true)}
	require.NoError(t, db.Get().Create(&pK).Error)
	require.NoError(t, db.Get().Create(&pO).Error)
	t.Cleanup(func() {
		db.Get().Unscoped().Where("pid IN ?", []string{"t-tier-k", "t-tier-o"}).Delete(&Plan{})
	})

	// kaitu 请求：包含 kaitu plan，绝不包含 overleap plan
	c, w := brandTestGinContext(t, http.MethodGet, "/api/tiers", "kaitu.io", nil)
	GetTiers(c)
	body := w.Body.String()
	assert.Contains(t, body, "t-tier-k", "kaitu /api/tiers should include kaitu plan")
	assert.NotContains(t, body, "t-tier-o", "kaitu /api/tiers must not leak overleap plan")

	// overleap 请求：反向
	c, w = brandTestGinContext(t, http.MethodGet, "/api/tiers", "overleap.io", nil)
	GetTiers(c)
	body = w.Body.String()
	assert.Contains(t, body, "t-tier-o", "overleap /api/tiers should include overleap plan")
	assert.NotContains(t, body, "t-tier-k", "overleap /api/tiers must not leak kaitu plan")

	// admin 端点保持跨品牌：两个 plan 都可见
	c, w = brandTestGinContext(t, http.MethodGet, "/app/tiers", "kaitu.io", nil)
	GetAdminTiers(c)
	body = w.Body.String()
	assert.Contains(t, body, "t-tier-k")
	assert.Contains(t, body, "t-tier-o")
}

// 真 MySQL：winback 价格区间按品牌隔离（Important fix：getPlanPriceRange 曾查全部 active plan）
func TestWinbackPlanPriceRangeBrandScoped(t *testing.T) {
	skipIfNoConfig(t)
	require.NoError(t, Migrate())

	// 极端价格保证断言不受 dev DB 存量数据影响：
	// kaitu 侧造一个全库最低价 1，overleap 侧造一个全库最高价 999999999。
	pK := Plan{PID: "t-wb-k", Label: "k", Price: 1, Month: 1, Product: ProductApp, Brand: string(BrandKaitu), IsActive: BoolPtr(true)}
	pO := Plan{PID: "t-wb-o", Label: "o", Price: 999999999, Month: 1, Product: ProductApp, Brand: string(BrandOverleap), IsActive: BoolPtr(true)}
	require.NoError(t, db.Get().Create(&pK).Error)
	require.NoError(t, db.Get().Create(&pO).Error)
	t.Cleanup(func() {
		db.Get().Unscoped().Where("pid IN ?", []string{"t-wb-k", "t-wb-o"}).Delete(&Plan{})
	})

	kMin, kMax, ok := getPlanPriceRange(t.Context(), BrandKaitu)
	require.True(t, ok)
	assert.Equal(t, uint64(1), kMin, "kaitu range should include the kaitu test plan")
	assert.NotEqual(t, uint64(999999999), kMax, "kaitu range must not include overleap plan price")

	oMin, oMax, ok := getPlanPriceRange(t.Context(), BrandOverleap)
	require.True(t, ok)
	assert.Equal(t, uint64(999999999), oMax, "overleap range should include the overleap test plan")
	assert.NotEqual(t, uint64(1), oMin, "overleap range must not include kaitu plan price")
}

// 真 MySQL：winback/abandoned 活动码变量按收件品牌计算。
// overleap-only 活动码：overleap 用户拿到完整 vars（价格区间来自 overleap plans），
// kaitu 用户拿到空 vars（活动在 kaitu 品牌下不存在）——绝不跨品牌泄漏。
func TestCampaignVarsForBrandScoped(t *testing.T) {
	skipIfNoConfig(t)
	require.NoError(t, Migrate())

	now := time.Now().Unix()
	campO := Campaign{Code: "TBRANDWB", Name: "o-wb", Type: CampaignTypeDiscount, Value: 90,
		StartAt: now - 3600, EndAt: now + 3600, MatcherType: "all",
		Brand: string(BrandOverleap), IsActive: BoolPtr(true)}
	require.NoError(t, db.Get().Create(&campO).Error)
	pO := Plan{PID: "t-wbv-o", Label: "o", Price: 1000, Month: 1, Product: ProductApp, Brand: string(BrandOverleap), IsActive: BoolPtr(true)}
	require.NoError(t, db.Get().Create(&pO).Error)
	t.Cleanup(func() {
		db.Get().Unscoped().Where("code = ?", "TBRANDWB").Delete(&Campaign{})
		db.Get().Unscoped().Where("pid = ?", "t-wbv-o").Delete(&Plan{})
	})

	oVars := campaignVarsForBrand(t.Context(), "TBRANDWB", 90, "14 天内有效", BrandOverleap)
	assert.Equal(t, "TBRANDWB", oVars["CampaignCode"], "overleap recipient should get the overleap campaign code")
	assert.NotEmpty(t, oVars["SavingsText"])

	kVars := campaignVarsForBrand(t.Context(), "TBRANDWB", 90, "14 天内有效", BrandKaitu)
	assert.Empty(t, kVars, "kaitu recipient must not receive an overleap-only campaign")
}

// BrandForCreate：空→kaitu；合法值（大小写不敏感）→对应品牌；非法值→error。
func TestBrandForCreate(t *testing.T) {
	b, err := BrandForCreate("")
	require.NoError(t, err)
	assert.Equal(t, BrandKaitu, b)

	b, err = BrandForCreate("overleap")
	require.NoError(t, err)
	assert.Equal(t, BrandOverleap, b)

	b, err = BrandForCreate("OVERLEAP")
	require.NoError(t, err)
	assert.Equal(t, BrandOverleap, b)

	_, err = BrandForCreate("nonsense")
	assert.Error(t, err)
}

// decodeResponseCode 从 handler 响应体解析业务 code。
func decodeResponseCode(t *testing.T, w *httptest.ResponseRecorder) ErrorCode {
	t.Helper()
	var resp struct {
		Code ErrorCode `json:"code"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp.Code
}

// admin create 端点：非法 brand 必须拒绝（ErrorInvalidArgument），不静默转 kaitu。
// 四个创建路径逐一断言。所有 handler 均在触达 DB 之前完成 brand 校验。
func TestAdminCreateRejectsInvalidBrand(t *testing.T) {
	testInitConfig()

	// 1. Plan
	planBody, _ := json.Marshal(map[string]any{
		"pid": "t-badbrand-p", "label": "x", "price": 100, "originPrice": 100, "month": 1,
		"brand": "nonsense",
	})
	c, w := brandTestGinContext(t, http.MethodPost, "/app/plans", "kaitu.io", planBody)
	api_admin_create_plan(c)
	assert.Equal(t, ErrorInvalidArgument, decodeResponseCode(t, w), "plan create must reject invalid brand")

	// 2. Announcement
	annBody, _ := json.Marshal(map[string]any{
		"message": "t-badbrand-msg", "brand": "nonsense",
	})
	c, w = brandTestGinContext(t, http.MethodPost, "/app/announcements", "kaitu.io", annBody)
	api_admin_create_announcement(c)
	assert.Equal(t, ErrorInvalidArgument, decodeResponseCode(t, w), "announcement create must reject invalid brand")

	// 3. Campaign（handler 层在提交审批前拒绝）
	now := time.Now().Unix()
	campBody, _ := json.Marshal(map[string]any{
		"code": "TBADBRAND", "name": "x", "type": CampaignTypeDiscount, "value": 90,
		"startAt": now, "endAt": now + 3600, "matcherType": "all",
		"brand": "nonsense",
	})
	c, w = brandTestGinContext(t, http.MethodPost, "/app/campaigns", "kaitu.io", campBody)
	api_admin_create_campaign(c)
	assert.Equal(t, ErrorInvalidArgument, decodeResponseCode(t, w), "campaign create must reject invalid brand")

	// 4. LicenseKeyBatch（handler 层在提交审批前拒绝）
	batchBody, _ := json.Marshal(map[string]any{
		"name": "t-badbrand-b", "recipientMatcher": "all", "planDays": 30, "quantity": 1, "expiresInDays": 30,
		"brand": "nonsense",
	})
	c, w = brandTestGinContext(t, http.MethodPost, "/app/license-key-batches", "kaitu.io", batchBody)
	api_admin_create_license_key_batch(c)
	assert.Equal(t, ErrorInvalidArgument, decodeResponseCode(t, w), "license key batch create must reject invalid brand")
}

// 审批 callback / logic 层兜底：非法 brand 返回 error（这两层无 gin context）。
func TestLogicLayerRejectsInvalidBrand(t *testing.T) {
	skipIfNoConfig(t)
	require.NoError(t, Migrate())

	// campaign approval callback
	now := time.Now().Unix()
	params, _ := json.Marshal(CampaignRequest{
		Code: "TBADBRAND2", Name: "x", Type: CampaignTypeDiscount, Value: 90,
		StartAt: now, EndAt: now + 3600, MatcherType: "all", Brand: "nonsense",
	})
	err := executeApprovalCampaignCreate(t.Context(), params)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid brand")
	// 确认没有落库
	var count int64
	db.Get().Model(&Campaign{}).Where("code = ?", "TBADBRAND2").Count(&count)
	assert.Equal(t, int64(0), count)

	// license key batch logic
	_, err = CreateLicenseKeyBatch(t.Context(), &CreateLicenseKeyBatchRequest{
		Name: "t-badbrand-b2", RecipientMatcher: "all", PlanDays: 30, Quantity: 1, ExpiresInDays: 30,
		Brand: "nonsense",
	}, 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid brand")
}
