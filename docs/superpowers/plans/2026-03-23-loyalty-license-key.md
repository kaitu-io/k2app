# 老用户回馈 + 老带新授权码系统 — 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 Campaign 新增 `paid_before` 时间维度 matcher，并实现独立的 LicenseKey 授权码系统，支持老用户回馈自用折扣 + 可分享送礼链接（老带新）。

**Architecture:** Campaign 扩展三个字段（`matcherParams`, `isShareable`, `sharesPerUser`）支持时间定向；新增独立 `LicenseKey` 表（inspired by wgcenter），软关联 Campaign，通过 UUID 唯一码分发到老用户，新用户通过 `/redeem/{uuid}` 落地页兑换。

**Tech Stack:** Go + Gin + GORM（后端），Next.js 15 + shadcn/ui（前端），SES（邮件），xid（UUID 生成），Go 标准库 `encoding/json`（matcherParams 解析）

---

## 文件结构

### 后端 (`api/`)

| 文件 | 变更 | 说明 |
|---|---|---|
| `model.go` | 修改 + 新增 | Campaign 加3字段；新增 LicenseKey struct |
| `migrate.go` | 修改 | 注册 `&LicenseKey{}` |
| `type.go` | 修改 | 新增 LicenseKey 相关 Request/Response 类型 |
| `response.go` | 修改 | 新增错误码 400007/400008 |
| `logic_campaign.go` | 修改 | 新增 `paid_before` / `paid_before_active` matcher |
| `logic_license_key.go` | 新建 | 生成、验证、消费 LicenseKey 核心逻辑 |
| `api_admin_campaigns.go` | 修改 | 新增 `POST /:id/issue-keys` 接口 |
| `api_admin_license_key.go` | 新建 | 管理端 CRUD + 统计 |
| `api_license_key.go` | 新建 | 用户端：落地页详情 + 预览 |
| `api_order.go` | 修改 | 下单时兼容 LicenseKey UUID 作为 campaignCode |
| `worker_license_key.go` | 新建 | 批量生成 + 邮件发送 worker |
| `route.go` | 修改 | 注册新路由 |

### 前端 (`web/`)

| 文件 | 变更 | 说明 |
|---|---|---|
| `src/lib/api.ts` | 修改 | 新增 LicenseKey 类型 + API 方法 |
| `src/app/(manager)/manager/campaigns/page.tsx` | 修改 | 新增 paid_before matcher UI + shareable 配置项 |
| `src/app/(manager)/manager/license-keys/page.tsx` | 新建 | 管理端授权码列表页 |
| `src/app/[locale]/redeem/[uuid]/page.tsx` | 新建 | 公开落地页 |
| `src/app/[locale]/redeem/[uuid]/RedeemClient.tsx` | 新建 | 落地页客户端组件 |
| `messages/zh-CN/licenseKeys.json` | 新建 | 中文 i18n |
| `messages/en-US/licenseKeys.json` | 新建 | 英文 i18n |
| `messages/namespaces.ts` | 修改 | 新增 `"licenseKeys"` 到 namespaces 数组（**勿手动编辑**，运行 `node scripts/i18n/split-namespaces.js web`） |

---

## Task 1: 数据模型扩展

**Files:**
- Modify: `api/model.go`
- Modify: `api/migrate.go`
- Modify: `api/type.go`
- Modify: `api/response.go`

- [ ] **Step 1: 在 `api/model.go` 的 Campaign struct 末尾新增三个字段**

在 `UsageCount` 前插入：
```go
// 新增字段 ↓
MatcherParams string `gorm:"type:varchar(500)" json:"matcherParams"`
IsShareable   bool   `gorm:"default:false" json:"isShareable"`
SharesPerUser int64  `gorm:"default:0" json:"sharesPerUser"`
```

- [ ] **Step 2: 在 `api/model.go` 末尾新增 LicenseKey struct**

```go
// LicenseKey 一次性授权码（定向分发，用于老带新）
type LicenseKey struct {
	ID        uint64         `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	DeletedAt gorm.DeletedAt `gorm:"index"`

	UUID string `gorm:"type:varchar(50);uniqueIndex;not null" json:"uuid"`

	DiscountType  string `gorm:"type:varchar(20);not null" json:"discountType"`
	DiscountValue uint64 `gorm:"not null" json:"discountValue"`

	RecipientMatcher string `gorm:"type:varchar(50);not null" json:"recipientMatcher"`
	ExpiresAt        int64  `gorm:"not null" json:"expiresAt"`

	CampaignID *uint64 `gorm:"index" json:"campaignId"`

	CreatedByUserID *uint64 `gorm:"index" json:"createdByUserId"`
	IsUsed          bool    `gorm:"default:false" json:"isUsed"`
	UsedByUserID    *uint64 `gorm:"index" json:"usedByUserId"`
	UsedAt          *time.Time `json:"usedAt"`
}

func (LicenseKey) TableName() string { return "license_keys" }
```

- [ ] **Step 3: 在 `api/migrate.go` 的 AutoMigrate 列表中新增 `&LicenseKey{}`**

在 `&Campaign{},` 下一行插入：
```go
&LicenseKey{},
```

- [ ] **Step 4: 在 `api/type.go` 的 CampaignRequest 中新增三个字段**

```go
MatcherParams string `json:"matcherParams"`
IsShareable   bool   `json:"isShareable"`
SharesPerUser int64  `json:"sharesPerUser"`
```

同样在 CampaignResponse 中新增这三个字段。

- [ ] **Step 5: 在 `api/type.go` 末尾新增 LicenseKey 类型**

```go
type LicenseKeyResponse struct {
	ID               uint64     `json:"id"`
	UUID             string     `json:"uuid"`
	DiscountType     string     `json:"discountType"`
	DiscountValue    uint64     `json:"discountValue"`
	RecipientMatcher string     `json:"recipientMatcher"`
	ExpiresAt        int64      `json:"expiresAt"`
	CampaignID       *uint64    `json:"campaignId"`
	CreatedByUserID  *uint64    `json:"createdByUserId"`
	IsUsed           bool       `json:"isUsed"`
	UsedByUserID     *uint64    `json:"usedByUserId"`
	UsedAt           *int64     `json:"usedAt"` // Unix ts or nil
	CreatedAt        int64      `json:"createdAt"`
}

