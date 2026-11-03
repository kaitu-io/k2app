package center

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wordgate/qtoolkit/db"
)

// 到手价走下单同一算法：$10 立减券作用在 $49 年付上 = $39。
// recallOfferPrice 是纯计算：用 nil db 也能跑，证明它不会像 applyCampaign 那样累加使用次数。
func TestRecallOfferPrice_UsesOrderAlgorithm(t *testing.T) {
	plan := &Plan{Price: 4900}
	coupon := &Campaign{Code: "X", Type: CampaignTypeCoupon, Value: 1000}
	assert.Equal(t, uint64(3900), recallOfferPrice(coupon, plan))
	pct := &Campaign{Code: "Y", Type: CampaignTypeDiscount, Value: 80}
	assert.Equal(t, uint64(3920), recallOfferPrice(pct, plan))
}

func TestRecallOfferText(t *testing.T) {
	assert.Equal(t, "1 年 $39（平时 $49），折合每月约 $3.3", recallOfferText(3900, 4900))
	assert.Equal(t, "1 年 $39.2（平时 $49），折合每月约 $3.3", recallOfferText(3920, 4900))
	assert.NotContains(t, recallOfferText(3900, 4900), "最高", "不写「最高立减」这种只有长套餐才拿得到的数")
}

func TestRecallValidUntil_UsesBeijingDate(t *testing.T) {
	// 2026-12-31 23:59:59 北京 = 15:59:59 UTC。
	end := time.Date(2026, 12, 31, 15, 59, 59, 0, time.UTC).Unix()
	assert.Equal(t, "12 月 31 日前有效", recallValidUntil(end))
}

func TestRecallPurchaseURL(t *testing.T) {
	u := recallPurchaseURL(BrandKaitu, "zh-CN", "a+b@example.com", "NEW10", "1y", "abandoned-3d")
	assert.True(t, strings.HasPrefix(u, BrandKaitu.Config().BaseURL+"/zh-CN/purchase?"), u)
	assert.Contains(t, u, "campaign=NEW10")
	assert.Contains(t, u, "plan=1y")
	assert.Contains(t, u, "utm_campaign=abandoned-3d")
	assert.True(t, strings.HasSuffix(u, "#email=a%2Bb%40example.com"), u)
	assert.NotContains(t, strings.SplitN(u, "#", 2)[0], "email=", "邮箱不得进 query")

	bare := recallPurchaseURL(BrandKaitu, "zh-CN", "", "", "", "abandoned-1h")
	assert.NotContains(t, bare, "?campaign=")
	assert.NotContains(t, bare, "&campaign=")
	assert.NotContains(t, bare, "#")
}

func TestInRecallHoldout_AboutTenPercent(t *testing.T) {
	n := 0
	for id := uint64(1); id <= 10000; id++ {
		if inRecallHoldout(id) {
			n++
		}
	}
	assert.InDelta(t, 1000, n, 150)
}

// 真 MySQL：recallMailer 的对照组记录、优惠缺失与无模板品牌的处理。
func TestRecallMailer_HoldoutAndOffer(t *testing.T) {
	skipIfNoConfig(t)
	require.NoError(t, Migrate())
	ctx := context.Background()
	uniq := fmt.Sprintf("%d", time.Now().UnixNano())

	mkTpl := func(slug string) {
		tpl := &EmailMarketingTemplate{Name: slug, Slug: slug, Language: "zh-CN", Subject: "s", Content: "c",
			IsActive: BoolPtr(true), Brand: string(BrandKaitu)}
		require.NoError(t, db.Get().Create(tpl).Error)
		t.Cleanup(func() { db.Get().Unscoped().Delete(tpl) })
	}
	offerSlug, plainSlug := "trm-3d-"+uniq, "trm-1d-"+uniq
	mkTpl(offerSlug)
	mkTpl(plainSlug)

	// 找一个对照组 id 和一个召回组 id（不入库：Holdout 只写日志行）。
	var hold, treat uint64
	for id := uint64(9_000_000_000); hold == 0 || treat == 0; id++ {
		if inRecallHoldout(id) {
			hold = id
		} else {
			treat = id
		}
	}
	batch := "abandoned:3d:test-" + uniq
	t.Cleanup(func() { db.Get().Where("batch_id = ?", batch).Delete(&EmailSendLog{}) })

	m := newRecallMailer(batch, offerSlug, "TRM"+uniq[len(uniq)-12:])
	holdUser := &User{ID: hold, Brand: "kaitu"}
	assert.True(t, m.Holdout(ctx, holdUser, "h@example.com"))
	assert.True(t, m.Holdout(ctx, holdUser, "h@example.com"), "重复运行仍判对照组")
	var rows []EmailSendLog
	require.NoError(t, db.Get().Where("batch_id = ?", batch).Find(&rows).Error)
	require.Len(t, rows, 1, "同一批次只落一行")
	assert.Equal(t, EmailSendLogStatusSkipped, rows[0].Status)
	require.NotNil(t, rows[0].ErrorMsg)
	assert.Equal(t, recallHoldoutReason, *rows[0].ErrorMsg)
	assert.NotZero(t, rows[0].TemplateID)
	assert.False(t, m.Holdout(ctx, &User{ID: treat, Brand: "kaitu"}, "t@example.com"))

	// 活动码不存在：带优惠的阶段不发（不发一封没有码的「优惠」邮件），并标记 OfferMissing。
	_, ok := m.Vars(ctx, &User{ID: treat, Brand: "kaitu"}, "t@example.com")
	assert.False(t, ok)
	assert.True(t, m.OfferMissing)

	// 该品牌没有这封信（Overleap）：不进对照组、不算优惠缺失、不发。
	o := newRecallMailer("abandoned:3d:test-o-"+uniq, offerSlug, "TRM"+uniq[len(uniq)-12:])
	assert.False(t, o.Holdout(ctx, &User{ID: hold, Brand: "overleap"}, "o@example.com"), "收不到信的人不能进对照组")
	_, ok = o.Vars(ctx, &User{ID: treat, Brand: "overleap"}, "o@example.com")
	assert.False(t, ok)
	assert.False(t, o.OfferMissing, "没有模板的品牌不该触发优惠缺失告警")

	// 只提醒的阶段：无码，照常有购买链接。
	plain := newRecallMailer("abandoned:1d:test-"+uniq, plainSlug, "")
	vars, ok := plain.Vars(ctx, &User{ID: treat, Brand: "kaitu", Language: "zh-CN"}, "t@example.com")
	require.True(t, ok)
	assert.Empty(t, vars["CampaignCode"])
	assert.Contains(t, vars["PurchaseURL"], "utm_campaign="+plainSlug)
	assert.False(t, plain.OfferMissing)
}
