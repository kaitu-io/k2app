---
name: kaitu-growth
description: Growth operations for Kaitu VPN — lifecycle framework (acquisition→activation→retention→monetization→referral), data-driven playbooks, campaign/EDM/license-key/retailer/announcement management, GFW event response, and social media operations (Twitter, 小红书). Covers all 61 kaitu-center marketing tools plus social media MCP servers.
triggers:
  - growth
  - 运营
  - 自媒体
  - social media
---

# Kaitu Growth Operations

Full-lifecycle growth operations. Strategic framework guides decisions, playbooks drive execution, data closes the loop.

## VPN Business Context

Kaitu is a VPN service targeting users in mainland China. These characteristics affect all growth decisions:

**DAU drop ≠ growth problem.** A VPN DAU decline may be caused by:
- Nodes blocked / GFW upgrade (service issue) → hand off to `kaitu-node-ops` / engineering
- Seasonal fluctuation (school holidays, national holidays) → normal
- Actual user churn → only this is a growth concern

**Always rule out service issues before taking growth actions.** See DAU triage in the Data Analysis section.

**Activation = first successful connection, not first login.** Users who register but can't connect are not activated.

**Monetization requires service quality.** Stable connections drive high renewal rates naturally; coupons are ineffective when connections are unstable. The monetization playbook has service health as a precondition.

**Acquisition is event-driven.** GFW upgrades = crisis + opportunity. Requires a dedicated event response playbook.

---

## Lifecycle Framework

```
Acquisition → Activation → Retention → Monetization → Referral
     ↑                                                     |
     └─────────────────────────────────────────────────────┘
```

| Stage | Core KPI | Data Tool | Healthy Threshold | Anomaly → Playbook |
|---|---|---|---|---|
| Acquisition | New signups (24h/7d/30d) | `user_statistics` | Stable or growing daily | → Acquisition playbook |
| Activation | Signup→first-connection rate | `usage_overview` + `user_statistics` | >60% | → Activation playbook |
| Retention | DAU trend / renewal rate | `usage_overview` + `order_statistics` | DAU stable, not declining | → DAU triage first, then Retention playbook |
| Monetization | Paid conversion / monthly revenue / ARPU | `order_statistics` | No month-over-month decline | → Monetization playbook |
| Referral | Retailer activity / license key redemption rate | `list_retailers` + `license_key_batch_stats_by_source` | Redemption >30% | → Referral playbook |

---

## Tool Reference

61 MCP tools grouped by domain. Parameter details are in each tool's built-in description; this section is a navigation index only.

### Campaign (8 tools)

| Tool | Purpose | Write | Approval |
|------|---------|-------|----------|
| `list_campaigns` | List all campaigns | | |
| `get_campaign` | Campaign details | | |
| `create_campaign` | Create campaign (discount/coupon) | ✓ | ✓ |
| `update_campaign` | Update campaign | ✓ | ✓ |
| `delete_campaign` | Delete campaign | ✓ | ✓ |
| `campaign_stats` | Campaign statistics (by code) | | |
| `campaign_funnel` | Conversion funnel (visit→signup→trial→paid) | | |
| `campaign_orders` | Orders attributed to campaign | | |

Campaign types: `discount`, `coupon`
Matcher rules: `first_order`, `vip`, `all`, `paid_before`, `paid_before_active`

### EDM Email (5 tools)

| Tool | Purpose | Write |
|------|---------|-------|
| `list_edm_templates` | List email templates (id, slug, language) | |
| `create_edm_template` | Create template (supports `{{.Var}}` placeholders) | ✓ |
| `update_edm_template` | Update template | ✓ |
| `send_templated_email` | Send emails to specified recipients by template slug | ✓ |
| `get_edm_send_stats` | Email send statistics | |

Send example:
```
send_templated_email(
  batch_id="mcp:2026-04-08:renewal-30d",
  items=[
    { email: "user@example.com", slug: "renewal-30d", vars: { "Days": "30", "Name": "Username" } }
  ]
)
```
- `batch_id` must be unique (prevents duplicate sends)
- `slug` references the template's slug field
- `vars` fills `{{.Var}}` placeholders in the template

### Retailer (5 tools)