type IssueKeysRequest struct {
	DryRun bool `json:"dryRun"` // true = 只返回预计数量，不实际生成
}

type IssueKeysResponse struct {
	EligibleUsers int64 `json:"eligibleUsers"`
	KeysToIssue   int64 `json:"keysToIssue"`
	Issued        bool  `json:"issued"`
}

type LicenseKeyPublicResponse struct {
	UUID          string `json:"uuid"`
	DiscountType  string `json:"discountType"`
	DiscountValue uint64 `json:"discountValue"`
	ExpiresAt     int64  `json:"expiresAt"`
	IsUsed        bool   `json:"isUsed"`
	IsExpired     bool   `json:"isExpired"`
	SenderName    string `json:"senderName"` // 发送人昵称（脱敏）
}
```

- [ ] **Step 6: 在 `api/response.go` 新增错误码**

```go
ErrorLicenseKeyNotFound  ErrorCode = 400007 // 授权码不存在
ErrorLicenseKeyUsed      ErrorCode = 400008 // 授权码已被使用
ErrorLicenseKeyExpired   ErrorCode = 400009 // 授权码已过期
ErrorLicenseKeyNotMatch  ErrorCode = 400010 // 不符合使用条件
```

- [ ] **Step 7: 运行迁移验证表结构**

```bash
cd api && go build ./... 2>&1
```
Expected: 编译成功，无报错

- [ ] **Step 8: Commit**

```bash
git add api/model.go api/migrate.go api/type.go api/response.go
git commit -m "feat(license-key): add LicenseKey model and Campaign shareable fields"
```

---

## Task 2: Campaign Matcher 扩展

**Files:**
- Modify: `api/logic_campaign.go`
- Test: `api/logic_campaign_test.go`（如不存在则新建）

- [ ] **Step 1: 写失败测试**

在 `api/` 目录新建或追加 `logic_campaign_matcher_test.go`：

```go
package center

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestPaidBeforeMatcher(t *testing.T) {
	ctx := context.Background()
	cutoff := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC).Unix()

	campaign := &Campaign{
		MatcherType:  "paid_before",
		MatcherParams: `{"beforeDate": ` + fmt.Sprintf("%d", cutoff) + `}`,
	}

	// 用户首次付款在截止日期前 → 匹配
	userOld := &User{ID: 1}
	// 模拟查询：需要 mock DB，此处用集成测试方式

	matcher := getCampaignMatcherWithDB(db.Get(), campaign.MatcherType, campaign.MatcherParams)
	assert.NotNil(t, matcher)
	_ = ctx
}
```

- [ ] **Step 2: 运行测试，确认编译失败**（`getCampaignMatcherWithDB` 未定义）

```bash
cd api && go test ./... -run TestPaidBeforeMatcher 2>&1 | head -20
```

- [ ] **Step 3: 重构 `getCampaignMatcher` 为接受 db + params 的版本**

在 `api/logic_campaign.go` 中：

```go
import (
	"encoding/json"
	"time"
	gormdb "gorm.io/gorm"
)

type matcherParams struct {
	BeforeDate int64 `json:"beforeDate"`
}

// getCampaignMatcherWithDB 返回用户资格校验函数（支持 DB 查询）
func getCampaignMatcherWithDB(db *gormdb.DB, matcherType, params string) func(ctx context.Context, user *User, order *Order) bool {
	switch matcherType {
	case "first_order":
		return func(ctx context.Context, user *User, order *Order) bool {
			return user.IsFirstOrderDone != nil && *user.IsFirstOrderDone
		}
	case "vip":
		return func(ctx context.Context, user *User, order *Order) bool {
			return user.IsVip()
		}
	case "paid_before":
		var p matcherParams
		_ = json.Unmarshal([]byte(params), &p)
		return func(ctx context.Context, user *User, order *Order) bool {
			if p.BeforeDate == 0 {
				return false
			}
			var firstPaidAt time.Time
			err := db.Model(&Order{}).
				Where("user_id = ? AND is_paid = true", user.ID).
				Order("paid_at ASC").
				Limit(1).
				Select("paid_at").
				Scan(&firstPaidAt).Error
			if err != nil || firstPaidAt.IsZero() {
				return false
			}
			return firstPaidAt.Unix() < p.BeforeDate
		}
	case "paid_before_active":
		var p matcherParams
		_ = json.Unmarshal([]byte(params), &p)
		return func(ctx context.Context, user *User, order *Order) bool {
			if p.BeforeDate == 0 {
				return false
			}
			var firstPaidAt time.Time
			err := db.Model(&Order{}).
				Where("user_id = ? AND is_paid = true", user.ID).
				Order("paid_at ASC").
				Limit(1).
				Select("paid_at").
				Scan(&firstPaidAt).Error
			if err != nil || firstPaidAt.IsZero() {
				return false
			}
			if firstPaidAt.Unix() >= p.BeforeDate {
				return false
			}
			// 套餐仍有效（User.ExpiredAt 是 Unix 时间戳）
			return user.ExpiredAt > time.Now().Unix()
		}
	case "all":
		return func(ctx context.Context, user *User, order *Order) bool { return true }
	default:
		return nil
	}
}

// getCampaignMatcher 保持原有签名兼容（无 DB 查询）
func getCampaignMatcher(matcherType string) func(ctx context.Context, user *User, order *Order) bool {
	return getCampaignMatcherWithDB(nil, matcherType, "")
}
```

更新 `matchCampaign` 函数调用：
```go
matcher := getCampaignMatcherWithDB(db.Get(), campaign.MatcherType, campaign.MatcherParams)
```

- [ ] **Step 4: 运行测试**

```bash
cd api && go test ./... -run TestPaidBefore 2>&1
```

- [ ] **Step 5: Commit**

```bash
git add api/logic_campaign.go api/logic_campaign_matcher_test.go
git commit -m "feat(campaign): add paid_before and paid_before_active matchers"
```

---

## Task 3: LicenseKey 核心逻辑

**Files:**
- Create: `api/logic_license_key.go`
- Test: `api/logic_license_key_test.go`

- [ ] **Step 1: 新建 `api/logic_license_key.go`**

```go
package center

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/xid"
	gormdb "gorm.io/gorm"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
)

