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
- **How**: Bottom banner in App → `openExternal` to `kaitu.io/survey/active?uid=xxx`
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

### Authentication
- One-time token in URL (generated per user, stored or signed JWT)
- No login required — expired users likely can't/won't log in

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

## Database Schema

```sql
CREATE TABLE survey_responses (
    id            BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    user_id       BIGINT UNSIGNED NOT NULL,
    survey_key    VARCHAR(64)  NOT NULL,   -- "active_2026q1", "expired_2026q1"
    answers       JSON         NOT NULL,   -- {"q1":"ai_tools","q2":"solo","q3":"..."}
    device_udid   VARCHAR(128) DEFAULT '',
    ip_address    VARCHAR(45)  DEFAULT '',
    reward_days   INT          DEFAULT 0,  -- 30 for this campaign, flexible per campaign
    created_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,

    UNIQUE KEY uk_user_survey (user_id, survey_key),
    KEY idx_survey_key (survey_key)
);
```

**Reusability**: New campaign → new `survey_key`. Table and API unchanged. `answers` JSON accommodates any question structure. `reward_days` varies per campaign.

## API Design

### `POST /app/survey/submit`

**Request**:
```json
{
  "survey_key": "active_2026q1",
  "answers": {"q1": "ai_tools", "q2": "solo", "q3": "连接稳定，AI 都能用"},
  "device_udid": "xxx"
}
```

**Auth**:
- Survey A: Standard auth token (user is logged in via App)
- Survey B: One-time token from URL param (`?token=xxx`), verified server-side

**Logic** (single transaction):
1. Validate survey_key is active campaign
2. Check `uk_user_survey` — if exists, return "already submitted"
3. Insert `survey_responses` row with `reward_days=30`
4. Update `user.expiredAt`:
   - Active users: `expiredAt += 30 days`
   - Expired users: `expiredAt = now() + 30 days`
5. Insert `UserProHistory` record (type: `reward`, reason: `survey_active_2026q1`, days: 30)
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

### `GET /app/survey/status?survey_key=xxx`

Returns whether current user has already submitted this survey. Used by App to decide whether to show banner.

## Web Pages

### `kaitu.io/survey/active`

- URL params: `?uid=xxx` (user UUID from App)
- Auth: validate uid exists and subscription is active
- 3-question form with progress indicator
- Submit → API → show success + "30 days added" confirmation
- i18n: zh-CN primary, en-US secondary

### `kaitu.io/survey/expired`

- URL params: `?token=xxx` (one-time token from EDM email)
- Auth: validate token, extract user_id
- 3-question form
- Submit → API → show success + "account reactivated for 30 days"
- i18n: zh-CN primary, en-US secondary

### Shared
- Same form component, different question sets based on survey type
- Mobile-responsive (users may open on phone)
- No login required for expired survey (token-based)

## App Integration (Survey A only)

### Connection Counter
- localStorage key: `k2_connect_success_count`
- Increment on each successful VPN connection (state → `connected`)
- Threshold: 5

### Banner Trigger
- Conditions: `connect_count >= 5` AND user is paid AND not yet submitted (check `/app/survey/status`)
- UI: Bottom banner similar to existing `AnnouncementBanner` pattern
- Action: `openExternal('https://kaitu.io/survey/active?uid=xxx')`
- Dismiss: localStorage flag, don't show again after dismiss or submission

## EDM Campaign (Survey B only)

### Target
- Users where `expiredAt` is within past 180 days
- Use existing EDM infrastructure (templates, rate limiting, idempotency)

### Email Content
- Subject: "We miss you — fill 3 questions, get 30 days free"
- Body: Brief, personal, clear CTA button with tokenized link
- Multi-language: zh-CN and en-US versions

### Token Generation
- Signed JWT or random token stored in DB, mapped to user_id
- Expiry: 14 days (campaign duration)
- Single-use: invalidated after survey submission

## Admin Dashboard

### Survey Stats Page
- Response rate per survey_key
- Per-question distribution charts (bar charts)
- Account sharing signal: users with multiple device_udid submissions
- Q3 open-text responses list (filterable)
- Export to CSV

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

-- Account sharing signal
SELECT user_id, COUNT(DISTINCT device_udid) AS devices
FROM survey_responses WHERE survey_key = 'active_2026q1'
GROUP BY user_id HAVING devices > 1;

-- Open text responses
SELECT user_id, JSON_UNQUOTE(JSON_EXTRACT(answers, '$.q3')) AS recommendation
FROM survey_responses WHERE survey_key = 'active_2026q1'
AND JSON_EXTRACT(answers, '$.q3') IS NOT NULL;
```