| Tool | Purpose | Write |
|------|---------|-------|
| `list_retailers` | List retailers | |
| `get_retailer_detail` | Retailer details (level, commission, metrics) | |
| `update_retailer_level` | Adjust commission level (L1-L4) | ✓ |
| `create_retailer_note` | Add follow-up note | ✓ |
| `list_retailer_todos` | Pending retailer action items | |

**Rule: Must `create_retailer_note` with reason BEFORE `update_retailer_level`.**

### Order (2 tools, read-only)

| Tool | Purpose |
|------|---------|
| `list_orders` | Order list (filterable by email) |
| `get_order_detail` | Order details (amount, payment method, status) |

### Plan (5 tools)

| Tool | Purpose | Write |
|------|---------|-------|
| `list_admin_plans` | List all plans (including hidden) | |
| `create_plan` | Create plan | ✓ |
| `update_plan` | Update plan | ✓ |
| `delete_plan` | Soft-delete plan | ✓ |
| `restore_plan` | Restore deleted plan | ✓ |

### License Key (9 tools)

| Tool | Purpose | Write | Approval |
|------|---------|-------|----------|
| `list_license_key_batches` | Batch list (filterable by source_tag) | | |
| `get_license_key_batch` | Batch details + redemption stats | | |
| `create_license_key_batch` | Create batch | ✓ | ✓ |
| `list_license_key_batch_keys` | Keys within a batch | | |
| `license_key_batch_stats` | Batch statistics (redemption rate, conversion) | | |
| `license_key_batch_stats_by_source` | Stats by channel (source_tag) | | |
| `invalidate_license_key_batch` | Invalidate batch (keeps redeemed keys for analytics) | ✓ | ✓ |
| `list_license_keys` | Global key list | | |
| `delete_license_key` | Delete single key | ✓ | |

Batch creation params: `source_tag` (channel: twitter, kol-xxx, winback), `recipient_matcher` (all / never_paid), `plan_days`, `quantity` (1-10000), `expires_in_days`

### User (6 tools)

| Tool | Purpose | Write |
|------|---------|-------|
| `lookup_user` | Find user by email or UUID | |
| `list_user_devices` | User's device list | |
| `add_user_membership` | Grant membership days | ✓ |
| `update_user_email` | Change user email | ✓ |
| `set_user_roles` | Set user roles | ✓ |
| `update_user_retailer_status` | Toggle retailer status | ✓ |

### Announcement (5 tools)

| Tool | Purpose | Write |
|------|---------|-------|
| `list_announcements` | List all announcements | |
| `create_announcement` | Create announcement | ✓ |
| `update_announcement` | Update announcement | ✓ |
| `delete_announcement` | Soft-delete announcement | ✓ |
| `activate_announcement` | Activate announcement | ✓ |

Announcement params: `min_version`/`max_version` (version targeting), `open_mode` (external=browser / webview=in-app), `auth_mode` (none / ott=auto-login), `expires_at`

### Statistics (6 tools, read-only)

| Tool | Returns |
|------|---------|
| `user_statistics` | Total users, paid, free, new (24h/7d/30d), monthly registration trend |
| `order_statistics` | Total revenue, order count, conversion rate, ARPU, 30-day daily revenue trend |
| `device_statistics` | Total devices, active devices, platform breakdown |
| `active_devices` | Currently active device list |
| `usage_overview` | DAU, connection count, node usage distribution (top 20), k2s downloads |
| `survey_stats` | Survey responses, satisfaction, feature requests |

### Approval (5 tools)

| Tool | Purpose | Write |
|------|---------|-------|
| `list_approvals` | Approval list (filterable by status) | |
| `get_approval` | Approval details | |
| `approve_approval` | Approve (executes pending action) | ✓ |
| `reject_approval` | Reject approval | ✓ |
| `cancel_approval` | Cancel approval (by creator) | ✓ |

---

## Routine Playbook

### Daily Report

Trigger: "daily report", "日报"

Steps:
1. `user_statistics` → new signups (24h/7d), paid user count
2. `order_statistics` → today's revenue, order count, conversion rate
3. `usage_overview(range=7d)` → DAU trend, node usage distribution
4. `get_edm_send_stats` → email delivery status
5. `list_retailer_todos` → pending retailer action items
6. `list_approvals(status=pending)` → pending approvals

