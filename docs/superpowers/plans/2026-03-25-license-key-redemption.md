# License Key Redemption Flow — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enable admin manual license key creation with short codes + user-facing `/g` redemption landing page for new user acquisition.

**Architecture:** Extend LicenseKey model with Crockford Base32 short code, add admin creation API, replace UUID-based public endpoints with code-based, build Next.js `/g` and `/g/[code]` pages, add entry point on `/purchase`.

**Tech Stack:** Go + Gin + GORM (backend), Next.js 15 + shadcn/ui + Tailwind (web frontend), Crockford Base32 (code generation)

**Spec:** `docs/superpowers/specs/2026-03-25-license-key-redemption-design.md`

---

## File Structure

### Backend (`api/`)

| File | Action | Responsibility |
|------|--------|---------------|
| `model.go` | Modify | Add `Code`, `Source`, `Note` fields to LicenseKey |
| `type.go` | Modify | Add `CreateLicenseKeysRequest`, update response types to include `code`/`source`/`note` |
| `response.go` | Modify | Add `ErrorLicenseKeyAlreadyRedeemed` error code (400011) |
| `logic_license_key.go` | Modify | Add `GenerateShortCode()`, `CreateManualLicenseKeys()`, update `GenerateLicenseKeysForCampaign()` to generate codes, update `RedeemLicenseKey()` to accept code |
| `api_license_key.go` | Modify | Change public endpoints from `:uuid` to `code/:code` |
| `api_admin_license_key.go` | Modify | Add `POST /app/license-keys` handler |
| `worker_license_key.go` | Modify | Update email links from `/redeem/{uuid}` to `/g/{code}` |
| `route.go` | Modify | Update public routes, add admin creation route |

### Web Frontend (`web/`)

| File | Action | Responsibility |
|------|--------|---------------|
| `web/src/lib/api.ts` | Modify | Update types + methods for code-based API |
| `web/src/app/[locale]/g/page.tsx` | Create | `/g` landing page (SSR shell) |
| `web/src/app/[locale]/g/GiftCodeClient.tsx` | Create | Client component: input box + lookup + redeem |
| `web/src/app/[locale]/g/[code]/page.tsx` | Create | `/g/[code]` direct link page (SSR prefetch) |
| `web/src/app/[locale]/g/[code]/RedeemClient.tsx` | Create | Client component: display key info + redeem button |
| `web/src/app/(manager)/manager/license-keys/page.tsx` | Modify | Add create button, bulk copy, code/source/note columns |

### Cleanup

| File | Action |
|------|--------|
| `web/src/app/[locale]/redeem/` | Delete entire directory |

---

## Task 1: Backend — Model + Error Code Changes

**Files:**
- Modify: `api/model.go:1062-1085` (LicenseKey struct)
- Modify: `api/type.go:980-1011` (response types)
- Modify: `api/response.go:38-41` (error codes)

- [ ] **Step 1: Add new fields to LicenseKey model**

In `api/model.go`, update the `LicenseKey` struct to add `Code`, `Source`, `Note` after the `UUID` field:

```go
type LicenseKey struct {
	ID        uint64         `gorm:"primarykey" json:"id"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	DeletedAt gorm.DeletedAt `gorm:"index"`

	UUID string `gorm:"type:varchar(50);uniqueIndex;not null" json:"uuid"`
	Code string `gorm:"type:varchar(8)" json:"code"` // Initially nullable for migration; after backfill, update to uniqueIndex;not null

	Source string `gorm:"type:varchar(16);not null;default:'campaign'" json:"source"` // "campaign" or "manual"
	Note   string `gorm:"type:varchar(255)" json:"note"`

	PlanDays int `gorm:"not null;default:30" json:"planDays"`

	RecipientMatcher string `gorm:"type:varchar(50);not null" json:"recipientMatcher"`
	ExpiresAt        int64  `gorm:"not null" json:"expiresAt"`

	CampaignID *uint64 `gorm:"index" json:"campaignId"`

	CreatedByUserID *uint64    `gorm:"index" json:"createdByUserId"`
	IsUsed          bool       `gorm:"default:false" json:"isUsed"`
	UsedByUserID    *uint64    `gorm:"index" json:"usedByUserId"`
	UsedAt          *time.Time `json:"usedAt"`
}
```

- [ ] **Step 2: Add error code for "already redeemed another key"**

In `api/response.go`, after `ErrorLicenseKeyNotMatch`:

```go
ErrorLicenseKeyAlreadyRedeemed ErrorCode = 400011 // 用户已使用过授权码
```

And in the `var` block add:

```go
ErrLicenseKeyAlreadyRedeemed = errors.New("user already redeemed a license key")
```

- [ ] **Step 3: Update response types in `api/type.go`**

Update `LicenseKeyResponse` to include new fields:

```go
type LicenseKeyResponse struct {
	ID               uint64     `json:"id"`
	UUID             string     `json:"uuid"`
	Code             string     `json:"code"`
	Source           string     `json:"source"`
	Note             string     `json:"note"`
	PlanDays         int        `json:"planDays"`
	RecipientMatcher string     `json:"recipientMatcher"`
	ExpiresAt        int64      `json:"expiresAt"`
	CampaignID       *uint64    `json:"campaignId"`
	CreatedByUserID  *uint64    `json:"createdByUserId"`
	IsUsed           bool       `json:"isUsed"`
	UsedByUserID     *uint64    `json:"usedByUserId"`
	UsedAt           *int64     `json:"usedAt"`
	CreatedAt        int64      `json:"createdAt"`
}
```

Update `LicenseKeyPublicResponse` to use `code` instead of `uuid`:

```go
type LicenseKeyPublicResponse struct {
	Code       string `json:"code"`
	PlanDays   int    `json:"planDays"`
	ExpiresAt  int64  `json:"expiresAt"`
	IsUsed     bool   `json:"isUsed"`
	IsExpired  bool   `json:"isExpired"`
	SenderName string `json:"senderName"`
}
```

Add the new request/response types:

```go
type CreateLicenseKeysRequest struct {
	Count            int    `json:"count" binding:"required,min=1,max=100"`
	PlanDays         int    `json:"planDays" binding:"required,min=1"`
	ExpiresInDays    int    `json:"expiresInDays" binding:"required,min=1"`
	RecipientMatcher string `json:"recipientMatcher" binding:"required,oneof=all never_paid"`
	Note             string `json:"note"`
}

type CreateLicenseKeysResponse struct {
	Keys []LicenseKeyBrief `json:"keys"`
}

