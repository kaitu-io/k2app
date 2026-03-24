# License Key Redemption Flow Design

**Date**: 2026-03-25
**Goal**: Enable admin manual license key creation + user-facing redemption landing page for new user acquisition

## Background

Current license keys are tightly coupled to Campaigns — keys can only be batch-generated via `POST /app/campaigns/:id/issue-keys`. This limits usage to campaign-driven scenarios. Additionally, the user-facing redemption page (`/redeem/[uuid]`) uses long xid strings that are hard to share and type.

### Problems Solved

1. **No independent key creation** — Admin cannot create keys for customer service compensation, KOL/channel partnerships, or offline events without first creating a Campaign
2. **Poor shareability** — xid format (`ctb1234567890abcdef12345`) is too long for social sharing, offline print, or verbal communication
3. **No manual input entry** — Users who receive a code as text (not a link) have no page to enter it

## Design

### 1. Data Model Changes

**LicenseKey table — new fields:**

| Field | Type | Description |
|-------|------|-------------|
| `code` | `VARCHAR(8) UNIQUE NOT NULL` | User-facing short code (e.g., `K3MNTX7Q`). Uppercase, replaces uuid as user-visible identifier |
| `source` | `VARCHAR(16) NOT NULL DEFAULT 'campaign'` | Origin: `campaign` (batch from activity) or `manual` (admin created) |
| `note` | `VARCHAR(255)` | Admin memo (e.g., "客服补偿 #1234", "KOL@xxx 渠道") |

**Short code generation:**
- 8 characters, Crockford Base32 alphabet (`0-9 A-H J-N P-T V-Z`, excludes `I L O U`)
- Always stored and displayed as uppercase
- No case-sensitivity concerns — input normalized to uppercase before query
- 32^8 ≈ 1.1 trillion combinations, negligible collision probability
- Generated server-side with DB uniqueness check on insert

**Why Crockford Base32:** Avoids visually confusable characters (`0/O`, `1/I/l`). Critical for offline print, verbal communication, and manual typing scenarios.

**UUID column:** Retained for internal reference and database foreign keys. New user-facing code paths use `code` exclusively.

**Existing `/redeem/[uuid]` route:** Delete entirely (never published).

**Migration:** Existing keys (created via campaigns) backfilled with generated codes during migration. Script generates unique 8-char codes for all existing rows.

### 2. Admin Manual Creation

#### API

`POST /app/license-keys` — Create license keys without a campaign (admin auth required)

```
Request:
{
  "count": 10,                // 1-100
  "planDays": 30,             // Days to grant on redemption
  "expiresInDays": 30,        // Key validity period in days
  "recipientMatcher": "all",  // "all" or "never_paid"
  "note": "KOL@xxx 渠道"      // Optional admin memo
}

Response:
{
  "keys": [
    { "id": 1, "code": "K3MNTX7Q", "planDays": 30, "expiresAt": 1743033600 },
    { "id": 2, "code": "R9BWFD4H", "planDays": 30, "expiresAt": 1743033600 },
    ...
  ]
}
```

**Behavior:**
- `source` auto-set to `manual`, `campaignId` null
- Returns full key list for admin to copy and distribute
- No email triggered (admin distributes manually)

#### Admin UI Changes (`/manager/license-keys`)

- **New "创建授权码" button** → opens form dialog:
  - Count (number input, 1-100)
  - Plan days (number input, default 30)
  - Validity period in days (number input, default 30)
  - Recipient matcher (select: all / never_paid)
  - Note (text input)
- **Creation result dialog** — after successful creation:
  - Displays generated codes in a list
  - "批量复制" (bulk copy) button — copies all codes as newline-separated text
  - Individual copy button per code
- **List columns updated:**
  - Add `code` column with copy button
  - Add `source` column (badge: manual / campaign)
  - Add `note` column
- **New filter:** by `source` (manual / campaign / all)

### 3. User Redemption Flow

#### Routes

| Route | Type | Purpose |
|-------|------|---------|
| `/[locale]/g` | Landing page | Manual code input (client-side, no SSR data fetch) |
| `/[locale]/g/[code]` | Direct link | Click-through redemption (SSR prefetch key info) |

#### `/g` Landing Page

**Layout:** Centered card, minimal design (similar to invite landing `/s/[code]`)

**Rendering:** Static client-side page. Input validation and key lookup happen via client-side fetch after user submits.

**Flow:**
1. User sees input field + brief explanation ("输入授权码，免费获取会员")
2. User pastes/types code → clicks "查看"
3. Client calls `GET /api/license-keys/code/:code`
4. Displays code info card: plan days, expiry, sender info (if available)
5. User clicks "兑换":
   - **Not logged in** → LoginDialog opens → after login, auto-completes redemption
   - **Logged in** → immediate redemption → success state

**Error states (on info card display):**
- Invalid code → "授权码不存在"
- Already used → "此授权码已被使用" + link to `/purchase`
- Expired → "此授权码已过期" + link to `/purchase`

**Error states (after redeem attempt):**
- Not eligible (recipient matcher) → "不符合使用条件" + link to `/purchase`
- Already redeemed another key → "您已使用过授权码" + link to `/purchase`

Note: Eligibility (`recipientMatcher`) and per-user anti-abuse checks can only be evaluated after login, so these errors surface only after the POST redeem call, not on the info card.

#### `/g/[code]` Direct Link Page

**SSR:** Server-side fetches key info via `GET /api/license-keys/code/:code` (follows existing SSR→Center API pattern).

**Flow:**
- Valid key → render info card + "兑换" button (same redemption flow as above)
- Invalid/used/expired → render error state with fallback to `/purchase`

#### Key Design Principle: Show Value Before Login

Users see "将获得 30 天会员" BEFORE being asked to register/login. This leverages sunk cost — they've already found the page and seen the reward, so they're more likely to complete registration.

### 4. Entry Points

| Location | Form | Purpose |
|----------|------|---------|
| `/purchase` page | Light text prompt: "已有授权码？[点此兑换](/g)" | Catch users with codes who land on purchase |
| External sharing | `kaitu.io/g/K3MNTX7Q` direct link | Social media, chat, email, print |
| `/g` page | Standalone landing | Manual code entry for all channels |

**Not added:**
- webapp Dashboard — license keys target new users who don't have the app yet

### 5. API Changes

**New endpoints:**
- `POST /app/license-keys` — Admin manual key creation, admin auth required (see Section 2)
- `GET /api/license-keys/code/:code` — Public key lookup by short code (replaces `/api/license-keys/:uuid`). Rate limited: 10 req/min per IP. Returns generic 404 for all not-found/error cases to prevent code enumeration.
- `POST /api/license-keys/code/:code/redeem` — Redeem by short code, user auth required (replaces `/api/license-keys/:uuid/redeem`)

**Removed endpoints:**
- `GET /api/license-keys/:uuid` — replaced by code-based lookup
- `POST /api/license-keys/:uuid/redeem` — replaced by code-based redeem

**Campaign key generation updated:**
- `POST /app/campaigns/:id/issue-keys` — now also generates `code` field for each key. Campaign keys continue to use fixed 30-day planDays.
- Email links updated: `kaitu.io/g/{code}` instead of `kaitu.io/redeem/{uuid}`

### 6. Scope Exclusions

- **No webapp integration** — target audience is new users without the app
- **No backward compatibility** — `/redeem/[uuid]` never published, delete entirely
- **No QR code generation** — can be added later; short URL is sufficient for now
- **No analytics tracking** — can be added later with UTM params on `/g/[code]` links
- **No configurable planDays for campaign keys** — campaigns continue to use fixed 30 days; only manual keys support custom planDays