Output format:
```
## Daily Report YYYY-MM-DD

### Users
- New signups: X (24h) / X (7d)
- Total paid users: X (vs yesterday +/-X)

### Revenue
- Today: ¥X (X orders)
- 7-day: ¥X
- Paid conversion rate: X%

### Activity
- DAU: X (7-day trend: ↑/↓/→)
- Top 3 nodes by usage: ...

### Action Items
- Pending approvals: X
- Retailer todos: X
- Emails pending: X

### Anomalies
- [Flag anomalies and suggest corresponding playbook]
```

### Weekly Report

In addition to daily report data:
1. `license_key_batch_stats_by_source` → per-channel redemption rates
2. `campaign_stats` → active campaign performance
3. `survey_stats` → user feedback trends
4. Week-over-week comparison for all metrics; flag >10% changes

---

## Acquisition Playbook

**Goal: Increase new user signups.**

### Campaign-Based Acquisition

1. Determine campaign type and target audience
2. `create_campaign(code, name, type, value, matcher_type, start_at, end_at)` → submits for approval
3. Wait for approval → `list_approvals(status=pending)`
4. Monitor after launch: `campaign_stats(code)` + `campaign_funnel(code)`
5. Adjust or terminate if underperforming: `update_campaign` / `delete_campaign`

### License Key Acquisition

1. Determine channel and quantity
2. `create_license_key_batch(name, source_tag, recipient_matcher, plan_days, quantity, expires_in_days)` → submits for approval
3. After approval → `list_license_key_batch_keys(batch_id)` to get key list
4. Distribute to target channels
5. Track performance: `license_key_batch_stats(batch_id)` + `license_key_batch_stats_by_source`
6. Reclaim unused keys from underperforming channels: `invalidate_license_key_batch(batch_id)`

### Channel Performance Analysis

`license_key_batch_stats_by_source` → compare redemption rate and paid conversion across source_tags.

Decision criteria:
- Redemption >30% AND paid conversion >10% → high-performing channel, increase investment
- Redemption >30% BUT paid conversion <5% → freeloader channel, tighten conditions (`recipient_matcher=never_paid`)
- Redemption <15% → underperforming channel, reduce or stop distribution

---

## Activation Playbook

**Goal: Improve signup→first-connection conversion rate.**

### New User Announcement

```
create_announcement(
  message="Welcome to Kaitu! Tap to view the quick start guide.",
  link_url="https://kaitu.io/install",
  open_mode="webview",
  auth_mode="ott",
  min_version="0.1.0",
  priority=1,
  is_active=true
)
```

### Welcome Email

Send welcome email guiding download and first connection:
```
send_templated_email(
  batch_id="mcp:YYYY-MM-DD:welcome",
  items=[{ email: "...", slug: "welcome", vars: { "Name": "..." } }]
)
```

### Monitor Activation Rate

Compare `user_statistics` (new signups) with `usage_overview` (new device connections).
Large gap = activation bottleneck. Investigate:
- Download links working?
- Installation flow has friction?
- Connection succeeding? (check `kaitu-support` feedback tickets)

---

## Retention Playbook

**Goal: Maintain stable DAU, reduce paid user churn.**

**Precondition: Complete DAU triage (see Data Analysis section) to confirm this is a growth problem, not a service problem.**

### Renewal Reminder Email

Send renewal reminders to users whose membership is expiring soon:
```
send_templated_email(
  batch_id="mcp:YYYY-MM-DD:renewal-30d",
  items=[{ email: "...", slug: "renewal-30d", vars: { "Days": "30" } }]
)
```

Recipient lists must be built via `center-ops` database queries (query users with membership expiring within N days).

### Winback Email

Target churned users (membership expired >30 days):
```
send_templated_email(
  batch_id="mcp:YYYY-MM-DD:winback",
  items=[{ email: "...", slug: "winback-7d", vars: { "DiscountCode": "COMEBACK20" } }]
)
```

More effective when paired with a discount campaign.

### DAU Monitoring

`usage_overview(range=30d)` → observe DAU trend.
3 consecutive days of decline → trigger triage flow.