const licenseKeyTTLDays = 30

// GenerateLicenseKeysForCampaign 为活动批量生成授权码
// 返回生成的授权码数量
func GenerateLicenseKeysForCampaign(ctx context.Context, campaign *Campaign) (int64, error) {
	if !campaign.IsShareable || campaign.SharesPerUser <= 0 {
		return 0, fmt.Errorf("campaign is not shareable or sharesPerUser is 0")
	}

	// 查询符合条件的老用户
	users, err := queryEligibleUsers(ctx, campaign)
	if err != nil {
		return 0, fmt.Errorf("query eligible users: %w", err)
	}

	expiresAt := time.Now().AddDate(0, 0, licenseKeyTTLDays).Unix()
	var keys []LicenseKey
	for _, user := range users {
		userID := user.ID
		campaignID := campaign.ID
		for i := int64(0); i < campaign.SharesPerUser; i++ {
			keys = append(keys, LicenseKey{
				UUID:             xid.New().String(),
				DiscountType:     campaign.Type,
				DiscountValue:    campaign.Value,
				RecipientMatcher: "never_paid",
				ExpiresAt:        expiresAt,
				CampaignID:       &campaignID,
				CreatedByUserID:  &userID,
			})
		}
	}

	// 分批插入（每批 100 条）
	batchSize := 100
	for i := 0; i < len(keys); i += batchSize {
		end := i + batchSize
		if end > len(keys) {
			end = len(keys)
		}
		if err := db.Get().CreateInBatches(keys[i:end], batchSize).Error; err != nil {
			return int64(i), fmt.Errorf("batch insert at %d: %w", i, err)
		}
	}

	log.Infof(ctx, "[LICENSE_KEY] campaign=%d generated=%d keys for %d users",
		campaign.ID, len(keys), len(users))
	return int64(len(keys)), nil
}

// queryEligibleUsers 查询符合活动条件的老用户
func queryEligibleUsers(ctx context.Context, campaign *Campaign) ([]User, error) {
	matcher := getCampaignMatcherWithDB(db.Get(), campaign.MatcherType, campaign.MatcherParams)
	if matcher == nil {
		return nil, fmt.Errorf("unknown matcherType: %s", campaign.MatcherType)
	}

	// 分页查询全量用户，避免一次加载太多
	var users []User
	var page int
	pageSize := 500
	for {
		var batch []User
		if err := db.Get().Offset(page*pageSize).Limit(pageSize).Find(&batch).Error; err != nil {
			return nil, err
		}
		if len(batch) == 0 {
			break
		}
		for _, u := range batch {
			userCopy := u
			if matcher(ctx, &userCopy, nil) {
				users = append(users, userCopy)
			}
		}
		if len(batch) < pageSize {
			break
		}
		page++
	}
	return users, nil
}

// GetLicenseKeyByUUID 查询授权码（公开，不需要登录）
func GetLicenseKeyByUUID(ctx context.Context, uuid string) (*LicenseKey, error) {
	var key LicenseKey
	err := db.Get().Where("uuid = ?", uuid).First(&key).Error
	if err != nil {
		return nil, err
	}
	return &key, nil
}

// ConsumeLicenseKey 原子消费授权码
// 返回 nil error 表示成功
func ConsumeLicenseKey(ctx context.Context, tx *gormdb.DB, uuid string, userID uint64) (*LicenseKey, error) {
	// 先查
	var key LicenseKey
	if err := tx.Where("uuid = ?", uuid).First(&key).Error; err != nil {
		return nil, ErrorLicenseKeyNotFound
	}
	if key.IsUsed {
		return nil, ErrorLicenseKeyUsed
	}
	if key.ExpiresAt < time.Now().Unix() {
		return nil, ErrorLicenseKeyExpired
	}

	// 原子更新（防并发）
	result := tx.Model(&LicenseKey{}).
		Where("uuid = ? AND is_used = false", uuid).
		Updates(map[string]any{
			"is_used":         true,
			"used_by_user_id": userID,
			"used_at":         time.Now(),
		})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrorLicenseKeyUsed // 并发竞争失败
	}

	key.IsUsed = true
	key.UsedByUserID = &userID
	return &key, nil
}

// MatchLicenseKey 校验用户是否有资格使用此授权码
func MatchLicenseKey(ctx context.Context, key *LicenseKey, user *User) bool {
	if key.RecipientMatcher == "all" {
		return true
	}
	if key.RecipientMatcher == "never_paid" {
		return user.IsFirstOrderDone == nil || !*user.IsFirstOrderDone
	}
	return false
}

// ApplyLicenseKeyDiscount 计算折扣后金额
func ApplyLicenseKeyDiscount(key *LicenseKey, originAmount uint64) (newAmount uint64, reduced uint64) {
	switch key.DiscountType {
	case "discount":
		newAmount = originAmount * key.DiscountValue / 100
	case "coupon":
		if key.DiscountValue >= originAmount {
			newAmount = 0
		} else {
			newAmount = originAmount - key.DiscountValue
		}
	default:
		newAmount = originAmount
	}
	if newAmount > originAmount {
		newAmount = originAmount
	}
	reduced = originAmount - newAmount
	return
}

