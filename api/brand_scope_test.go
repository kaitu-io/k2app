package center

import (
	"testing"

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