type LicenseKeyBrief struct {
	ID        uint64 `json:"id"`
	Code      string `json:"code"`
	PlanDays  int    `json:"planDays"`
	ExpiresAt int64  `json:"expiresAt"`
}
```

- [ ] **Step 4: Update `toLicenseKeyResponse` helper in `api/api_admin_license_key.go`**

Add the new fields to the helper function:

```go
func toLicenseKeyResponse(k *LicenseKey) LicenseKeyResponse {
	resp := LicenseKeyResponse{
		ID:               k.ID,
		UUID:             k.UUID,
		Code:             k.Code,
		Source:           k.Source,
		Note:             k.Note,
		PlanDays:         k.PlanDays,
		RecipientMatcher: k.RecipientMatcher,
		ExpiresAt:        k.ExpiresAt,
		CampaignID:       k.CampaignID,
		CreatedByUserID:  k.CreatedByUserID,
		IsUsed:           k.IsUsed,
		UsedByUserID:     k.UsedByUserID,
		CreatedAt:        k.CreatedAt.Unix(),
	}
	if k.UsedAt != nil {
		usedAt := k.UsedAt.Unix()
		resp.UsedAt = &usedAt
	}
	return resp
}
```

- [ ] **Step 5: Verify compilation**

Run: `cd api && go build ./...`
Expected: SUCCESS

- [ ] **Step 6: Commit**

```bash
git add api/model.go api/type.go api/response.go api/api_admin_license_key.go
git commit -m "feat(api): add Code/Source/Note fields to LicenseKey model"
```

---

## Task 2: Backend — Short Code Generation + Manual Creation Logic

**Files:**
- Modify: `api/logic_license_key.go`

- [ ] **Step 1: Add Crockford Base32 code generation function**

At the top of `api/logic_license_key.go`, add imports and the code generator:

```go
import (
	"crypto/rand"
	"math/big"
	"strings"
	// ... existing imports ...
)

// Crockford Base32 alphabet — excludes I, L, O, U to avoid visual confusion.
const crockfordAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// GenerateShortCode generates a unique 8-char Crockford Base32 code.
// Retries up to 10 times on collision.
func GenerateShortCode(ctx context.Context) (string, error) {
	for attempt := 0; attempt < 10; attempt++ {
		code := make([]byte, 8)
		for i := range code {
			n, err := rand.Int(rand.Reader, big.NewInt(32))
			if err != nil {
				return "", fmt.Errorf("failed to generate random byte: %w", err)
			}
			code[i] = crockfordAlphabet[n.Int64()]
		}
		codeStr := string(code)

		// Check uniqueness
		var count int64
		if err := db.Get().Model(&LicenseKey{}).Where("code = ?", codeStr).Count(&count).Error; err != nil {
			return "", fmt.Errorf("failed to check code uniqueness: %w", err)
		}
		if count == 0 {
			return codeStr, nil
		}
		log.Warnf(ctx, "[LICENSE_KEY] code collision on attempt %d: %s", attempt+1, codeStr)
	}
	return "", fmt.Errorf("failed to generate unique code after 10 attempts")
}