---

## Monetization Playbook

**Goal: Improve paid conversion rate and ARPU.**

**Precondition: Service quality is healthy (nodes up, connections stable). Coupons are ineffective when connections are unstable.**

### Plan Management

1. `list_admin_plans` → review current plans
2. Adjust pricing/duration: `update_plan(id, price, month)`
3. Add new plan: `create_plan(pid, label, price, month)`
4. Track impact: `order_statistics` — compare revenue before and after changes

### Discount Campaigns

Create time-limited discounts to drive conversion:
```
create_campaign(
  code="SPRING2026",
  name="Spring Sale",
  type="discount",
  value=20,
  matcher_type="first_order",
  start_at="2026-04-01T00:00:00Z",
  end_at="2026-04-15T23:59:59Z"
)
```

Track: `campaign_funnel(code="SPRING2026")` to view stage-by-stage conversion.

### Membership Grants (special cases)

Service outage compensation, KOL partnerships, etc.:
```
add_user_membership(uuid="...", months=1, reason="GFW upgrade compensation")
```

---

## Referral Playbook

**Goal: Activate retailer channels, increase word-of-mouth growth.**

### Daily Retailer Management

1. `list_retailer_todos` → process pending action items
2. For each: `get_retailer_detail(uuid)` → review performance metrics
3. Decision:
   - Strong performance → `create_retailer_note` + `update_retailer_level` (upgrade)
   - Needs follow-up → `create_retailer_note` (record action plan)
   - Inactive → `create_retailer_note` (record status)

### Level Adjustment Criteria

| Level | Criteria | Commission Rate |
|---|---|---|
| L1 | New retailer | Base |
| L2 | Monthly avg >X orders | Elevated |
| L3 | Monthly avg >Y orders, sustained 3+ months | Higher |
| L4 | Top retailer | Highest |

Specific thresholds are adjusted based on current business conditions; not hardcoded here.

### Channel License Key Distribution

Create dedicated license key batches for retailers:
```
create_license_key_batch(
  name="KOL-zhangsan-202604",
  source_tag="kol-zhangsan",
  recipient_matcher="never_paid",
  plan_days=7,
  quantity=100,
  expires_in_days=30
)
```

Track: `license_key_batch_stats_by_source` — compare by source_tag.

---

## Announcement Playbook

**Goal: Reach users via in-app announcements.**

### Publishing

```
create_announcement(
  message="Announcement content (max 500 chars)",
  link_url="https://kaitu.io/...",
  link_text="Learn more",
  open_mode="webview",        # webview=in-app / external=browser
  auth_mode="ott",            # ott=auto-login / none=no login
  priority=1,
  min_version="0.4.0",        # version targeting (optional)
  max_version="0.5.0",
  expires_at="2026-05-01T00:00:00Z",
  is_active=true
)
```

### Lifecycle

- Create with `is_active=false` → draft, not visible
- `activate_announcement(id)` → go live
- Auto-hidden after `expires_at`
- `delete_announcement(id)` → manual takedown

### Version Targeting

Use `min_version` / `max_version` for precision targeting:
- Force-update notice: `max_version="0.3.99"` (only old-version users see it)
- New feature guide: `min_version="0.4.0"` (only new-version users see it)

---

## GFW Event Response Playbook

**Trigger: Major GFW upgrade, users report widespread connection failures.**

This is a crisis-and-opportunity scenario requiring simultaneous responses on both fronts.

### Crisis Response (priority)

1. **Assess scope**: `usage_overview(range=7d)` → sharp DAU drop? Check node distribution — concentrated or widespread?
2. **Notify users via announcement**:
   ```
   create_announcement(
     message="We've detected network disruptions and are working on a fix. Please update to the latest version for the best experience.",
     link_url="https://kaitu.io/install",
     open_mode="external",
     priority=10,
     is_active=true
   )
   ```
3. **Coordinate technical fix**: Work with `kaitu-node-ops` (node rotation/expansion) and engineering (protocol adjustments)
4. **Update announcement after recovery**: `update_announcement` → change to "Service restored" notice, set `expires_at`

### Opportunity Capture

GFW upgrades cause a surge in new user searches:

