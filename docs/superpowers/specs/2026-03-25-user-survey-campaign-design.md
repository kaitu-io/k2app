# User Survey Campaign Design

## Background

505 paying users, but we don't know their real usage scenarios. Before planning marketing, we need:
1. **User profiles**: What are they using Kaitu for? What's their profession?
2. **Account sharing**: Is one account used by multiple people? (Directly impacts pricing strategy)

A survey-with-reward campaign also boosts user engagement.

## Core Decisions

- **Self-hosted**: All survey data in our own Center API database. No external tools (Tally.so, Google Forms) — China accessibility issues + data control requirements.
- **Web site pages**: Both surveys live on `kaitu.io/survey/[type]` (Next.js). App users open via `openExternal` browser link. Expired users reach it via EDM email. One codebase, unified experience.
- **Questions hardcoded in frontend**: 6 fixed questions across 2 surveys. No backend survey template system — that would be over-engineering.
- **Reusable DB schema**: `survey_key` differentiates campaigns. Future surveys reuse the same table and API.

## Two Surveys, Two Audiences

Active users and expired users are completely different signal sources:
- **Active users** reveal: where the real product value is, how users describe it in their own words (marketing copy source)
- **Expired users** reveal: why they churned, what conditions would bring them back

Mixing them pollutes both datasets.

## Survey A — Active Users

### Trigger
- **Who**: Subscription not expired
- **When**: After 5th successful VPN connection (localStorage counter)
- **How**: Bottom banner in App → `openExternal` to `kaitu.io/survey/active?token=xxx` (short-lived signed token, see Authentication section)
- **Frequency**: Once per account (deduplicated by `uk_user_survey`)

### Questions

**Q1. What do you mainly use Kaitu for?** (single choice)
- Using AI tools (ChatGPT / Claude / Gemini)
- Work needs (remote office, accessing company systems)
- Watching YouTube / Netflix
- Learning / research (Google, academic platforms)
- Other: ___

**Business decision**: If AI tools > 40%, market positioning becomes "network infrastructure for AI practitioners". Otherwise, completely different strategy.

**Q2. Is this account used by you alone or shared?** (single choice)
- Only me
- Shared with family (2-3 people)
- Shared with friends/colleagues (3+ people)

**Business decision**: If sharing > 30%, launch single-device plan (~$19/year). If low, skip it.

**Q3. How would you describe Kaitu to a friend?** (open text, optional)
- Placeholder examples: "Stable connection, all AI tools work" / "Less disconnections than others"

**Business decision**: User language → marketing copy. Their words, not ours.

### Reward
- +30 days from current `expiredAt`
- Automatic, same transaction as survey submission

## Survey B — Expired Users

### Trigger
- **Who**: Account expired ≤ 180 days ago
- **How**: EDM email with personalized token link → `kaitu.io/survey/expired?token=xxx`. Also App open popup (for users who still have App installed).
- **Frequency**: Once per account

### Questions

**Q1. Why didn't you renew?** (single choice)
- Too expensive
- Connection wasn't stable enough
- Found a better alternative
- Don't need it right now, might come back
- Forgot to renew, no reminder

**Business decision**: Each option → different action. "Forgot" ≥ 20% → add 7-day pre-expiry push notification (zero-cost recovery). "Too expensive" → low-price plan. "Not stable" → product bug, not marketing.

**Q2. What improvement would most likely bring you back?** (single choice)
- Lower price (e.g., cheaper single-device plan)
- More stable connection, fewer disconnects
- Support more simultaneous devices
- Chinese customer support for quick issue resolution
- Other: ___

**Business decision**: Determines recall email messaging per cohort. Price-sensitive users get price offer; stability-concerned users get "v0.4 upgrade" message.

**Q3. Do you still need access to overseas networks?** (single choice)
- Yes, even more than before
- Yes, about the same
- Not right now, but maybe later
- Basically no

**Business decision**: High "yes" → these users churned for fixable reasons, worth investing in recall. High "no" → deprioritize, focus on acquisition.

### Reward
- +30 days from `now()` (not from old expiry — they need immediate reactivation)
- Automatic, same transaction as survey submission

## Authentication

Both surveys use **signed JWT tokens** in URL params. No stored tokens — stateless verification.