// CountEligibleUsers 预估符合条件的用户数（dryRun 用）
func CountEligibleUsers(ctx context.Context, campaign *Campaign) (int64, error) {
	users, err := queryEligibleUsers(ctx, campaign)
	if err != nil {
		return 0, err
	}
	return int64(len(users)), nil
}
```

注意：`ErrorLicenseKeyNotFound` 等需要定义为可与 `error` 比较的类型。在 `response.go` 中为它们加上 `Error()` 方法，或改用 sentinel errors。推荐：

```go
// 在 response.go 追加
var (
	ErrLicenseKeyNotFound = errors.New("license key not found")
	ErrLicenseKeyUsed     = errors.New("license key already used")
	ErrLicenseKeyExpired  = errors.New("license key expired")
	ErrLicenseKeyNotMatch = errors.New("license key recipient mismatch")
)
```

- [ ] **Step 2: 写单元测试 `api/logic_license_key_test.go`**

```go
package center

import (
	"testing"
	"github.com/stretchr/testify/assert"
)

func TestApplyLicenseKeyDiscount(t *testing.T) {
	tests := []struct {
		name         string
		discountType string
		value        uint64
		origin       uint64
		wantNew      uint64
		wantReduced  uint64
	}{
		{"discount 80%", "discount", 80, 1000, 800, 200},
		{"coupon 200 cents", "coupon", 200, 1000, 800, 200},
		{"coupon exceeds price", "coupon", 1500, 1000, 0, 1000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := &LicenseKey{DiscountType: tt.discountType, DiscountValue: tt.value}
			got, reduced := ApplyLicenseKeyDiscount(key, tt.origin)
			assert.Equal(t, tt.wantNew, got)
			assert.Equal(t, tt.wantReduced, reduced)
		})
	}
}

func TestMatchLicenseKey_NeverPaid(t *testing.T) {
	key := &LicenseKey{RecipientMatcher: "never_paid"}

	// 从未付款用户 → 匹配
	neverPaid := false
	user := &User{IsFirstOrderDone: &neverPaid}
	assert.True(t, MatchLicenseKey(nil, key, user))

	// 已付款用户 → 不匹配
	paid := true
	user2 := &User{IsFirstOrderDone: &paid}
	assert.False(t, MatchLicenseKey(nil, key, user2))
}
```

- [ ] **Step 3: 运行测试**

```bash
cd api && go test ./... -run "TestApplyLicenseKey|TestMatchLicenseKey" -v 2>&1
```
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add api/logic_license_key.go api/logic_license_key_test.go api/response.go
git commit -m "feat(license-key): core logic for generate, consume, match, apply discount"
```

---

## Task 4: 管理端 API

**Files:**
- Create: `api/api_admin_license_key.go`
- Modify: `api/api_admin_campaigns.go`
- Modify: `api/route.go`

- [ ] **Step 1: 新建 `api/api_admin_license_key.go`**

```go
package center

import (
	"strconv"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
)

// GET /app/license-keys
func api_admin_list_license_keys(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	campaignIDStr := c.Query("campaignId")
	isUsed := c.Query("isUsed") // "true" / "false" / ""

	query := db.Get().Model(&LicenseKey{})
	if campaignIDStr != "" {
		id, _ := strconv.ParseUint(campaignIDStr, 10, 64)
		query = query.Where("campaign_id = ?", id)
	}
	if isUsed == "true" {
		query = query.Where("is_used = true")
	} else if isUsed == "false" {
		query = query.Where("is_used = false")
	}

	var total int64
	query.Count(&total)

	var keys []LicenseKey
	query.Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&keys)

	items := make([]LicenseKeyResponse, 0, len(keys))
	for _, k := range keys {
		items = append(items, toLicenseKeyResponse(k))
	}
	pagination := &Pagination{Page: page, PageSize: pageSize, Total: total}
	ListWithData(c, items, pagination)
}

// DELETE /app/license-keys/:id
func api_admin_delete_license_key(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Error(c, ErrorInvalidArgument, "invalid id")
		return
	}
	if err := db.Get().Delete(&LicenseKey{}, id).Error; err != nil {
		Error(c, ErrorSystemError, err.Error())
		return
	}
	SuccessEmpty(c)
}

func toLicenseKeyResponse(k LicenseKey) LicenseKeyResponse {
	r := LicenseKeyResponse{
		ID:               k.ID,
		UUID:             k.UUID,
		DiscountType:     k.DiscountType,
		DiscountValue:    k.DiscountValue,
		RecipientMatcher: k.RecipientMatcher,
		ExpiresAt:        k.ExpiresAt,
		CampaignID:       k.CampaignID,
		CreatedByUserID:  k.CreatedByUserID,
		IsUsed:           k.IsUsed,
		UsedByUserID:     k.UsedByUserID,
		CreatedAt:        k.CreatedAt.Unix(),
	}
	if k.UsedAt != nil {
		ts := k.UsedAt.Unix()
		r.UsedAt = &ts
	}
	return r
}
```

- [ ] **Step 2: 在 `api/api_admin_campaigns.go` 末尾新增 issue-keys 接口**

```go
// POST /app/campaigns/:id/issue-keys
func api_admin_issue_license_keys(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		Error(c, ErrorInvalidArgument, "invalid id")
		return
	}

	var req IssueKeysRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}

	var campaign Campaign
	if err := db.Get().First(&campaign, id).Error; err != nil {
		Error(c, ErrorNotFound, "campaign not found")
		return
	}
	if !campaign.IsShareable {
		Error(c, ErrorInvalidArgument, "campaign is not shareable")
		return
	}

	ctx := c.Request.Context()

	if req.DryRun {
		count, err := CountEligibleUsers(ctx, &campaign)
		if err != nil {
			Error(c, ErrorSystemError, err.Error())
			return
		}
		resp1 := IssueKeysResponse{
			EligibleUsers: count,
			KeysToIssue:   count * campaign.SharesPerUser,
			Issued:        false,
		}
		Success(c, &resp1)
		return
	}

	count, err := GenerateLicenseKeysForCampaign(ctx, &campaign)
	if err != nil {
		Error(c, ErrorSystemError, err.Error())
		return
	}
	resp2 := IssueKeysResponse{
		EligibleUsers: count / campaign.SharesPerUser,
		KeysToIssue:   count,
		Issued:        true,
	}
	Success(c, &resp2)
}
```