// NormalizeCode normalizes user input to uppercase for lookup.
func NormalizeCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}
```

- [ ] **Step 2: Add `CreateManualLicenseKeys` function**

```go
// CreateManualLicenseKeys creates license keys without a campaign.
func CreateManualLicenseKeys(ctx context.Context, req *CreateLicenseKeysRequest) ([]LicenseKey, error) {
	expiresAt := time.Now().AddDate(0, 0, req.ExpiresInDays).Unix()
	keys := make([]LicenseKey, 0, req.Count)

	for i := 0; i < req.Count; i++ {
		code, err := GenerateShortCode(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to generate code for key %d: %w", i+1, err)
		}
		keys = append(keys, LicenseKey{
			UUID:             xid.New().String(),
			Code:             code,
			Source:           "manual",
			Note:             req.Note,
			PlanDays:         req.PlanDays,
			RecipientMatcher: req.RecipientMatcher,
			ExpiresAt:        expiresAt,
		})
	}

	// Batch insert
	if err := db.Get().CreateInBatches(&keys, 100).Error; err != nil {
		return nil, fmt.Errorf("failed to batch insert license keys: %w", err)
	}

	log.Infof(ctx, "[LICENSE_KEY] created %d manual keys (planDays=%d, expires=%d, matcher=%s)",
		len(keys), req.PlanDays, req.ExpiresInDays, req.RecipientMatcher)
	return keys, nil
}
```

- [ ] **Step 3: Update `GenerateLicenseKeysForCampaign` to generate codes**

In the existing loop inside `GenerateLicenseKeysForCampaign`, update key creation to include `Code` and `Source`:

Find the section where keys are appended (around line 180-190) and add code generation:

```go
code, err := GenerateShortCode(ctx)
if err != nil {
	return 0, fmt.Errorf("failed to generate code: %w", err)
}
keys = append(keys, LicenseKey{
	UUID:             xid.New().String(),
	Code:             code,
	Source:           "campaign",
	PlanDays:         licenseKeyTTLDays,
	RecipientMatcher: "never_paid",
	ExpiresAt:        time.Now().AddDate(0, 0, licenseKeyTTLDays).Unix(),
	CampaignID:       &campaign.ID,
	CreatedByUserID:  &userID,
})
```

- [ ] **Step 4: Rewrite `RedeemLicenseKey` to use code instead of UUID**

Replace the entire `RedeemLicenseKey` function (lines 34-103 of `api/logic_license_key.go`):

```go
// RedeemLicenseKey validates, consumes, and grants plan access to the user.
// Runs inside a DB transaction.
func RedeemLicenseKey(ctx context.Context, code string, userID uint64) (*LicenseKey, *UserProHistory, error) {
	code = NormalizeCode(code)
	var history *UserProHistory
	var key *LicenseKey

	err := db.Get().Transaction(func(tx *gormdb.DB) error {
		// 1. Load key
		var k LicenseKey
		if err := tx.Where("code = ?", code).First(&k).Error; err != nil {
			return ErrLicenseKeyNotFound
		}
		if k.IsUsed {
			return ErrLicenseKeyUsed
		}
		if k.IsExpired() {
			return ErrLicenseKeyExpired
		}

		// 2. Anti-abuse: one key per user ever
		var existingCount int64
		if err := tx.Model(&LicenseKey{}).Where("used_by_user_id = ?", userID).Count(&existingCount).Error; err != nil {
			return err
		}
		if existingCount > 0 {
			return ErrLicenseKeyAlreadyRedeemed
		}

		// 3. Load user
		var user User
		if err := tx.First(&user, userID).Error; err != nil {
			return err
		}

		// 4. Check eligibility
		if !MatchLicenseKey(&k, &user) {
			return ErrLicenseKeyNotMatch
		}

		// 5. Atomic consume
		result := tx.Model(&LicenseKey{}).
			Where("code = ? AND is_used = false", code).
			Updates(map[string]any{
				"is_used":         true,
				"used_by_user_id": userID,
				"used_at":         time.Now(),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrLicenseKeyUsed
		}

		// 6. Grant plan days
		reason := fmt.Sprintf("礼物码兑换 - %s", k.Code)
		h, err := addProExpiredDays(ctx, tx, &user, VipSystemGrant, k.ID, k.PlanDays, reason)
		if err != nil {
			return err
		}

		k.IsUsed = true
		k.UsedByUserID = &userID
		key = &k
		history = h
		return nil
	})

	return key, history, err
}
```

- [ ] **Step 5: Update `ConsumeLicenseKey` to use code + correct error**

Replace `ConsumeLicenseKey` (lines 114-156) — update `uuid` param to `code`, queries from `Where("uuid = ?"...)` to `Where("code = ?"...)`, and fix the anti-abuse error from `ErrLicenseKeyNotMatch` to `ErrLicenseKeyAlreadyRedeemed`:

```go
// ConsumeLicenseKey atomically marks a key as used.
// Uses conditional UPDATE to prevent concurrent double-redemption.
func ConsumeLicenseKey(ctx context.Context, tx *gormdb.DB, code string, userID uint64) (*LicenseKey, error) {
	code = NormalizeCode(code)
	var key LicenseKey
	if err := tx.Where("code = ?", code).First(&key).Error; err != nil {
		return nil, ErrLicenseKeyNotFound
	}
	if key.IsUsed {
		return nil, ErrLicenseKeyUsed
	}
	if key.IsExpired() {
		return nil, ErrLicenseKeyExpired
	}

	// 同一用户只能使用一个 LicenseKey
	var existingUseCount int64
	if err := tx.Model(&LicenseKey{}).Where("used_by_user_id = ?", userID).Count(&existingUseCount).Error; err != nil {
		return nil, err
	}
	if existingUseCount > 0 {
		return nil, ErrLicenseKeyAlreadyRedeemed
	}

	now := time.Now()
	result := tx.Model(&LicenseKey{}).
		Where("code = ? AND is_used = false", code).
		Updates(map[string]any{
			"is_used":         true,
			"used_by_user_id": userID,
			"used_at":         now,
		})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrLicenseKeyUsed
	}

	key.IsUsed = true
	key.UsedByUserID = &userID
	key.UsedAt = &now
	return &key, nil
}
```

- [ ] **Step 6: Update `GetLicenseKeyByUUID` → `GetLicenseKeyByCode`**

Rename and update query:

```go
// GetLicenseKeyByCode fetches a key by its short code.
func GetLicenseKeyByCode(ctx context.Context, code string) (*LicenseKey, error) {
	code = NormalizeCode(code)
	var key LicenseKey
	if err := db.Get().Where("code = ?", code).First(&key).Error; err != nil {
		return nil, err
	}
	return &key, nil
}
```

- [ ] **Step 5: Verify compilation**

Run: `cd api && go build ./...`
Expected: SUCCESS

- [ ] **Step 6: Commit**

```bash
git add api/logic_license_key.go
git commit -m "feat(api): add Crockford Base32 code generation and manual key creation"
```

---

## Task 3: Backend — API Endpoints

**Files:**
- Modify: `api/api_license_key.go` (public endpoints)
- Modify: `api/api_admin_license_key.go` (admin create endpoint)
- Modify: `api/route.go` (route registration)

- [ ] **Step 1: Update public endpoints in `api/api_license_key.go`**

Change `api_get_license_key` to lookup by code:

```go
func api_get_license_key(c *gin.Context) {
	code := NormalizeCode(c.Param("code"))
	if code == "" {
		Error(c, ErrorLicenseKeyNotFound, "not found")
		return
	}

	var key LicenseKey
	if err := db.Get().Where("code = ?", code).First(&key).Error; err != nil {
		Error(c, ErrorLicenseKeyNotFound, "not found")
		return
	}

	resp := LicenseKeyPublicResponse{
		Code:      key.Code,
		PlanDays:  key.PlanDays,
		ExpiresAt: key.ExpiresAt,
		IsUsed:    key.IsUsed,
		IsExpired: key.IsExpired(),
	}

	if key.CreatedByUserID != nil {
		var sender User
		if err := db.Get().Preload("LoginIdentifies").First(&sender, *key.CreatedByUserID).Error; err == nil {
			email := getUserEmailFromIdentifies(&sender)
			if email != "" {
				resp.SenderName = hideEmail(email)
			}
		}
	}

	Success(c, &resp)
}
```

Change `api_redeem_license_key` to use code:

```go
func api_redeem_license_key(c *gin.Context) {
	code := NormalizeCode(c.Param("code"))
	userID := ReqUserID(c)

	key, history, err := RedeemLicenseKey(c, code, userID)
	if err != nil {
		switch {
		case errors.Is(err, ErrLicenseKeyNotFound):
			Error(c, ErrorLicenseKeyNotFound, "not found")
		case errors.Is(err, ErrLicenseKeyUsed):
			Error(c, ErrorLicenseKeyUsed, "already used")
		case errors.Is(err, ErrLicenseKeyExpired):
			Error(c, ErrorLicenseKeyExpired, "expired")
		case errors.Is(err, ErrLicenseKeyNotMatch):
			Error(c, ErrorLicenseKeyNotMatch, "not eligible")
		case errors.Is(err, ErrLicenseKeyAlreadyRedeemed):
			Error(c, ErrorLicenseKeyAlreadyRedeemed, "already redeemed another key")
		default:
			Error(c, ErrorSystemError, "redeem failed")
		}
		return
	}

	// Reload user to get updated ExpiredAt (same pattern as existing handler)
	var updatedUser User
	if err := db.Get().First(&updatedUser, userID).Error; err != nil {
		Error(c, ErrorSystemError, "failed to reload user")
		return
	}

	Success(c, gin.H{
		"planDays":    key.PlanDays,
		"newExpireAt": updatedUser.ExpiredAt,
		"historyId":   history.ID,
	})
}
```

- [ ] **Step 2: Add admin manual creation handler in `api/api_admin_license_key.go`**

```go
func api_admin_create_license_keys(c *gin.Context) {
	var req CreateLicenseKeysRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidParams, err.Error())
		return
	}

	keys, err := CreateManualLicenseKeys(c, &req)
	if err != nil {
		Error(c, ErrorSystemError, err.Error())
		return
	}

	briefs := make([]LicenseKeyBrief, len(keys))
	for i, k := range keys {
		briefs[i] = LicenseKeyBrief{
			ID:        k.ID,
			Code:      k.Code,
			PlanDays:  k.PlanDays,
			ExpiresAt: k.ExpiresAt,
		}
	}

	Success(c, &CreateLicenseKeysResponse{Keys: briefs})
}
```

- [ ] **Step 3: Add filter by `source` in `api_admin_list_license_keys`**

In the existing list handler, add source filter alongside existing `campaignId` and `isUsed` filters:

```go
if source := c.Query("source"); source != "" {
	query = query.Where("source = ?", source)
}
```

- [ ] **Step 4: Update routes in `api/route.go`**

Public routes — replace UUID-based with code-based. Note: rate limiting on the GET endpoint is deferred (address space of 32^8 is large enough, and the endpoint returns generic 404 for invalid codes).

```go
api.GET("/license-keys/code/:code", api_get_license_key)
api.POST("/license-keys/code/:code/redeem", AuthRequired(), api_redeem_license_key)
```

Admin routes — add creation endpoint:

```go
admin.POST("/license-keys", api_admin_create_license_keys)
```

Remove old routes:
```go
// DELETE these lines:
// api.GET("/license-keys/:uuid", api_get_license_key)
// api.POST("/license-keys/:uuid/redeem", AuthRequired(), api_redeem_license_key)
```

- [ ] **Step 5: Verify compilation**

Run: `cd api && go build ./...`
Expected: SUCCESS

- [ ] **Step 6: Commit**

```bash
git add api/api_license_key.go api/api_admin_license_key.go api/route.go
git commit -m "feat(api): code-based license key endpoints + admin manual creation"
```

---

## Task 4: Backend — Update Email Links

**Files:**
- Modify: `api/worker_license_key.go`

- [ ] **Step 1: Update email template links**

In `sendGiftEmail`, change the link format from:

```go
fmt.Sprintf("%s/redeem/%s", licenseKeyBaseURL, key.UUID)
```

to:

```go
fmt.Sprintf("%s/g/%s", licenseKeyBaseURL, key.Code)
```

- [ ] **Step 2: Verify compilation**

Run: `cd api && go build ./...`
Expected: SUCCESS

- [ ] **Step 3: Commit**

```bash
git add api/worker_license_key.go
git commit -m "feat(api): update license key email links to /g/{code}"
```

---

## Task 5: Backend — Migration for Existing Keys

**Files:**
- Modify: `api/migrate.go` (GORM AutoMigrate handles new columns)

- [ ] **Step 1: Verify AutoMigrate covers new fields**

GORM `AutoMigrate` automatically adds new columns. The `LicenseKey` struct in `model.go` already has the new fields. Check that `&LicenseKey{}` is in the `AutoMigrate` call in `api/migrate.go` (it should already be there from the initial implementation).

- [ ] **Step 2: Backfill existing keys + add unique index**

The `Code` field in Task 1 was set to `gorm:"type:varchar(8)"` (nullable, no index) so AutoMigrate can add the column without failing on existing rows.

Add a one-time migration function in `api/migrate.go`, called after AutoMigrate:

```go
func backfillLicenseKeyCodes(ctx context.Context) error {
	var keys []LicenseKey
	if err := db.Get().Where("code = '' OR code IS NULL").Find(&keys).Error; err != nil {
		return err
	}
	if len(keys) == 0 {
		return nil
	}
	log.Infof(ctx, "[MIGRATE] backfilling %d license keys with short codes", len(keys))
	for i := range keys {
		code, err := GenerateShortCode(ctx)
		if err != nil {
			return fmt.Errorf("failed to generate code for key %d: %w", keys[i].ID, err)
		}
		if err := db.Get().Model(&keys[i]).Update("code", code).Error; err != nil {
			return fmt.Errorf("failed to update key %d with code: %w", keys[i].ID, err)
		}
	}
	log.Infof(ctx, "[MIGRATE] backfilled %d license keys with short codes", len(keys))
	return nil
}
```

Call `backfillLicenseKeyCodes(ctx)` after AutoMigrate in the migration function.

- [ ] **Step 3: Update model tag to final form**

After backfill, update the `Code` gorm tag in `api/model.go` to the final form:

```go
Code string `gorm:"type:varchar(8);uniqueIndex;not null" json:"code"`
```

Run AutoMigrate again (next deploy) — GORM will add the unique index and NOT NULL constraint now that all rows have codes.

- [ ] **Step 3: Set Source default for existing rows**

Existing rows will get `source = 'campaign'` from the column default. No additional migration needed.

- [ ] **Step 4: Verify compilation**

Run: `cd api && go build ./...`
Expected: SUCCESS

- [ ] **Step 5: Commit**

```bash
git add api/migrate.go api/model.go
git commit -m "feat(api): add license key code backfill migration"
```

---

## Task 6: Web — API Client Types + Methods

**Files:**
- Modify: `web/src/lib/api.ts`

- [ ] **Step 1: Update TypeScript types**

Update `LicenseKeyPublic`:

```typescript
export interface LicenseKeyPublic {
  code: string;
  planDays: number;
  expiresAt: number;
  isUsed: boolean;
  isExpired: boolean;
  senderName: string;
}
```

Update `LicenseKeyAdmin` to include new fields:

```typescript
export interface LicenseKeyAdmin {
  id: number;
  uuid: string;
  code: string;
  source: string;
  note: string;
  planDays: number;
  recipientMatcher: string;
  expiresAt: number;
  campaignId?: number;
  createdByUserId?: number;
  isUsed: boolean;
  usedByUserId?: number;
  usedAt?: number;
  createdAt: number;
}
```

Add new types:

```typescript
export interface CreateLicenseKeysRequest {
  count: number;
  planDays: number;
  expiresInDays: number;
  recipientMatcher: string;
  note?: string;
}