1. **Prepare acquisition content**: Coordinate with `kaitu-content` to publish relevant articles
2. **Activate referral channels**: Create short-term license key batches for active retailers
3. **Temporary discount**: `create_campaign` with time-limited offer (lower first-purchase barrier)
4. **Monitor conversion**: `user_statistics` for signup surge + `order_statistics` for first orders

### Post-Event Review

After the event subsides:
1. `usage_overview(range=30d)` → has DAU recovered to pre-event levels?
2. `user_statistics` → net user gain during the event?
3. `order_statistics` → revenue impact during the event?
4. Document lessons learned, optimize response speed for next time

---

## Data Analysis Guide

### DAU Triage Flow (most important)

When DAU drops, **do NOT take growth actions immediately**. Rule out service issues first:

```
DAU Decline
  │
  ├─ usage_overview → node usage distribution anomaly?
  │   ├─ Some nodes traffic dropped to zero → nodes blocked → hand off to kaitu-node-ops
  │   └─ Broad decline → continue investigation
  │
  ├─ GFW upgrade period? (news/social media/user feedback)
  │   ├─ Yes → GFW Event Response playbook
  │   └─ No → continue investigation
  │
  ├─ Seasonal? (compare year-over-year, month-over-month)
  │   ├─ Matches pattern → normal fluctuation, no intervention needed
  │   └─ Does not match → growth problem, enter Retention playbook
  │
  └─ survey_stats → concentrated new complaints from users?
      ├─ Yes → identify specific issue
      └─ No → likely natural churn, analyze holistically
```

### Data → Judgment → Action Quick Reference

| Data Anomaly | Judgment Criteria | Action |
|---|---|---|
| 7d new signups down >20% WoW | Still declining after ruling out seasonality | Check if campaigns expired → Acquisition playbook |
| Paid conversion rate <5% | Sustained 7+ days | Review plan pricing + competitors → Monetization playbook |
| License key redemption <15% | Specific channel consistently underperforming | Stop that channel → Acquisition playbook channel analysis |
| DAU declining >10% for 3 consecutive days | Complete DAU triage first | Triage result determines next step |
| Retailer todos backlog >10 items | Unprocessed for 3+ days | Batch process → Referral playbook |
| Pending approvals >5 items | Unprocessed for 24+ hours | Remind david to review approvals |

---

## Approval Workflow

The following operations require two-person approval (Superadmin auto-approves):

| Operation | Tool |
|-----------|------|
| Create/update/delete campaign | `create_campaign` / `update_campaign` / `delete_campaign` |
| Create license key batch | `create_license_key_batch` |
| Invalidate license key batch | `invalidate_license_key_batch` |

Workflow:
1. Execute write operation → system auto-creates approval request
2. `list_approvals(status=pending)` → check pending approvals
3. Superadmin executes `approve_approval(id)` or `reject_approval(id, reason)`
4. Action executes automatically upon approval

---

## Social Media Operations

Social media is a key acquisition and retention channel. Two MCP servers provide direct platform access.

### Platform Tool Reference

#### Twitter (`mcp-twikit`) — 8 tools

| Tool | Purpose | Write |
|------|---------|-------|
| `search_twitter` | Search tweets by keyword (sort: Top/Latest) | |
| `get_user_tweets` | Get a user's tweet history | |
| `get_timeline` | Home timeline (For You) | |
| `get_latest_timeline` | Home timeline (Following) | |
| `post_tweet` | Post tweet (supports media, reply, @mentions) | ✓ |
| `delete_tweet` | Delete a tweet | ✓ |
| `send_dm` | Send direct message (supports media) | ✓ |
| `delete_dm` | Delete a direct message | ✓ |

Auth: Twitter username + email + password (env vars in `~/.claude/settings.json`). No official API key needed (uses twikit reverse-engineered client). Rate limit: 300 tweets / 1000 DMs per 15-min window.

#### 小红书 (`xiaohongshu-mcp`) — 13 tools