- [ ] **Step 3: 在 `api/route.go` 注册路由**

在 campaign 路由组之后追加：
```go
// Campaign 发放授权码
admin.POST("/campaigns/:id/issue-keys", api_admin_issue_license_keys)

// LicenseKey 管理
admin.GET("/license-keys", api_admin_list_license_keys)
admin.DELETE("/license-keys/:id", api_admin_delete_license_key)
```

- [ ] **Step 4: 编译验证**

```bash
cd api && go build ./... 2>&1
```

- [ ] **Step 5: Commit**

```bash
git add api/api_admin_license_key.go api/api_admin_campaigns.go api/route.go
git commit -m "feat(license-key): admin API endpoints for list, delete, issue-keys"
```

---

## Task 5: 用户端 API + 下单集成

**Files:**
- Create: `api/api_license_key.go`
- Modify: `api/api_order.go`
- Modify: `api/route.go`

- [ ] **Step 1: 新建 `api/api_license_key.go`**

```go
package center

import (
	"time"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
)

// GET /api/license-keys/:uuid  (无需登录)
func api_get_license_key(c *gin.Context) {
	uuid := c.Param("uuid")
	key, err := GetLicenseKeyByUUID(c.Request.Context(), uuid)
	if err != nil {
		Error(c, ErrorLicenseKeyNotFound, "not found")
		return
	}

	resp := LicenseKeyPublicResponse{
		UUID:          key.UUID,
		DiscountType:  key.DiscountType,
		DiscountValue: key.DiscountValue,
		ExpiresAt:     key.ExpiresAt,
		IsUsed:        key.IsUsed,
		IsExpired:     key.ExpiresAt < time.Now().Unix(),
	}

	// 获取发送人昵称（脱敏邮箱）
	if key.CreatedByUserID != nil {
		var sender User
		if err := db.Get().Preload("LoginIdentifies").
			First(&sender, *key.CreatedByUserID).Error; err == nil {
			resp.SenderName = senderDisplayName(sender)
		}
	}

	Success(c, &resp)
}

// senderDisplayName 从 LoginIdentifies 获取发送人昵称（脱敏邮箱）
// 注意：user 须已 Preload("LoginIdentifies")
func senderDisplayName(u User) string {
	email := getUserEmailFromIdentifies(&u)
	if email == "" {
		return "你的朋友"
	}
	return hideEmail(email) // hideEmail 已在 worker_renewal_reminder.go 中定义
}
```

- [ ] **Step 2: 在 `api/route.go` 注册公开路由**

```go
// 公开路由（无需登录）
public.GET("/license-keys/:uuid", api_get_license_key)
```

- [ ] **Step 3: 修改 `api/api_order.go` 的下单逻辑**

在处理 `campaignCode` 的地方，新增判断：如果 `campaignCode` 长度 == 20（xid 格式），走 LicenseKey 路径：

```go
// 判断是 LicenseKey UUID 还是普通 Campaign 码
if req.CampaignCode != "" {
	if len(req.CampaignCode) == 20 { // xid 格式
		key, err := GetLicenseKeyByUUID(ctx, req.CampaignCode)
		if err != nil || key.IsUsed || key.ExpiresAt < time.Now().Unix() {
			Error(c, ErrorLicenseKeyNotFound, "invalid license key")
			return
		}
		if !MatchLicenseKey(ctx, key, &currentUser) {
			Error(c, ErrorLicenseKeyNotMatch, "not eligible")
			return
		}
		newAmount, reduced := ApplyLicenseKeyDiscount(key, order.OriginAmount)
		order.PayAmount = newAmount
		order.CampaignReduceAmount = reduced
		order.CampaignCode = &req.CampaignCode
		// 消费（在事务中）
		if _, err := ConsumeLicenseKey(ctx, tx, req.CampaignCode, currentUser.ID); err != nil {
			Error(c, ErrorLicenseKeyUsed, err.Error())
			return
		}
	} else {
		// 原有 Campaign 逻辑不变
		// ...
	}
}
```

- [ ] **Step 4: 编译验证**

```bash
cd api && go build ./... 2>&1
```

- [ ] **Step 5: Commit**

```bash
git add api/api_license_key.go api/api_order.go api/route.go
git commit -m "feat(license-key): public get endpoint and order checkout integration"
```

---

## Task 6: 邮件发送 Worker

**Files:**
- Create: `api/worker_license_key.go`

- [ ] **Step 1: 新建 `api/worker_license_key.go`**

```go
package center

import (
	"context"
	"fmt"

	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/mail"
)

const baseURL = "https://kaitu.io"

// SendLicenseKeyEmails 为活动的所有授权码发送邮件
// 在 issue-keys 接口生成后调用（可异步）
func SendLicenseKeyEmails(ctx context.Context, campaignID uint64) error {
	var keys []LicenseKey
	if err := db.Get().
		Where("campaign_id = ? AND is_used = false", campaignID).
		Find(&keys).Error; err != nil {
		return err
	}

	// 按 created_by_user_id 分组
	userKeys := map[uint64][]LicenseKey{}
	for _, k := range keys {
		if k.CreatedByUserID != nil {
			userKeys[*k.CreatedByUserID] = append(userKeys[*k.CreatedByUserID], k)
		}
	}

	var sent, failed int
	for userID, userKeyList := range userKeys {
		var user User
		if err := db.Get().Preload("LoginIdentifies").First(&user, userID).Error; err != nil {
			failed++
			continue
		}
		if err := sendGiftEmail(ctx, user, userKeyList); err != nil {
			log.Warnf(ctx, "[LICENSE_KEY] failed to send email to user %d: %v", userID, err)
			failed++
		} else {
			sent++
		}
	}

	log.Infof(ctx, "[LICENSE_KEY] email send complete: sent=%d failed=%d", sent, failed)
	return nil
}

func sendGiftEmail(ctx context.Context, user User, keys []LicenseKey) error {
	subject := fmt.Sprintf("你有 %d 个专属礼物名额可以送给朋友", len(keys))

	body := "<p>感谢你一直以来对 Kaitu 的支持！</p>"
	body += "<p>作为老用户专属福利，我们送你以下礼物链接，可以分享给朋友：</p>"
	body += "<ul>"
	for _, k := range keys {
		link := fmt.Sprintf("%s/redeem/%s", baseURL, k.UUID)
		body += fmt.Sprintf(`<li><a href="%s">%s</a>（30天内有效）</li>`, link, link)
	}
	body += "</ul>"
	body += "<p>每个链接只能使用一次，仅限从未购买过的新用户。</p>"

	// 通过 Preload 获取邮箱
	email := getUserEmailFromIdentifies(&user)
	if email == "" {
		return fmt.Errorf("no email for user %d", user.ID)
	}
	return mail.Send(&mail.Message{
		To:      email,
		Subject: subject,
		Body:    body,
	})
}
```

