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

- [ ] **Step 4: Update `RedeemLicenseKey` to use code instead of UUID**

Change the function signature and internal query:

```go
func RedeemLicenseKey(ctx context.Context, code string, userID uint64) (*LicenseKey, *UserProHistory, error) {
	code = NormalizeCode(code)
```

Update the key lookup query from `Where("uuid = ?", uuid)` to `Where("code = ?", code)`.

Update the atomic consume query from `Where("uuid = ? AND is_used = false", uuid)` to `Where("code = ? AND is_used = false", code)`.

Update the anti-abuse check to return `ErrLicenseKeyAlreadyRedeemed` instead of `ErrLicenseKeyUsed` when the user (not the key) has already redeemed another key.

Update the history reason from `"礼物码兑换 - " + uuid` to `"礼物码兑换 - " + code`.

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
		Error(c, ErrorInternal, err.Error())
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

Reference: Follow the pattern from `web/src/app/[locale]/redeem/[uuid]/page.tsx` and `RedeemClient.tsx`, but use `code` param instead of `uuid`.

- [ ] **Step 1: Create server component `page.tsx`**

```tsx
import { api } from "@/lib/api";
import { notFound } from "next/navigation";
import RedeemClient from "./RedeemClient";

interface Props {
  params: Promise<{ locale: string; code: string }>;
}

export async function generateMetadata({ params }: Props) {
  const { code } = await params;
  return {
    title: `兑换授权码 ${code} — Kaitu`,
  };
}

export default async function GiftCodePage({ params }: Props) {
  const { code } = await params;

  try {
    const keyData = await api.getLicenseKey(code);
    return <RedeemClient code={code} initialData={keyData} />;
  } catch {
    notFound();
  }
}
```

- [ ] **Step 2: Create client component `RedeemClient.tsx`**

Model after existing `web/src/app/[locale]/redeem/[uuid]/RedeemClient.tsx` but:
- Accept `code: string` prop instead of `uuid`
- Call `api.redeemLicenseKey(code)` instead of `api.redeemLicenseKey(uuid)`
- Add `ErrorLicenseKeyAlreadyRedeemed = 400011` error code handling → show "您已使用过授权码"
- Use same UI pattern: gift card display (sender, days, expiry countdown), redeem button, error/success states
- On "兑换" click when not logged in: show login prompt (check existing RedeemClient for the auth pattern)
- Success state: show granted days + link to `/account`
- Error states: used/expired/not-eligible/already-redeemed → fallback link to `/purchase`

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

Reference: Follow layout pattern from `/s/[code]` (invite landing page) — centered card, minimal design.

- [ ] **Step 1: Create server component `page.tsx`**

```tsx
import GiftCodeClient from "./GiftCodeClient";

export function generateMetadata() {
  return {
    title: "兑换授权码 — Kaitu",
    description: "输入授权码，免费获取 Kaitu 会员",
  };
}

export default function GiftCodeLandingPage() {
  return <GiftCodeClient />;
}
```

- [ ] **Step 2: Create client component `GiftCodeClient.tsx`**

States: `idle` → `loading` → `found` (show key info) → `redeeming` → `success` / `error`

```
idle:
  - Centered card with heading "兑换授权码"
  - Subtext: "输入授权码，免费获取会员"
  - Input field (uppercase transform on display, max 8 chars)
  - "查看" button

found:
  - Key info card (same as RedeemClient): plan days, expiry, sender
  - "兑换" button
  - "重新输入" link to go back to idle

success:
  - CheckCircle icon
  - "兑换成功" + plan days
  - Link to /account

error:
  - Error message (mapped from error code)
  - Link to /purchase
```

Flow:
1. User types code → click "查看" → `api.getLicenseKey(code.toUpperCase())`
2. On success: transition to `found` state, display key info
3. On 404: show "授权码不存在"
4. User clicks "兑换" → check auth state
   - Not logged in: redirect to login (use existing auth pattern from RedeemClient)
   - Logged in: call `api.redeemLicenseKey(code)` → success/error

- [ ] **Step 3: Verify build**

Run: `cd web && yarn build`
Expected: SUCCESS

- [ ] **Step 4: Commit**

```bash
git add web/src/app/[locale]/g/
git commit -m "feat(web): add /g landing page for manual code input"
```

---

## Task 9: Web — Admin Page Updates

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

## Task 10: Web — Purchase Page Entry Point

**Files:**
- Modify: `web/src/app/[locale]/purchase/page.tsx` or the client component it renders

- [ ] **Step 1: Add gift code prompt**

Find the appropriate location in the purchase page (above or below the plan selection area) and add a light prompt:

```tsx
<p className="text-sm text-muted-foreground text-center">
  已有授权码？
  <Link href="/g" className="text-primary underline underline-offset-4 hover:text-primary/80">
    点此兑换
  </Link>
</p>
```

Keep it subtle — should not compete with the purchase flow.

- [ ] **Step 2: Verify build**

Run: `cd web && yarn build`
Expected: SUCCESS

- [ ] **Step 3: Commit**

```bash
git add web/src/app/[locale]/purchase/
git commit -m "feat(web): add gift code prompt on purchase page"
```

---

## Task 11: Cleanup — Remove Old Redeem Pages

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

## Task 12: Final Verification

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
