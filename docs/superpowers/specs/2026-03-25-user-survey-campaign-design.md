# User Survey Campaign Design

## Background

505 paying users, but we don't know their real usage scenarios. Before planning marketing, we need:
1. **User profiles**: What are they using Kaitu for? What's their profession?
2. **Account sharing**: Is one account used by multiple people? (Directly impacts pricing strategy)

A survey-with-reward campaign also boosts user engagement.

## Core Decisions

- **Self-hosted**: All survey data in our own Center API database. No external tools (Tally.so, Google Forms) — China accessibility issues + data control requirements.
- **Web site pages**: All surveys live on `kaitu.io/survey/[surveyKey]` (Next.js dynamic route). App users open via `openExternal`. Expired users reach it via EDM email. One codebase, unified experience.
- **Questions hardcoded in frontend**: 6 fixed questions across 2 surveys. No backend survey template system — that would be over-engineering.
- **Reusable DB schema**: `survey_key` differentiates campaigns. Future surveys reuse the same table and API.
- **Web site login as auth**: No JWT tokens or custom auth. Survey pages use the web site's existing login flow. Expired users can still log in (account exists, just subscription expired).

## Two Surveys, Two Audiences

Active users and expired users are completely different signal sources:
- **Active users** reveal: where the real product value is, how users describe it in their own words (marketing copy source)
- **Expired users** reveal: why they churned, what conditions would bring them back

Mixing them pollutes both datasets.

## Survey A — Active Users

### Trigger
- **Who**: Subscription not expired
- **When**: After 5th successful VPN connection (localStorage counter)
- **How**: Bottom banner in App → `openExternal` to `kaitu.io/survey/active_2026q1`
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
- **How**: EDM email linking to `kaitu.io/survey/expired_2026q1`. Also App open popup (for users who still have App installed).
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

**No custom tokens.** Both surveys use the web site's existing login flow.

- User opens survey page → if not logged in, web site redirects to login → after login, redirects back to survey
- Expired users can still log in — their account exists, only subscription is expired
- `POST /api/survey/submit` uses standard Bearer token auth, same as all other `/api/*` endpoints
- EDM email links are plain URLs (`kaitu.io/survey/expired_2026q1`) — no tokens, no params

This eliminates: JWT generation, JWT verification, token endpoint, token expiry handling, and dual-auth branching.

### Single-Use Enforcement
`UNIQUE KEY (user_id, survey_key)` prevents duplicate submissions. If user visits the page again after submitting, `GET /api/survey/status` returns `submitted: true` and the page shows a "you've already completed this" message.

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

## API Design

All survey endpoints use `/api/survey/` prefix. Standard Bearer token auth.

### `POST /api/survey/submit`

**Request**:
```json
{
  "survey_key": "active_2026q1",
  "answers": {"q1": "ai_tools", "q2": "solo", "q3": "连接稳定，AI 都能用"}
}
```

**Auth**: Standard Bearer token (same as all `/api/*` endpoints).

**Logic** (single transaction):
1. Validate `survey_key` is an active campaign (hardcoded allowlist in code, e.g., `var activeSurveys = []string{"active_2026q1", "expired_2026q1"}`)
2. Check `uk_user_survey` — if exists, return `code: "already_submitted"`
3. Insert `survey_responses` row with `reward_days=30`
4. Update `user.expiredAt` via `addProExpiredDays()`:
   - If user not expired: `expiredAt += 30 days`
   - If user expired: `expiredAt = now() + 30 days`
5. Insert `UserProHistory` record (type: `survey_reward`, reason: `survey_{survey_key}`, days: 30)
6. Return success + new expiry date

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
- `code: "survey_closed"` — survey_key not in active list

### `GET /api/survey/status?survey_key=xxx`

**Auth**: Standard Bearer token.

Returns whether current user has already submitted this survey. Used by both the App (to decide whether to show banner) and the web page (to show "already completed" state).

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
  [surveyKey]/page.tsx     -- Dynamic route: renders survey based on surveyKey
  _components/
    SurveyForm.tsx         -- Shared form component
    SurveySuccess.tsx      -- Success/reward confirmation
    surveyConfig.ts        -- Question definitions per survey_key
```

### `kaitu.io/[locale]/survey/[surveyKey]`

- `surveyKey` maps to question config in `surveyConfig.ts` — unknown keys → 404
- Page checks login state → if not logged in, redirect to login with return URL
- Page checks `GET /api/survey/status` → if already submitted, show "completed" state
- 3-question form with progress indicator
- Submit → `POST /api/survey/submit` → show success + reward confirmation
- i18n: zh-CN primary, en-US secondary

### Error States
- **Not logged in**: Redirect to web site login, return to survey after
- **Already submitted**: "You've already completed this survey. Your reward has been applied."
- **Unknown surveyKey**: 404 page
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
- Action: `openExternal('https://kaitu.io/survey/active_2026q1')`
- Dismiss: localStorage flag. Known limitation: dismiss doesn't sync across devices, but server-side status check prevents showing banner to users who already submitted on another device.

## EDM Campaign (Survey B only)

### Target
- Users where `expiredAt` is within past 180 days
- Use existing EDM infrastructure (templates, rate limiting, idempotency)
- EDM `EmailSendLog` tracks which users received the email (existing infrastructure)

### Email Content
- Subject: "We miss you — fill 3 questions, get 30 days free"
- Body: Brief, personal, CTA button linking to `https://kaitu.io/survey/expired_2026q1`
- Multi-language: zh-CN and en-US versions (language inferred from existing EDM language logic)

### Reminder
- 7 days after initial send, query users in `EmailSendLog` for this campaign who have no matching row in `survey_responses` → send reminder email

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

## Implementation Notes

- Use existing `addProExpiredDays()` in `api/logic_member.go` for reward extension
- Add `VipSurveyReward` constant to `VipChangeType` in `api/model.go`
- `UserProHistory.ReferenceID` = `survey_responses.id` after insert
- Register `survey` i18n namespace in `web/` messages config
- API calls from web pages go through Next.js server-side proxy (no CORS issues)

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