- [ ] **Step 2: 在 `api_admin_campaigns.go` 的 issue-keys 接口中，生成成功后异步发送邮件**

```go
// 在 GenerateLicenseKeysForCampaign 成功后追加
go func() {
    bgCtx := context.Background()
    if err := SendLicenseKeyEmails(bgCtx, campaign.ID); err != nil {
        log.Warnf(bgCtx, "[LICENSE_KEY] email send failed: %v", err)
    }
}()
```

- [ ] **Step 3: 编译验证**

```bash
cd api && go build ./... 2>&1
```

- [ ] **Step 4: Commit**

```bash
git add api/worker_license_key.go api/api_admin_campaigns.go
git commit -m "feat(license-key): async email distribution after issue-keys"
```

---

## Task 7: 前端 — i18n + API 类型

**Files:**
- Modify: `web/src/lib/api.ts`
- Create: `web/messages/zh-CN/licenseKeys.json`
- Create: `web/messages/en-US/licenseKeys.json`

- [ ] **Step 1: 在 `web/src/lib/api.ts` 新增 LicenseKey 类型和 API 方法**

```typescript
// Types
export interface LicenseKeyPublic {
  uuid: string
  discountType: 'discount' | 'coupon'
  discountValue: number
  expiresAt: number
  isUsed: boolean
  isExpired: boolean
  senderName: string
}

export interface LicenseKeyAdmin {
  id: number
  uuid: string
  discountType: string
  discountValue: number
  recipientMatcher: string
  expiresAt: number
  campaignId?: number
  createdByUserId?: number
  isUsed: boolean
  usedByUserId?: number
  usedAt?: number
  createdAt: number
}

export interface IssueKeysRequest {
  dryRun: boolean
}

export interface IssueKeysResponse {
  eligibleUsers: number
  keysToIssue: number
  issued: boolean
}

// API methods
export const getLicenseKey = (uuid: string) =>
  apiGet<LicenseKeyPublic>(`/api/license-keys/${uuid}`)

export const listAdminLicenseKeys = (params: {
  campaignId?: number
  isUsed?: boolean
  page?: number
  pageSize?: number
}) => apiGet<{ items: LicenseKeyAdmin[]; total: number }>('/app/license-keys', params)

export const issueKeys = (campaignId: number, req: IssueKeysRequest) =>
  apiPost<IssueKeysResponse>(`/app/campaigns/${campaignId}/issue-keys`, req)

export const deleteAdminLicenseKey = (id: number) =>
  apiDelete(`/app/license-keys/${id}`)
```

- [ ] **Step 2: 新建 `web/messages/zh-CN/licenseKeys.json`**

```json
{
  "gift": {
    "title": "{name} 送给你一个礼物",
    "subtitle": "Kaitu VPN 专属优惠",
    "discount": "享 {value}% 折扣",
    "coupon": "立减 ¥{value}",
    "expires": "还剩 {days} 天到期",
    "cta": "立即领取",
    "used": "此礼物已被领走",
    "expired": "此礼物已过期",
    "fallback": "查看新用户专属优惠",
    "loading": "加载中..."
  },
  "admin": {
    "title": "授权码管理",
    "total": "共 {total} 个",
    "columns": {
      "uuid": "唯一码",
      "discount": "折扣",
      "status": "状态",
      "expires": "过期时间",
      "campaign": "关联活动",
      "sender": "发送人",
      "recipient": "使用人",
      "createdAt": "创建时间"
    },
    "status": {
      "unused": "未使用",
      "used": "已使用",
      "expired": "已过期"
    }
  },
  "campaigns": {
    "matchers": {
      "paid_before": "{date} 前首次付款的用户",
      "paid_before_active": "{date} 前首次付款且套餐有效的用户"
    },
    "shareable": {
      "label": "启用老带新",
      "sharesPerUser": "每人可分享数量",
      "issueKeys": "立即发放授权码",
      "dryRun": "预估人数",
      "confirmIssue": "确认为 {count} 位用户各发放 {n} 个授权码（共 {total} 个）？"
    }
  }
}
```

- [ ] **Step 3: 新建 `web/messages/en-US/licenseKeys.json`**（对应翻译，略）

- [ ] **Step 4: 更新 namespaces.ts**