### Token Format
```
JWT payload: { user_id, survey_key, exp }
Signed with: Center API HMAC secret
Expiry: 1 hour (Survey A, generated on banner click) / 14 days (Survey B, generated for EDM)
```

### Survey A Flow
1. User clicks banner in App
2. App calls `GET /api/survey/token?survey_key=active_2026q1` (auth'd with existing session)
3. API generates short-lived JWT (1 hour), returns URL
4. App opens `kaitu.io/survey/active?token=xxx` in browser

### Survey B Flow
1. EDM system generates 14-day JWT per user during email send
2. Email contains `kaitu.io/survey/expired?token=xxx`
3. User clicks link, browser opens page

### Single-Use Enforcement
Token itself is not invalidated. Instead, `uk_user_survey` unique constraint prevents duplicate submissions — if user clicks link again after submitting, API returns "already submitted" and page shows a friendly message.

## Database Schema

```sql
CREATE TABLE survey_responses (
    id            BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    user_id       BIGINT UNSIGNED NOT NULL,
    survey_key    VARCHAR(64)  NOT NULL,   -- "active_2026q1", "expired_2026q1"
    answers       JSON         NOT NULL,   -- {"q1":"ai_tools","q2":"solo","q3":"..."}
    ip_address    VARCHAR(45)  DEFAULT '',
    reward_days   INT          DEFAULT 0,  -- 30 for this campaign, flexible per campaign
    created_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,

    UNIQUE KEY uk_user_survey (user_id, survey_key),
    KEY idx_survey_key (survey_key)
);
```

**Reusability**: New campaign → new `survey_key`. Table and API unchanged. `answers` JSON accommodates any question structure. `reward_days` varies per campaign.

**Note**: `device_udid` removed — surveys are filled in a web browser, not in the app, so UDID is unavailable. Account sharing signal comes from Q2 answers directly.

## API Design

All survey endpoints use `/api/survey/` prefix (user-facing routes).

### `POST /api/survey/submit`

**Request**:
```json
{
  "token": "eyJhbG...",
  "answers": {"q1": "ai_tools", "q2": "solo", "q3": "连接稳定，AI 都能用"}
}
```

**Auth**: JWT token in request body. Server verifies signature and extracts `user_id` + `survey_key`. Same auth flow for both Survey A and B — unified, no branching.

**Logic** (single transaction):
1. Verify JWT signature and expiry
2. Extract `user_id` and `survey_key` from token
3. Validate `survey_key` is an active campaign (hardcoded allowlist in code, e.g., `var activeSurveys = []string{"active_2026q1", "expired_2026q1"}`)
4. Check `uk_user_survey` — if exists, return `code: "already_submitted"`
5. Insert `survey_responses` row with `reward_days=30`
6. Update `user.expiredAt`:
   - If user not expired: `expiredAt += 30 days`
   - If user expired: `expiredAt = now() + 30 days`
7. Insert `UserProHistory` record (type: `reward`, reason: `survey_{survey_key}`, days: 30)
8. Return success + new expiry date

**Response**:
```json
{
  "code": 0,
  "data": {
    "reward_days": 30,
    "new_expired_at": 1753488000
  }
}
```

**Error responses**:
- `code: "already_submitted"` — user already filled this survey
- `code: "token_expired"` — JWT expired
- `code: "token_invalid"` — bad signature or malformed
- `code: "survey_closed"` — survey_key not in active list

### `GET /api/survey/token?survey_key=xxx`

**Auth**: Standard Bearer token (logged-in user from App).

Returns a signed JWT URL for the given survey. Used by App to generate the `openExternal` link.

**Response**:
```json
{
  "code": 0,
  "data": {
    "url": "https://kaitu.io/survey/active?token=eyJhbG..."
  }
}
```

### `GET /api/survey/status?survey_key=xxx`

**Auth**: Standard Bearer token.

Returns whether current user has already submitted this survey. Used by App to decide whether to show banner.

**Response**:
```json
{
  "code": 0,
  "data": {
    "submitted": false
  }
}
```

## Web Pages

### Next.js Route Structure
```
web/src/app/[locale]/survey/
  active/page.tsx    -- Survey A form
  expired/page.tsx   -- Survey B form
  _components/
    SurveyForm.tsx   -- Shared form component
    SurveySuccess.tsx -- Success/reward confirmation
```

### `kaitu.io/[locale]/survey/active`

- URL params: `?token=xxx`
- Page validates token client-side (decode JWT to check expiry), full validation on submit
- 3-question form with progress indicator
- Submit → API → show success + "30 days added" confirmation
- i18n: zh-CN primary, en-US secondary

### `kaitu.io/[locale]/survey/expired`

- URL params: `?token=xxx`
- Same flow as active, different question set
- Submit → API → show success + "account reactivated for 30 days"

### Error States
- **Token expired**: "This link has expired. Please request a new one from the app / contact support."
- **Token invalid**: "Invalid link. Please use the link from your email or app."
- **Already submitted**: "You've already completed this survey. Your reward has been applied." (show current expiry date)
- **Survey closed**: "This survey has ended. Thank you for your interest."

### Shared
- `SurveyForm` component accepts question config array, renders single-choice + optional text
- Mobile-responsive (users may open on phone)
- Minimal page — no header/footer chrome, just logo + form + submit

## App Integration (Survey A only)

### Connection Counter
- localStorage key: `k2_connect_success_count`
- Increment location: `vpn-machine.store.ts` transition handler, on entry to `connected` state
- Threshold: 5

### Banner Trigger
- Conditions: `connect_count >= 5` AND user is paid AND not yet submitted (check `GET /api/survey/status`)
- The `/api/survey/status` check is the authoritative source; localStorage dismiss is a fast-path to avoid unnecessary API calls
- UI: Bottom banner similar to existing `AnnouncementBanner` pattern
- Action: Call `GET /api/survey/token` → `openExternal(url)`
- Dismiss: localStorage flag. Known limitation: dismiss doesn't sync across devices, but server-side status check prevents showing banner to users who already submitted on another device.

## EDM Campaign (Survey B only)

### Target
- Users where `expiredAt` is within past 180 days
- Use existing EDM infrastructure (templates, rate limiting, idempotency)
- EDM `EmailSendLog` tracks which users received the email (existing infrastructure)

### Email Content
- Subject: "We miss you — fill 3 questions, get 30 days free"
- Body: Brief, personal, clear CTA button with tokenized link
- Multi-language: zh-CN and en-US versions (language inferred from existing EDM language logic)

### Token Generation
- During EDM batch send, generate 14-day JWT per user
- Embed in email CTA: `https://kaitu.io/survey/expired?token=xxx`

### Reminder
- 7 days after initial send, query users in `EmailSendLog` for this campaign who have no matching row in `survey_responses` → send reminder email with fresh token

## Admin Dashboard

### Route
`web/src/app/(manager)/manager/surveys/page.tsx`

### Features
- Campaign selector (dropdown of survey_keys)
- Response count + response rate (total targeted vs. submitted)
- Per-question bar charts (use existing chart patterns from admin dashboard)
- Account sharing breakdown from Q2 answers (pie chart: solo / family / friends)
- Q3 open-text responses table (sortable, filterable)
- CSV export button

## Timeline

- **Campaign duration**: 2 weeks
- **Survey A**: Triggered continuously for active users who hit 5 connections
- **Survey B**: EDM sent once, reminder after 7 days for non-respondents

## Analytics Queries

```sql
-- Response rate
SELECT survey_key, COUNT(*) AS responses FROM survey_responses GROUP BY survey_key;

-- Q1 distribution (active survey)
SELECT JSON_UNQUOTE(JSON_EXTRACT(answers, '$.q1')) AS choice, COUNT(*)
FROM survey_responses WHERE survey_key = 'active_2026q1' GROUP BY choice;

-- Account sharing from Q2
SELECT JSON_UNQUOTE(JSON_EXTRACT(answers, '$.q2')) AS sharing, COUNT(*)
FROM survey_responses WHERE survey_key = 'active_2026q1' GROUP BY sharing;

-- Open text responses
SELECT user_id, JSON_UNQUOTE(JSON_EXTRACT(answers, '$.q3')) AS recommendation
FROM survey_responses WHERE survey_key = 'active_2026q1'
AND JSON_EXTRACT(answers, '$.q3') IS NOT NULL
AND JSON_UNQUOTE(JSON_EXTRACT(answers, '$.q3')) != '';
```