| Tool | Purpose | Write |
|------|---------|-------|
| `check_login_status` | Check if logged in | |
| `get_login_qrcode` | Get QR code for login | |
| `delete_cookies` | Reset login state | ✓ |
| `publish_content` | Publish image+text post (title/body/images/tags/schedule/visibility) | ✓ |
| `publish_video` | Publish video post (local file only) | ✓ |
| `search_feeds` | Search posts (sort/type/time/location filters) | |
| `list_feeds` | Home feed recommendations | |
| `get_feed_detail` | Post details + comments | |
| `get_user_profile` | User profile info | |
| `post_comment` | Comment on a post | ✓ |
| `reply_comment` | Reply to a comment | ✓ |
| `like_feed` | Like / unlike a post | ✓ |
| `favorite_feed` | Bookmark / unbookmark a post | ✓ |

Auth: QR code scan via `xiaohongshu-login` CLI. Cookie-based session. Must start `xiaohongshu-mcp` service first (`http://localhost:18060/mcp`).

Limits: Title ≤20 chars, body ≤1000 chars, ~50 posts/day. No simultaneous web login (kicks MCP session; mobile app is fine).

---

### Social Media Playbook: Proactive Engagement

**Goal: Build brand presence and drive organic acquisition through community engagement.**

#### Step 1: Monitor Relevant Conversations

```
# Twitter: find users discussing VPN problems in China
search_twitter(query="翻墙 慢 OR 连不上 OR 不稳定", sort_by="Latest", count=20)
search_twitter(query="VPN China blocked", sort_by="Latest", count=20)

# 小红书: find VPN-related posts
search_feeds(keyword="翻墙工具推荐", sort="time_descending", count=20)
search_feeds(keyword="VPN 推荐 2026", sort="general", count=20)
```

#### Step 2: Engage with Value

Reply to users experiencing VPN issues with helpful, non-spammy responses:

```
# Twitter: reply to a tweet about VPN instability
post_tweet(
  text="我之前也遇到过这个问题，后来换了个用 QUIC 协议的工具就稳定多了。可以试试看 kaitu.io，有免费试用",
  reply_to="TWEET_ID"
)

# 小红书: comment on a relevant post
post_comment(note_id="NOTE_ID", content="写得很详细！补充一个选择：Kaitu 用的是自研 k2cc 拥塞控制，丢包 30% 还能满速，适合网络环境差的场景")
```

**Tone guidelines:**
- Helpful first, promotional second — provide genuine value
- Match platform language style (Twitter: concise; 小红书: conversational + emoji-friendly)
- Never bash competitors by name
- Include specific technical differentiators (k2cc, QUIC, zero-config) over generic claims

**Anti-fingerprint (MANDATORY for ALL posts, comments, and DMs):**

All content sent to social media platforms MUST be de-fingerprinted to read as naturally human-written. AI-generated text is easily detected and flagged by platforms. Apply these rules to every piece of content before sending:

1. **Vary sentence structure** — mix short and long sentences, use fragments, start with conjunctions ("但是…", "And…"). Never use uniform sentence patterns.
2. **Imperfect punctuation** — occasional missing comma, casual ellipsis ("..."), platform-native conventions (Twitter: no period at end; 小红书: liberal use of "～", "！", "hhhh", "yyds")
3. **Colloquial language** — use slang, contractions, internet speak appropriate to the platform. 小红书: "绝了", "真的会谢", "姐妹们". Twitter: abbreviations, informal tone.
4. **No AI patterns** — NEVER use: "首先…其次…最后", "值得注意的是", "总的来说", "In conclusion", "It's worth noting", "Here's the thing:", numbered lists in comments, or any structured enumeration pattern.
5. **Asymmetric depth** — elaborate on one point, mention others briefly. Don't give equal weight to everything (a dead giveaway of AI writing).
6. **Personal voice** — use first person ("我用了半年了", "I've been using this for months"), share subjective experience, express mild opinions with hedging ("感觉比之前好不少", "seems way better tbh").
7. **Platform-native formatting** — 小红书: emoji between paragraphs, hashtag at end. Twitter: no hashtag spam, 1-2 max. Never format comments like a blog post.
8. **Vary across posts** — never reuse the same sentence structures, openings, or sign-offs across multiple posts. Each post should feel independently written.
9. **Length variation** — not every reply needs to be the same length. Some should be a single short sentence, others 2-3 sentences. Match the energy of what you're replying to.

#### Step 3: Content Publishing