export interface CreateLicenseKeysResponse {
  keys: { id: number; code: string; planDays: number; expiresAt: number }[];
}
```

- [ ] **Step 2: Update API methods**

Change `getLicenseKey` and `redeemLicenseKey` to use code:

```typescript
async getLicenseKey(code: string): Promise<LicenseKeyPublic> {
  return this.request<LicenseKeyPublic>(`/api/license-keys/code/${code}`);
}

async redeemLicenseKey(code: string): Promise<{ planDays: number; newExpireAt: number; historyId: number }> {
  return this.request<{ planDays: number; newExpireAt: number; historyId: number }>(
    `/api/license-keys/code/${code}/redeem`,
    { method: 'POST' }
  );
}
```

Add admin creation method:

```typescript
async createAdminLicenseKeys(req: CreateLicenseKeysRequest): Promise<CreateLicenseKeysResponse> {
  return this.request<CreateLicenseKeysResponse>('/app/license-keys', {
    method: 'POST',
    body: JSON.stringify(req),
  });
}
```

Update `listAdminLicenseKeys` to accept `source` filter:

```typescript
async listAdminLicenseKeys(params: {
  page?: number;
  pageSize?: number;
  campaignId?: number;
  isUsed?: boolean;
  source?: string;
}): Promise<{ items: LicenseKeyAdmin[]; total: number }>
```

Add `source` to the query params construction.

- [ ] **Step 3: Verify build**

Run: `cd web && yarn build`
Expected: SUCCESS (may have type errors in pages that reference old types — those are fixed in later tasks)

- [ ] **Step 4: Commit**

```bash
git add web/src/lib/api.ts
git commit -m "feat(web): update license key API types and methods for code-based flow"
```

---

## Task 7: Web — `/g/[code]` Direct Link Page

**Files:**
- Create: `web/src/app/[locale]/g/[code]/page.tsx`
- Create: `web/src/app/[locale]/g/[code]/RedeemClient.tsx`

- [ ] **Step 1: Create server component `page.tsx`**

Follow exact pattern from `web/src/app/[locale]/redeem/[uuid]/page.tsx` — SSR fetch, Header/Footer, setRequestLocale:

```tsx
import { api } from '@/lib/api';
import { notFound } from 'next/navigation';
import { routing } from '@/i18n/routing';
import { setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import Header from '@/components/Header';
import Footer from '@/components/Footer';
import RedeemClient from './RedeemClient';

type Locale = (typeof routing.locales)[number];

export async function generateMetadata(): Promise<Metadata> {
  return { title: '兑换授权码 | Kaitu' };
}

export default async function GiftCodeDirectPage({
  params,
}: {
  params: Promise<{ code: string; locale: string }>;
}) {
  const { code, locale: rawLocale } = await params;
  const locale = rawLocale as Locale;
  setRequestLocale(locale);

  let key = null;
  try {
    key = await api.getLicenseKey(code);
  } catch {
    notFound();
  }

  return (
    <div className="min-h-screen bg-background">
      <Header />
      <RedeemClient initialKey={key} code={code} />
      <Footer />
    </div>
  );
}
```

- [ ] **Step 2: Create client component `RedeemClient.tsx`**

Adapted from existing `web/src/app/[locale]/redeem/[uuid]/RedeemClient.tsx` — same UI, but uses `code` prop and adds `ErrorLicenseKeyAlreadyRedeemed` (400011) handling.

**Auth flow**: User clicks "兑换" → `api.redeemLicenseKey(code)` → if 401, `api.request()` auto-emits `auth:unauthorized` → `AuthContext` calls `redirectToLogin()` → user redirected to `/login?next=/g/{code}` → after login, browser returns to `/g/{code}` → user clicks again → success. No explicit auth check needed in the component.

```tsx
'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { Link } from '@/i18n/routing';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Gift, AlertCircle, Clock, CheckCircle } from 'lucide-react';
import type { LicenseKeyPublic } from '@/lib/api';
import { api, ApiError } from '@/lib/api';

const ErrorLicenseKeyNotFound = 400007;
const ErrorLicenseKeyUsed = 400008;
const ErrorLicenseKeyExpired = 400009;
const ErrorLicenseKeyNotMatch = 400010;
const ErrorLicenseKeyAlreadyRedeemed = 400011;

interface RedeemClientProps {
  initialKey: LicenseKeyPublic | null;
  code: string;
}

function getDaysRemaining(expiresAt: number): number {
  const now = Math.floor(Date.now() / 1000);
  const diff = expiresAt - now;
  return Math.max(0, Math.ceil(diff / 86400));
}

export default function RedeemClient({ initialKey, code }: RedeemClientProps) {
  const t = useTranslations('licenseKeys');
  const [redeemState, setRedeemState] = useState<'idle' | 'loading' | 'success' | 'error'>('idle');
  const [redeemDays, setRedeemDays] = useState<number>(0);
  const [errorKey, setErrorKey] = useState<string>('');

  // Used state
  if (initialKey?.isUsed) {
    return (
      <div className="max-w-lg mx-auto px-4 py-20 text-center">
        <Card className="p-10 border-muted bg-muted/20">
          <AlertCircle className="w-14 h-14 text-muted-foreground mx-auto mb-4" />
          <h1 className="text-2xl font-bold text-foreground mb-3">{t('gift.used')}</h1>
          <p className="text-muted-foreground mb-8">{t('gift.subtitle')}</p>
          <Button asChild variant="outline" size="lg">
            <Link href="/purchase">{t('gift.fallback')}</Link>
          </Button>
        </Card>
      </div>
    );
  }

  // Expired state
  if (initialKey?.isExpired) {
    return (
      <div className="max-w-lg mx-auto px-4 py-20 text-center">
        <Card className="p-10 border-muted bg-muted/20">
          <Clock className="w-14 h-14 text-muted-foreground mx-auto mb-4" />
          <h1 className="text-2xl font-bold text-foreground mb-3">{t('gift.expired')}</h1>
          <p className="text-muted-foreground mb-8">{t('gift.subtitle')}</p>
          <Button asChild variant="outline" size="lg">
            <Link href="/purchase">{t('gift.fallback')}</Link>
          </Button>
        </Card>
      </div>
    );
  }

  // Success state
  if (redeemState === 'success') {
    return (
      <div className="max-w-lg mx-auto px-4 py-20 text-center">
        <Card className="p-10 border-primary/30 bg-primary/5">
          <CheckCircle className="w-14 h-14 text-primary mx-auto mb-4" />
          <h1 className="text-2xl font-bold text-foreground mb-3">{t('gift.successTitle')}</h1>
          <p className="text-muted-foreground mb-8">{t('gift.successBody', { days: redeemDays })}</p>
          <Button asChild size="lg">
            <Link href="/account">{t('gift.viewAccount')}</Link>
          </Button>
        </Card>
      </div>
    );
  }

  const key = initialKey;
  if (!key) return null;

  const daysRemaining = getDaysRemaining(key.expiresAt);

  const handleRedeem = async () => {
    setRedeemState('loading');
    setErrorKey('');
    try {
      const result = await api.redeemLicenseKey(code);
      setRedeemDays(result.planDays);
      setRedeemState('success');
    } catch (err) {
      if (err instanceof ApiError) {
        const errCode = err.code as number;
        switch (errCode) {
          case ErrorLicenseKeyUsed:
            setErrorKey('gift.used');
            break;
          case ErrorLicenseKeyExpired:
            setErrorKey('gift.expired');
            break;
          case ErrorLicenseKeyNotFound:
          case ErrorLicenseKeyNotMatch:
            setErrorKey('gift.notEligible');
            break;
          case ErrorLicenseKeyAlreadyRedeemed:
            setErrorKey('gift.alreadyRedeemed');
            break;
          default:
            setErrorKey('gift.redeemFailed');
        }
      } else {
        setErrorKey('gift.redeemFailed');
      }
      setRedeemState('error');
    }
  };

  return (
    <div className="max-w-lg mx-auto px-4 py-16 sm:py-24">
      <div className="text-center mb-10">
        <div className="inline-flex items-center justify-center w-20 h-20 rounded-full bg-primary/10 border border-primary/20 mb-6">
          <Gift className="w-10 h-10 text-primary" />
        </div>
        <h1 className="text-2xl sm:text-3xl font-bold text-foreground leading-snug">
          {key.senderName ? t('gift.title', { name: key.senderName }) : t('gift.titleAnonymous')}
        </h1>
        <p className="mt-2 text-muted-foreground">{t('gift.subtitle')}</p>
      </div>

      <Card className="p-8 sm:p-10 mb-8 border-primary/30 bg-primary/5 text-center">
        <p className="text-sm uppercase tracking-widest text-primary mb-4 font-mono">Kaitu VPN</p>
        <div className="text-5xl sm:text-6xl font-mono font-bold text-primary mb-4">
          {t('gift.planDays', { days: key.planDays })}
        </div>
        <div className="flex items-center justify-center gap-1.5 text-sm text-muted-foreground">
          <Clock className="w-4 h-4" />
          <span>{t('gift.expires', { days: daysRemaining })}</span>
        </div>
      </Card>

      {redeemState === 'error' && errorKey && (
        <p className="text-sm text-destructive text-center mb-4">
          {t(errorKey as Parameters<typeof t>[0])}
        </p>
      )}

      <div className="text-center">
        <Button
          size="lg"
          className="w-full sm:w-auto px-12 py-6 text-lg font-bold"
          onClick={handleRedeem}
          disabled={redeemState === 'loading'}
        >
          {redeemState === 'loading' ? t('gift.loading') : t('gift.cta')}
        </Button>
      </div>
    </div>
  );
}
```

- [ ] **Step 3: Verify build**

Run: `cd web && yarn build`
Expected: SUCCESS

- [ ] **Step 4: Commit**

```bash
git add web/src/app/[locale]/g/[code]/
git commit -m "feat(web): add /g/[code] direct link redemption page"
```

---

## Task 8: Web — `/g` Landing Page (Manual Code Input)

**Files:**
- Create: `web/src/app/[locale]/g/page.tsx`
- Create: `web/src/app/[locale]/g/GiftCodeClient.tsx`

- [ ] **Step 1: Create server component `page.tsx`**

Follow pattern from invite landing `web/src/app/[locale]/s/[code]/page.tsx` — Header/Footer wrapper, setRequestLocale:

```tsx
import { setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import { routing } from '@/i18n/routing';
import Header from '@/components/Header';
import Footer from '@/components/Footer';
import GiftCodeClient from './GiftCodeClient';

type Locale = (typeof routing.locales)[number];

export async function generateMetadata(): Promise<Metadata> {
  return {
    title: '兑换授权码 | Kaitu',
    description: '输入授权码，免费获取 Kaitu 会员',
  };
}

export default async function GiftCodeLandingPage({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const { locale: rawLocale } = await params;
  setRequestLocale(rawLocale as Locale);

  return (
    <div className="min-h-screen bg-background">
      <Header />
      <GiftCodeClient />
      <Footer />
    </div>
  );
}
```

- [ ] **Step 2: Create client component `GiftCodeClient.tsx`**

Client-side only — no SSR data fetch. User enters code → lookup → display → redeem.

**Auth flow** same as Task 7: `api.redeemLicenseKey()` on 401 auto-redirects to `/login?next=/g` → user returns and re-enters code.

```tsx
'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { Link } from '@/i18n/routing';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { Gift, AlertCircle, Clock, CheckCircle, Loader2, Search } from 'lucide-react';
import type { LicenseKeyPublic } from '@/lib/api';
import { api, ApiError } from '@/lib/api';

const ErrorLicenseKeyNotFound = 400007;
const ErrorLicenseKeyUsed = 400008;
const ErrorLicenseKeyExpired = 400009;
const ErrorLicenseKeyNotMatch = 400010;
const ErrorLicenseKeyAlreadyRedeemed = 400011;

type PageState = 'idle' | 'looking' | 'found' | 'redeeming' | 'success' | 'error';

function getDaysRemaining(expiresAt: number): number {
  const now = Math.floor(Date.now() / 1000);
  return Math.max(0, Math.ceil((expiresAt - now) / 86400));
}

export default function GiftCodeClient() {
  const t = useTranslations('licenseKeys');
  const [inputCode, setInputCode] = useState('');
  const [state, setState] = useState<PageState>('idle');
  const [keyData, setKeyData] = useState<LicenseKeyPublic | null>(null);
  const [redeemDays, setRedeemDays] = useState(0);
  const [errorMsg, setErrorMsg] = useState('');

  const handleLookup = async () => {
    const code = inputCode.trim().toUpperCase();
    if (!code) return;
    setState('looking');
    setErrorMsg('');
    try {
      const data = await api.getLicenseKey(code);
      setKeyData(data);
      setState('found');
    } catch {
      setErrorMsg(t('landing.notFound'));
      setState('error');
    }
  };

  const handleRedeem = async () => {
    const code = inputCode.trim().toUpperCase();
    setState('redeeming');
    setErrorMsg('');
    try {
      const result = await api.redeemLicenseKey(code);
      setRedeemDays(result.planDays);
      setState('success');
    } catch (err) {
      if (err instanceof ApiError) {
        const errCode = err.code as number;
        switch (errCode) {
          case ErrorLicenseKeyUsed:
            setErrorMsg(t('gift.used'));
            break;
          case ErrorLicenseKeyExpired:
            setErrorMsg(t('gift.expired'));
            break;
          case ErrorLicenseKeyNotFound:
          case ErrorLicenseKeyNotMatch:
            setErrorMsg(t('gift.notEligible'));
            break;
          case ErrorLicenseKeyAlreadyRedeemed:
            setErrorMsg(t('gift.alreadyRedeemed'));
            break;
          default:
            setErrorMsg(t('gift.redeemFailed'));
        }
      } else {
        setErrorMsg(t('gift.redeemFailed'));
      }
      setState('error');
    }
  };

  const handleReset = () => {
    setInputCode('');
    setKeyData(null);
    setErrorMsg('');
    setState('idle');
  };

  // Success state
  if (state === 'success') {
    return (
      <div className="max-w-lg mx-auto px-4 py-20 text-center">
        <Card className="p-10 border-primary/30 bg-primary/5">
          <CheckCircle className="w-14 h-14 text-primary mx-auto mb-4" />
          <h1 className="text-2xl font-bold text-foreground mb-3">{t('gift.successTitle')}</h1>
          <p className="text-muted-foreground mb-8">{t('gift.successBody', { days: redeemDays })}</p>
          <Button asChild size="lg">
            <Link href="/account">{t('gift.viewAccount')}</Link>
          </Button>
        </Card>
      </div>
    );
  }

  // Found state — show key info + redeem button
  if (state === 'found' && keyData) {
    const daysRemaining = getDaysRemaining(keyData.expiresAt);

    // Key already used or expired
    if (keyData.isUsed) {
      return (
        <div className="max-w-lg mx-auto px-4 py-20 text-center">
          <Card className="p-10 border-muted bg-muted/20">
            <AlertCircle className="w-14 h-14 text-muted-foreground mx-auto mb-4" />
            <h1 className="text-2xl font-bold text-foreground mb-3">{t('gift.used')}</h1>
            <Button asChild variant="outline" size="lg" className="mt-4">
              <Link href="/purchase">{t('gift.fallback')}</Link>
            </Button>
          </Card>
        </div>
      );
    }
    if (keyData.isExpired) {
      return (
        <div className="max-w-lg mx-auto px-4 py-20 text-center">
          <Card className="p-10 border-muted bg-muted/20">
            <Clock className="w-14 h-14 text-muted-foreground mx-auto mb-4" />
            <h1 className="text-2xl font-bold text-foreground mb-3">{t('gift.expired')}</h1>
            <Button asChild variant="outline" size="lg" className="mt-4">
              <Link href="/purchase">{t('gift.fallback')}</Link>
            </Button>
          </Card>
        </div>
      );
    }

    return (
      <div className="max-w-lg mx-auto px-4 py-16 sm:py-24">
        <div className="text-center mb-10">
          <div className="inline-flex items-center justify-center w-20 h-20 rounded-full bg-primary/10 border border-primary/20 mb-6">
            <Gift className="w-10 h-10 text-primary" />
          </div>
          <h1 className="text-2xl sm:text-3xl font-bold text-foreground leading-snug">
            {keyData.senderName
              ? t('gift.title', { name: keyData.senderName })
              : t('gift.titleAnonymous')}
          </h1>
          <p className="mt-2 text-muted-foreground">{t('gift.subtitle')}</p>
        </div>

        <Card className="p-8 sm:p-10 mb-8 border-primary/30 bg-primary/5 text-center">
          <p className="text-sm uppercase tracking-widest text-primary mb-4 font-mono">Kaitu VPN</p>
          <div className="text-5xl sm:text-6xl font-mono font-bold text-primary mb-4">
            {t('gift.planDays', { days: keyData.planDays })}
          </div>
          <div className="flex items-center justify-center gap-1.5 text-sm text-muted-foreground">
            <Clock className="w-4 h-4" />
            <span>{t('gift.expires', { days: daysRemaining })}</span>
          </div>
        </Card>

        {errorMsg && (
          <p className="text-sm text-destructive text-center mb-4">{errorMsg}</p>
        )}

        <div className="flex flex-col items-center gap-3">
          <Button
            size="lg"
            className="w-full sm:w-auto px-12 py-6 text-lg font-bold"
            onClick={handleRedeem}
            disabled={state === 'redeeming'}
          >
            {state === 'redeeming' ? t('gift.loading') : t('gift.cta')}
          </Button>
          <button
            onClick={handleReset}
            className="text-sm text-muted-foreground underline underline-offset-4 hover:text-foreground"
          >
            {t('landing.reenter')}
          </button>
        </div>
      </div>
    );
  }

  // Idle / Error state — show input form
  return (
    <div className="max-w-lg mx-auto px-4 py-16 sm:py-24">
      <div className="text-center mb-10">
        <div className="inline-flex items-center justify-center w-20 h-20 rounded-full bg-primary/10 border border-primary/20 mb-6">
          <Gift className="w-10 h-10 text-primary" />
        </div>
        <h1 className="text-2xl sm:text-3xl font-bold text-foreground leading-snug">
          {t('landing.title')}
        </h1>
        <p className="mt-2 text-muted-foreground">{t('landing.subtitle')}</p>
      </div>

      <Card className="p-8 sm:p-10">
        <div className="flex gap-3">
          <Input
            value={inputCode}
            onChange={(e) => setInputCode(e.target.value.toUpperCase())}
            placeholder={t('landing.placeholder')}
            maxLength={8}
            className="font-mono text-lg tracking-widest uppercase text-center"
            onKeyDown={(e) => e.key === 'Enter' && handleLookup()}
          />
          <Button
            onClick={handleLookup}
            disabled={!inputCode.trim() || state === 'looking'}
            size="lg"
          >
            {state === 'looking' ? (
              <Loader2 className="w-4 h-4 animate-spin" />
            ) : (
              <Search className="w-4 h-4" />
            )}
          </Button>
        </div>

        {errorMsg && (
          <p className="text-sm text-destructive text-center mt-4">{errorMsg}</p>
        )}
      </Card>
    </div>
  );
}
```

- [ ] **Step 3: Verify build**

Run: `cd web && yarn build`
Expected: SUCCESS

- [ ] **Step 4: Commit**

```bash
git add web/src/app/[locale]/g/page.tsx web/src/app/[locale]/g/GiftCodeClient.tsx
git commit -m "feat(web): add /g landing page for manual code input"
```

---

## Task 9: Web — i18n Keys for New Pages

**Files:**
- Modify: `web/messages/zh-CN/licenseKeys.json`
- Modify: `web/messages/en-US/licenseKeys.json`
- Copy changes to: all other locale files (`ja`, `zh-TW`, `zh-HK`, `en-GB`, `en-AU`)

- [ ] **Step 1: Add new keys to `web/messages/zh-CN/licenseKeys.json`**

Add these keys inside the existing `gift` object and add a new `landing` section:

```json
{
  "gift": {
    "title": "{name} 送给你一个礼物",
    "titleAnonymous": "你收到一个礼物",
    "subtitle": "Kaitu VPN 专属会员",
    "planDays": "{days} 天会员",
    "expires": "还剩 {days} 天到期",
    "cta": "立即领取",
    "used": "此礼物已被领走",
    "expired": "此礼物已过期",
    "fallback": "查看新用户专属优惠",
    "loading": "兑换中...",
    "successTitle": "兑换成功！",
    "successBody": "已为你延长 {days} 天会员",
    "viewAccount": "查看我的账户",
    "notEligible": "不符合使用条件",
    "redeemFailed": "兑换失败，请重试",
    "alreadyRedeemed": "您已使用过授权码"
  },
  "landing": {
    "title": "兑换授权码",
    "subtitle": "输入授权码，免费获取会员",
    "placeholder": "输入 8 位授权码",
    "notFound": "授权码不存在",
    "reenter": "重新输入"
  },
  "admin": { ... },
  "campaigns": { ... }
}
```

New keys added: `gift.titleAnonymous`, `gift.alreadyRedeemed`, `landing.*` (5 keys).

- [ ] **Step 2: Add corresponding en-US keys**

```json
{
  "gift": {
    "titleAnonymous": "You received a gift",
    "alreadyRedeemed": "You have already redeemed a license key"
  },
  "landing": {
    "title": "Redeem Gift Code",
    "subtitle": "Enter your gift code to claim free membership",
    "placeholder": "Enter 8-digit code",
    "notFound": "Gift code not found",
    "reenter": "Enter a different code"
  }
}
```

- [ ] **Step 3: Copy to remaining locales**

Copy the zh-CN version to `zh-TW`, `zh-HK` (same text). Copy the en-US version to `en-GB`, `en-AU`. For `ja`, translate or use zh-CN as placeholder.

- [ ] **Step 4: Add `purchase.giftCodePrompt` key to all locales**

In `web/messages/zh-CN/purchase.json`, add:
```json
"giftCodePrompt": "已有授权码？",
"giftCodeLink": "点此兑换"
```

In `web/messages/en-US/purchase.json`, add:
```json
"giftCodePrompt": "Have a gift code?",
"giftCodeLink": "Redeem here"
```

Copy to all other locales accordingly.

- [ ] **Step 5: Verify build**

Run: `cd web && yarn build`
Expected: SUCCESS

- [ ] **Step 6: Commit**

```bash
git add web/messages/
git commit -m "feat(web): add i18n keys for gift code landing and redemption pages"
```

---

## Task 10: Web — Admin Page Updates

**Files:**
- Modify: `web/src/app/(manager)/manager/license-keys/page.tsx`

- [ ] **Step 1: Add "创建授权码" button + creation dialog**

Add a dialog component with form fields:
- Count: number input (1-100, default 10)
- Plan days: number input (default 30)
- Validity (days): number input (default 30)
- Recipient matcher: select (all / never_paid)
- Note: text input

On submit: call `api.createAdminLicenseKeys(req)`.

On success: show result dialog with:
- List of generated codes
- "批量复制" button (copies all codes joined by `\n` to clipboard)
- Individual copy button per code

- [ ] **Step 2: Update table columns**

Replace the UUID column (which shows first 8 chars) with full `Code` column + copy button.

Add `Source` column with badge:
- `manual` → "手动" (default badge)
- `campaign` → "活动" (secondary badge)

Add `Note` column (truncated with tooltip if long).

- [ ] **Step 3: Add source filter**

Add a source filter dropdown alongside existing campaignId and isUsed filters:
- Options: 全部 / 手动 (manual) / 活动 (campaign)

Pass `source` param to `api.listAdminLicenseKeys()`.

- [ ] **Step 4: Update stats to include source breakdown**

Update the stats display to show manual vs campaign counts if useful. (Optional — skip if stats API doesn't support source grouping yet.)

- [ ] **Step 5: Verify build**

Run: `cd web && yarn build`
Expected: SUCCESS

- [ ] **Step 6: Commit**

```bash
git add web/src/app/\(manager\)/manager/license-keys/page.tsx
git commit -m "feat(web): admin license key creation, code column, source filter"
```

---

## Task 11: Web — Purchase Page Entry Point

**Files:**
- Modify: `web/src/app/[locale]/purchase/PurchaseClient.tsx` (client component)

- [ ] **Step 1: Add gift code prompt**

In `PurchaseClient.tsx`, add a subtle prompt below the plan selection area. The component already uses `useTranslations()` and `Link` from `@/i18n/routing`.

Find an appropriate location (e.g., after the campaign code input section, around line 104-106 where `showCampaign` state is) and add:

```tsx
<p className="text-sm text-muted-foreground text-center mt-4">
  {t('purchase.giftCodePrompt')}
  <Link href="/g" className="text-primary underline underline-offset-4 hover:text-primary/80 ml-1">
    {t('purchase.giftCodeLink')}
  </Link>
</p>
```

Keep it subtle — should not compete with the purchase flow.

- [ ] **Step 2: Verify build**

Run: `cd web && yarn build`
Expected: SUCCESS

- [ ] **Step 3: Commit**

```bash
git add web/src/app/[locale]/purchase/PurchaseClient.tsx
git commit -m "feat(web): add gift code prompt on purchase page"
```

---

## Task 12: Cleanup — Remove Old Redeem Pages

**Files:**
- Delete: `web/src/app/[locale]/redeem/` (entire directory)

- [ ] **Step 1: Delete the old redeem directory**

```bash
rm -rf web/src/app/[locale]/redeem/
```

- [ ] **Step 2: Search for any remaining references to `/redeem`**

Search the codebase for references to the old `/redeem` path and update or remove them:

```bash
grep -r "/redeem" web/src/ --include="*.ts" --include="*.tsx"
```

Update any found references to use `/g` instead.

- [ ] **Step 3: Verify build**

Run: `cd web && yarn build`
Expected: SUCCESS

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "cleanup: remove old /redeem pages, replace with /g"
```

---

## Task 13: Final Verification

- [ ] **Step 1: Run backend tests**

```bash
cd api && go test ./...
```

Fix any test failures related to the license key changes (tests may reference old UUID-based functions).

- [ ] **Step 2: Run web build**

```bash
cd web && yarn build
```

- [ ] **Step 3: Run web tests**

```bash
cd web && yarn test
```

- [ ] **Step 4: Commit any test fixes**

```bash
git add -A
git commit -m "test: fix license key tests for code-based flow"
```