`web/messages/namespaces.ts` 不能手动编辑，运行生成脚本：
```bash
cd web && node scripts/i18n/split-namespaces.js web 2>&1
```
验证 `namespaces.ts` 中的数组已包含 `"licenseKeys"`。

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/api.ts web/messages/zh-CN/licenseKeys.json web/messages/en-US/licenseKeys.json web/messages/namespaces.ts
git commit -m "feat(license-key): frontend types, API methods, and i18n strings"
```

---

## Task 8: 前端 — Campaign 页面扩展

**Files:**
- Modify: `web/src/app/(manager)/manager/campaigns/page.tsx`

- [ ] **Step 1: 在 Campaign 表单的 matcherType 选项中新增两个选项**

找到现有的 `matcherType` 选择器，新增：
```tsx
{ value: 'paid_before', label: t('campaigns.matchers.paid_before', { date: '...' }) },
{ value: 'paid_before_active', label: t('campaigns.matchers.paid_before_active', { date: '...' }) },
```

- [ ] **Step 2: 当 matcherType 为 `paid_before` 或 `paid_before_active` 时，显示日期选择器**

```tsx
{(form.matcherType === 'paid_before' || form.matcherType === 'paid_before_active') && (
  <div>
    <label>{t('campaigns.matcherParams.beforeDate')}</label>
    <input
      type="datetime-local"
      value={matcherParamsBeforeDate}
      onChange={(e) => {
        const ts = Math.floor(new Date(e.target.value).getTime() / 1000)
        setForm({ ...form, matcherParams: JSON.stringify({ beforeDate: ts }) })
      }}
    />
  </div>
)}
```

- [ ] **Step 3: 新增 isShareable 开关和 sharesPerUser 输入**

```tsx
<div>
  <label>{t('campaigns.shareable.label')}</label>
  <Switch
    checked={form.isShareable}
    onChange={(v) => setForm({ ...form, isShareable: v })}
  />
</div>
{form.isShareable && (
  <>
    <div>
      <label>{t('campaigns.shareable.sharesPerUser')}</label>
      <input
        type="number"
        min={1}
        max={10}
        value={form.sharesPerUser}
        onChange={(e) => setForm({ ...form, sharesPerUser: Number(e.target.value) })}
      />
    </div>
    <Button
      variant="outline"
      onClick={() => handleIssueKeys(campaign.id, true)} // dryRun
    >
      {t('campaigns.shareable.dryRun')}
    </Button>
    <Button
      onClick={() => handleIssueKeys(campaign.id, false)}
    >
      {t('campaigns.shareable.issueKeys')}
    </Button>
  </>
)}
```

- [ ] **Step 4: 实现 `handleIssueKeys`**

```tsx
const handleIssueKeys = async (campaignId: number, dryRun: boolean) => {
  const res = await issueKeys(campaignId, { dryRun })
  if (dryRun) {
    alert(t('campaigns.shareable.confirmIssue', {
      count: res.eligibleUsers,
      n: campaign.sharesPerUser,
      total: res.keysToIssue,
    }))
  } else {
    toast.success(`已发放 ${res.keysToIssue} 个授权码`)
  }
}
```

- [ ] **Step 5: Commit**

```bash
git add web/src/app/(manager)/manager/campaigns/page.tsx
git commit -m "feat(campaign): add paid_before matcher UI and shareable keys configuration"
```

---

## Task 9: 前端 — 落地页

**Files:**
- Create: `web/src/app/[locale]/redeem/[uuid]/page.tsx`
- Create: `web/src/app/[locale]/redeem/[uuid]/RedeemClient.tsx`

- [ ] **Step 1: 新建 `page.tsx`（服务端 metadata）**

```tsx
import { getLicenseKey } from '@/lib/api'
import RedeemClient from './RedeemClient'
import { notFound } from 'next/navigation'

export async function generateMetadata({ params }) {
  return { title: '专属礼物 | Kaitu' }
}

export default async function RedeemPage({ params }) {
  const { uuid } = await params
  let key = null
  try {
    key = await getLicenseKey(uuid)
  } catch {
    notFound()
  }
  return <RedeemClient initialKey={key} uuid={uuid} />
}
```

- [ ] **Step 2: 新建 `RedeemClient.tsx`**

```tsx
'use client'
import { useTranslations } from 'next-intl'
import { useRouter } from 'next/navigation'
import { LicenseKeyPublic } from '@/lib/api'