```
# Twitter: share product updates / technical insights
post_tweet(text="k2 0.4.2 发布 🚀 新增智能路由规则预览，连接前就能看到哪些流量走代理。下载: kaitu.io/install")

# 小红书: publish how-to guide with images
publish_content(
  title="2026翻墙工具横评",
  content="详细对比了 5 款主流工具...",
  images=["/path/to/comparison-chart.png"],
  tags=["翻墙", "VPN", "科学上网"]
)
```

#### Step 4: Track & Respond to Replies

```
# Twitter: check replies to our recent tweets
get_user_tweets(username="kaitu_io", tweet_type="replies", count=20)

# 小红书: check comments on our posts
get_feed_detail(note_id="NOTE_ID")  # includes comments
```

Reply to every genuine question within 24 hours.

---

### Social Media Playbook: GFW Event Amplification

**Trigger: GFW upgrade causing widespread blocks (combine with GFW Event Response playbook)**

During GFW events, social media demand surges. Capitalize with timely content:

1. **Twitter rapid response thread**:
   ```
   post_tweet(text="⚠️ 检测到大规模封锁升级。Kaitu 用户：请更新到最新版本，我们已经部署了新协议应对。下载: kaitu.io/install\n\n目前各节点状态 👇")
   ```

2. **小红书 guide post**:
   ```
   publish_content(
     title="最新翻墙方法（4月实测有效）",
     content="今天很多工具都挂了，分享一个目前还能用的方案...",
     images=[...],
     tags=["翻墙", "GFW", "科学上网", "VPN推荐"]
   )
   ```

3. **Monitor competitor mentions** for users looking to switch:
   ```
   search_twitter(query="clash 挂了 OR v2ray 连不上 OR shadowsocks 被封", sort_by="Latest")
   search_feeds(keyword="clash 用不了", sort="time_descending")
   ```

4. **Engage switchers** with helpful migration guidance (not hard sell)

---

### Social Media Playbook: License Key Distribution

**Goal: Distribute trial keys through social media channels for trackable acquisition.**

1. Create a channel-specific license key batch:
   ```
   create_license_key_batch(
     name="Twitter-GFW-Event-202604",
     source_tag="twitter-gfw-202604",
     recipient_matcher="never_paid",
     plan_days=7,
     quantity=50,
     expires_in_days=14
   )
   ```

2. After approval, get keys: `list_license_key_batch_keys(batch_id=...)`

3. Distribute via social media:
   ```
   post_tweet(text="送 50 个 Kaitu 7天体验码 🎁 评论区留言「想试」我私信发你。首次注册用户专享。")
   ```

4. Fulfill via DM:
   ```
   send_dm(user_id="...", message="你的 Kaitu 体验码: XXXX-XXXX\n兑换地址: kaitu.io/redeem\n有问题随时问 😊")
   ```

5. Track channel performance: `license_key_batch_stats(batch_id=...)` + `license_key_batch_stats_by_source`

---

### Social Media Safety Rules

- **Never post credentials, internal URLs, server IPs, or employee names**
- **Rate limit awareness**: Don't exceed 10 replies/hour on any platform to avoid triggering anti-spam
- **Review before posting**: All social media posts require user confirmation before `post_tweet` / `publish_content` / `post_comment`
- **No automated mass-commenting**: Each comment must be contextually relevant to the target post
- **Account safety**: mcp-twikit uses reverse-engineered auth — use a dedicated social media account, not a personal one
- **小红书 session**: Don't open web xiaohongshu.com while MCP is running (session conflict)
- **License key DM distribution**: Max 20 DMs per session; pause and confirm with user before continuing

---

## Safety Rules

- **Email sends >100 recipients**: Must confirm with user before executing
- **License key batches >1000 keys**: Must confirm with user before executing
- **Before `update_retailer_level`**: Must `create_retailer_note` with reason first
- **`batch_id` must be unique**: Format `mcp:YYYY-MM-DD:purpose` to prevent duplicate sends
- **User data is read-only**: Never delete users or modify passwords
- **Recipient lists**: Build via `center-ops` database queries or `lookup_user`; never guess email addresses
- **Campaigns targeting all users** (`matcher_type=all`): Must confirm with user before executing
