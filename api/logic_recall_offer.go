package center

import (
	"context"
	"fmt"
	"hash/fnv"
	"maps"
	"net/url"
	"strconv"
	"time"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

// 召回优惠（spec docs/superpowers/specs/2026-11-03-recall-offer-flatten-design.md）。
//
// 每类人只有一个优惠，所有阶段都给同一个，**不随时间加深**：逐步加深的折扣会教会用户"再等等更便宜"，
// 4~11 月数据也显示付款几乎全来自不带折扣的前两封提醒，95 折 / 9 折 / 85 折几乎没人用。
// 新客（从没付过费：试用召回、未付款召回）与老客（过期召回）力度相同，免得老客为了折扣故意断档。
const (
	recallOfferNewCode  = "NEW10"  // coupon $10，first_order（新客）
	recallOfferBackCode = "BACK10" // coupon $10，vip（老客）
)

// inRecallHoldout 召回对照组（10%）：按用户确定性分组、终身不变，三条召回链路共用一套分组。
// 盐沿用试用召回上线时的 "trial-recall-v1"——改盐会把已在跑的试用召回实验整体重新洗牌。
func inRecallHoldout(userID uint64) bool {
	h := fnv.New32a()
	_, _ = h.Write([]byte("trial-recall-v1:" + strconv.FormatUint(userID, 10)))
	return h.Sum32()%10 == 0
}

// recallHoldoutReason 对照组 skipped 日志行的 error_msg，recall_stats 据此识别对照组。
const recallHoldoutReason = "holdout"

// recallOfferPlan 品牌在售的 12 个月 App 套餐（召回邮件只推年付这一档）；没有返回 nil。
func recallOfferPlan(ctx context.Context, brand Brand) *Plan {
	var plans []Plan
	if err := db.Get().WithContext(ctx).Scopes(ScopeBrand(brand)).
		Where("is_active = ? AND product = ? AND month = ?", true, ProductApp, 12).
		Order("id ASC").Find(&plans).Error; err != nil {
		log.Errorf(ctx, "[RECALL-OFFER] query plans failed: %v", err)
		return nil
	}
	if len(plans) == 0 {
		return nil
	}
	return &plans[0]
}

// recallOfferPrice 该活动码作用在套餐上的到手价（美分），与下单同一算法（campaignPrice）。
// 不能调 applyCampaign：它会累加活动使用次数，每发一封邮件就虚增一次用量。
func recallOfferPrice(campaign *Campaign, plan *Plan) uint64 {
	price, _ := campaignPrice(campaign, plan.Price)
	return price
}

// recallOfferText 具体价格文案，如「1 年 $39（平时 $49），折合每月约 $3.3」。
// 锚点用用户实际见过的售价（plan.Price），不用划线价——召回对象多疑，锚点要经得起核对。
func recallOfferText(offerPrice, regularPrice uint64) string {
	perMonth := (offerPrice + 60) / 120 * 10 // 每月价，四舍五入到 $0.1
	return fmt.Sprintf("1 年 %s（平时 %s），折合每月约 %s", formatCents(offerPrice), formatCents(regularPrice), formatCents(perMonth))
}

// recallValidUntil 真实期限 = 活动码自己的截止日（北京时间），如「12 月 31 日前有效」。
// 不写「本周内有效」「7 天内有效」这类码本身并不执行的期限。
func recallValidUntil(endAt int64) string {
	t := time.Unix(endAt, 0).In(trialRecallLocation())
	return fmt.Sprintf("%d 月 %d 日前有效", int(t.Month()), t.Day())
}

// recallPurchaseURL 官网购买页：预填活动码与年付套餐，带 utm；邮箱放 fragment（不进 GA4 / 访问日志）。
func recallPurchaseURL(brand Brand, locale, email, code, planPID, utmCampaign string) string {
	q := url.Values{}
	if code != "" {
		q.Set("campaign", code)
	}
	if planPID != "" {
		q.Set("plan", planPID)
	}
	q.Set("utm_source", "email")
	q.Set("utm_medium", "lifecycle")
	q.Set("utm_campaign", utmCampaign)
	u := brand.Config().BaseURL + "/" + locale + "/purchase?" + q.Encode()
	if email != "" {
		frag := url.Values{}
		frag.Set("email", email)
		u += "#" + frag.Encode()
	}
	return u
}

// recallOffer 一个品牌下某活动码的邮件变量（不含逐人的 PurchaseURL）。
type recallOffer struct {
	Code    string
	PlanPID string
	Vars    map[string]string
}

// loadRecallOffer 活动码在该品牌可用且有在售年付 App 套餐时返回优惠；否则 nil（调用方不应发带码的邮件）。
//
// 变量：CampaignCode、OfferText（具体价格）、OfferPrice、RegularPrice、ValidDays（活动码真实截止日）。
// SavingsText 与 OfferText 相同，只为兼容还没改的旧模板。
func loadRecallOffer(ctx context.Context, code string, brand Brand) *recallOffer {
	campaign := getCampaignByCode(ctx, code, brand)
	if campaign == nil {
		return nil
	}
	plan := recallOfferPlan(ctx, brand)
	if plan == nil {
		return nil
	}
	offer := recallOfferPrice(campaign, plan)
	text := recallOfferText(offer, plan.Price)
	return &recallOffer{
		Code:    code,
		PlanPID: plan.PID,
		Vars: map[string]string{
			"CampaignCode": code,
			"OfferText":    text,
			"SavingsText":  text,
			"OfferPrice":   formatCents(offer),
			"RegularPrice": formatCents(plan.Price),
			"ValidDays":    recallValidUntil(campaign.EndAt),
		},
	}
}

// recallMailer 未付款召回 / 过期召回的逐人决策：对照组、优惠变量、购买链接。一个实例对应一次阶段发送。
type recallMailer struct {
	batchID   string
	slug      string // 也用作 utm_campaign
	offerCode string // 空 = 本阶段不带优惠（只提醒）

	offers    map[Brand]*recallOffer
	loaded    map[Brand]bool
	templates map[Brand]uint64 // 该品牌此 slug 的源模板 ID，0 = 该品牌没有这封信
	// OfferMissing 本阶段带优惠、但某品牌的活动码或年付套餐不可用（这些人本次不发）。
	OfferMissing bool
}

func newRecallMailer(batchID, slug, offerCode string) *recallMailer {
	return &recallMailer{batchID: batchID, slug: slug, offerCode: offerCode,
		offers: map[Brand]*recallOffer{}, loaded: map[Brand]bool{}, templates: map[Brand]uint64{}}
}

func recallBrand(user *User) Brand {
	if b := Brand(user.Brand); b.Valid() {
		return b
	}
	return BrandKaitu
}

// templateFor 该品牌是否有这封信（缓存）。没有模板的品牌（如 Overleap 没有召回邮件）的用户
// 本来就收不到信：既不进对照组（否则对照组混进"本来也收不到"的人），也不算优惠缺失（否则天天误报）。
func (m *recallMailer) templateFor(b Brand) uint64 {
	id, ok := m.templates[b]
	if !ok {
		id = recallTemplateID(m.slug, b)
		m.templates[b] = id
	}
	return id
}

func (m *recallMailer) offerFor(ctx context.Context, b Brand) *recallOffer {
	if !m.loaded[b] {
		m.loaded[b] = true
		m.offers[b] = loadRecallOffer(ctx, m.offerCode, b)
	}
	return m.offers[b]
}

// Vars 该用户的模板变量；false = 本阶段应带优惠但优惠不可用——宁可不发，也不发一封没有码的「优惠」邮件。
func (m *recallMailer) Vars(ctx context.Context, user *User, email string) (map[string]string, bool) {
	b := recallBrand(user)
	if m.templateFor(b) == 0 {
		return nil, false
	}
	vars := map[string]string{}
	code, planPID := "", ""
	if m.offerCode != "" {
		offer := m.offerFor(ctx, b)
		if offer == nil {
			m.OfferMissing = true
			return nil, false
		}
		maps.Copy(vars, offer.Vars)
		code, planPID = offer.Code, offer.PlanPID
	}
	// 购买链接只给开途：官网预填参数与 locale 规则是开途官网的，Overleap 模板不用这个变量。
	if b == BrandKaitu {
		vars["PurchaseURL"] = recallPurchaseURL(b, trialRecallLocale(user.Language), email, code, planPID, m.slug)
	}
	return vars, true
}

// Holdout 对照组用户不发信，但落一条 skipped（error_msg=holdout）记录，recall_stats 用它算对照组转化。
// 同一批次重复运行不重复落（(batch, template, user) 有唯一索引）。
func (m *recallMailer) Holdout(ctx context.Context, user *User, email string) bool {
	if !inRecallHoldoutForEmail(user) || m.templateFor(recallBrand(user)) == 0 {
		return false
	}
	var n int64
	db.Get().Model(&EmailSendLog{}).Where("batch_id = ? AND user_id = ?", m.batchID, user.ID).Count(&n)
	if n == 0 {
		createEmailSendLog(ctx, m.batchID, m.templateFor(recallBrand(user)), user.ID, email, "",
			EmailSendLogStatusSkipped, recallHoldoutReason)
	}
	return true
}

// recallTemplateID 某品牌下该 slug 的启用源模板 ID；查不到返回 0。
func recallTemplateID(slug string, b Brand) uint64 {
	if !b.Valid() {
		b = BrandKaitu
	}
	var tpl EmailMarketingTemplate
	if err := db.Get().Where("slug = ? AND brand = ? AND is_active = ? AND origin_id IS NULL", slug, string(b), true).
		Order("id ASC").First(&tpl).Error; err != nil {
		return 0
	}
	return tpl.ID
}