export default function RedeemClient({
  initialKey,
  uuid,
}: {
  initialKey: LicenseKeyPublic
  uuid: string
}) {
  const t = useTranslations('licenseKeys')
  const router = useRouter()
  const key = initialKey

  if (key.isUsed) {
    return (
      <div className="flex flex-col items-center justify-center min-h-screen gap-4">
        <p className="text-xl">{t('gift.used')}</p>
        <button onClick={() => router.push('/purchase')}>
          {t('gift.fallback')}
        </button>
      </div>
    )
  }

  if (key.isExpired) {
    return (
      <div className="flex flex-col items-center justify-center min-h-screen gap-4">
        <p className="text-xl">{t('gift.expired')}</p>
        <button onClick={() => router.push('/purchase')}>
          {t('gift.fallback')}
        </button>
      </div>
    )
  }

  const daysLeft = Math.max(0, Math.ceil((key.expiresAt - Date.now() / 1000) / 86400))
  const discountLabel = key.discountType === 'discount'
    ? t('gift.discount', { value: key.discountValue })
    : t('gift.coupon', { value: (key.discountValue / 100).toFixed(2) })

  return (
    <div className="flex flex-col items-center justify-center min-h-screen gap-6 p-8">
      <h1 className="text-2xl font-bold">
        {t('gift.title', { name: key.senderName })}
      </h1>
      <div className="border rounded-2xl p-8 text-center shadow-lg max-w-sm w-full">
        <p className="text-4xl font-bold text-primary mb-2">{discountLabel}</p>
        <p className="text-sm text-muted-foreground">
          {t('gift.expires', { days: daysLeft })}
        </p>
      </div>
      <button
        className="bg-primary text-white px-8 py-3 rounded-full text-lg font-semibold"
        onClick={() => router.push(`/purchase?licenseKey=${uuid}`)}
      >
        {t('gift.cta')}
      </button>
    </div>
  )
}
```

- [ ] **Step 3: 验证路由**

```bash
cd web && yarn dev 2>&1 &
# 访问 http://localhost:3000/zh-CN/redeem/test123
# 预期: 404 页面（UUID 不存在）
```

- [ ] **Step 4: Commit**

```bash
git add "web/src/app/[locale]/redeem/"
git commit -m "feat(license-key): redeem landing page with gift framing and fallback states"
```

---

## Task 10: 防滥用 + 统计 + 预览接口

**Files:**
- Modify: `api/logic_license_key.go`
- Modify: `api/api_license_key.go`
- Modify: `api/api_admin_license_key.go`

- [ ] **Step 1: 在 `logic_license_key.go` 中新增同账号唯一性检查**

在 `ConsumeLicenseKey` 的"先查"步骤后，先检查该用户是否已使用过任何 LicenseKey：

```go
// 同一用户只能使用一个 LicenseKey
var existingUseCount int64
tx.Model(&LicenseKey{}).Where("used_by_user_id = ?", userID).Count(&existingUseCount)
if existingUseCount > 0 {
    return nil, ErrLicenseKeyNotMatch
}
```

- [ ] **Step 2: 在 `api_license_key.go` 中新增 preview 接口**

```go
// POST /api/license-keys/:uuid/preview  (需登录)
// 在下单前预确认折扣金额
func api_preview_license_key(c *gin.Context) {
	uuid := c.Param("uuid")
	user := mustGetCurrentUser(c) // 已有的 auth helper

	key, err := GetLicenseKeyByUUID(c.Request.Context(), uuid)
	if err != nil {
		Error(c, ErrorLicenseKeyNotFound, "not found")
		return
	}
	if key.IsUsed {
		Error(c, ErrorLicenseKeyUsed, "already used")
		return
	}
	if key.ExpiresAt < time.Now().Unix() {
		Error(c, ErrorLicenseKeyExpired, "expired")
		return
	}
	if !MatchLicenseKey(c.Request.Context(), key, &user) {
		Error(c, ErrorLicenseKeyNotMatch, "not eligible")
		return
	}
	type PreviewResponse struct {
		DiscountType  string `json:"discountType"`
		DiscountValue uint64 `json:"discountValue"`
		ExpiresAt     int64  `json:"expiresAt"`
		IsValid       bool   `json:"isValid"`
	}
	resp := PreviewResponse{
		DiscountType:  key.DiscountType,
		DiscountValue: key.DiscountValue,
		ExpiresAt:     key.ExpiresAt,
		IsValid:       true,
	}
	Success(c, &resp)
}
```

- [ ] **Step 3: 在 `api_admin_license_key.go` 新增统计接口**

```go
// GET /app/license-keys/stats
func api_admin_license_key_stats(c *gin.Context) {
	type StatsRow struct {
		CampaignID *uint64 `json:"campaignId"`
		Total      int64   `json:"total"`
		Used       int64   `json:"used"`
		Expired    int64   `json:"expired"`
	}

	now := time.Now().Unix()
	var rows []StatsRow
	db.Get().Model(&LicenseKey{}).
		Select("campaign_id, COUNT(*) as total, SUM(CASE WHEN is_used THEN 1 ELSE 0 END) as used, SUM(CASE WHEN NOT is_used AND expires_at < ? THEN 1 ELSE 0 END) as expired", now).
		Group("campaign_id").
		Scan(&rows)

	Success(c, &rows)
}
```

在 `route.go` 注册（**必须在 `:id` 路由之前注册，防止路径冲突**）：
```go
admin.GET("/license-keys/stats", api_admin_license_key_stats)
```

- [ ] **Step 4: 在 `route.go` 注册 preview 路由**

```go
// 需登录路由
authed.POST("/license-keys/:uuid/preview", api_preview_license_key)
```

- [ ] **Step 5: 编译验证**

```bash
cd api && go build ./... 2>&1
```

- [ ] **Step 6: 在 Task 7 的 `api.ts` 追加两个方法**

```typescript
export const previewLicenseKey = (uuid: string) =>
  apiPost<{ discountType: string; discountValue: number; expiresAt: number; isValid: boolean }>(
    `/api/license-keys/${uuid}/preview`,
    {}
  )

export const getLicenseKeyStats = () =>
  apiGet<Array<{ campaignId?: number; total: number; used: number; expired: number }>>(
    '/app/license-keys/stats'
  )
```

- [ ] **Step 7: Commit**

```bash
git add api/logic_license_key.go api/api_license_key.go api/api_admin_license_key.go api/route.go web/src/lib/api.ts
git commit -m "feat(license-key): anti-abuse per-account check, preview endpoint, admin stats"
```

---

## Task 11: 集成验证

- [ ] **Step 1: 运行完整后端测试**

```bash
cd api && go test ./... 2>&1
```
Expected: PASS

- [ ] **Step 2: 构建前端**

```bash
cd web && yarn build 2>&1 | tail -20
```
Expected: 无类型错误，构建成功

- [ ] **Step 3: 手动验收流程（本地）**

```
1. 创建 Campaign: matcherType=paid_before, isShareable=true, sharesPerUser=3
2. 调用 POST /app/campaigns/:id/issue-keys?dryRun=true → 确认用户数
3. 调用 POST /app/campaigns/:id/issue-keys → 生成授权码
4. GET /app/license-keys?campaignId=:id → 确认码已生成
5. GET /api/license-keys/:uuid → 确认公开详情返回
6. POST /api/orders { campaignCode: uuid } → 确认折扣正确应用
7. 重复 Step 6 → 确认 ErrorLicenseKeyUsed 返回
```

- [ ] **Step 4: 最终 Commit**

```bash
git add -A
git commit -m "feat(license-key): complete loyalty reward and referral system"
```

---

## 验收标准

| 场景 | 预期结果 |
|---|---|
| `paid_before` matcher | 首次付款在截止日期前的用户才通过 |
| `paid_before_active` matcher | 同上 + 套餐仍有效 |
| issue-keys dryRun | 返回预计用户数，不插入数据 |
| issue-keys 正式发放 | 每个符合条件的用户生成 N 个 LicenseKey |
| 落地页 (有效码) | 显示发送人 + 折扣 + 倒计时 + 领取按钮 |
| 落地页 (已使用) | 显示"已被领走" + 普通优惠兜底 |
| 落地页 (已过期) | 显示"已过期" + 普通优惠兜底 |
| 下单使用有效 UUID | 折扣正确，码标记为已使用 |
| 并发下单同一 UUID | 第二个请求返回 400008 |
| 已付款用户使用 never_paid 码 | 返回 400010 |
