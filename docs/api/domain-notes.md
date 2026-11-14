# Approvals, chat, enterprise, campaigns, device account — notes

> Moved verbatim from `api/CLAUDE.md` (2026-11-14) so the per-session doc stays small. **Read this before changing `logic_approval*.go`, `api_chat*.go` / `logic_chat_*.go`, `api_admin_enterprise.go` / `resolveSlotBindings`, `logic_campaign.go` matchers, `logic_recall_offer.go` / `recallMailer`, `LicenseKeyBatch`, or `/api/auth/device-account/restore`.** Short invariants stay in the same-named `api/CLAUDE.md` sections.

## Approval Workflow (Maker-Checker)

Critical admin operations (EDM, campaigns, plans, withdrawals, hard delete, license key batches, order refunds, Stripe withdrawal) go through `SubmitApproval()`. Superadmin bypasses and executes synchronously; others create a pending record another admin approves.

- **Files**: `logic_approval.go` (service + action labels), `logic_approval_callbacks.go`, `api_admin_approval.go`. Callbacks are registered by `RegisterApprovalCallback(action, cb)` in `InitWorker()` — that list is the source of truth for actions.
- **Pattern**: validate → `approvalID, executed, err := SubmitApproval(c, action, params, summary)` → `!executed` ⇒ `PendingApproval(c, approvalID)`; superadmin path continues to `Success`. **Each callback re-validates preconditions** (state may have changed since submission).
- **Concurrency**: atomic `UPDATE WHERE status='pending'` + `RowsAffected` check prevents double-approve. Slack DM notifications are best-effort.

## Chat

Spec `docs/superpowers/specs/2026-10-01-support-chat-design.md`. Entry: `api_chat.go` (`/api/chat/*`), `api_chat_slack_events.go`, `api_admin_chat.go`, `logic_chat_*.go`, `model_chat.go` (`Guest`, `GuestIdentity`, …), `worker_chat.go`.
- Gates: viper `chat.enabled` + `chat.preview_enabled` (both false = hard off, even for existing cookies) and `chat.brands` allow-list (empty = all brands off).
- Slack is the agent's primary UI: one private channel per conversation, every message mirrored in id order; `slack_mirrored_at` is an outbox marker backfilled by the `chat:slack-sweep` cron. Slack failures only log — they never fail `appendMessage`.
- AI replies via `qtoolkit/openai/filesearch` against the brand's `BrandConfig.ChatAIStore` (KB sources in `docs/customer-service/<brand>/`, synced by `scripts/sync-kb.sh`); errors / empty / cap (`chatAIMaxReplies`) hand off to a human.

## Enterprise

Spec `docs/superpowers/specs/2026-07-22-enterprise-router-multi-ssid-design.md`. Admin `/app/enterprise/*` (`api_admin_enterprise.go`, opsAdmin: read `RoleDevopsViewer|Editor`, write `RoleDevopsEditor`); models `EnterpriseCustomer` / `EnterpriseLine` / `EnterpriseRouterBinding` (`model.go`). `/api/subs` adds `slot_bindings` via `resolveSlotBindings` (`logic_enterprise.go`) — snake_case wire contract with k2 `config.SlotBinding`, `tunnel_index` indexes the same response's `tunnels`. **Deliberately health-blind**: quota cutoff / node death never remove a slot; only a human rebind does.

## Campaign Matcher Semantics

`first_order` and `vip` are exact mirrors and must never collapse into the same meaning — `logic_campaign_matcher_test.go` pins both. **History (do not repeat):** `first_order` once meant "已付费" while every author read it as "new customer" — all 5 `first_order` campaigns (FIRST_ORDER_20, READY4U, STAYFREE, SMOOTHDAY, KEEPGOING) silently rejected 100% of recipients with `ErrorInvalidCampaignCode` (fixed 2026-06-06). Keep new matcher names describing the **audience**, and mirror the admin UI label in `web/.../manager/campaigns/page.tsx`.

**召回邮件优惠（abandoned / winback / trial-recall）只有两个码，不随阶段加深**：新客 `NEW10`、老客 `BACK10`（`logic_recall_offer.go`）；邮件标价调纯函数 `campaignPrice`，**绝不调 `applyCampaign`**（它累加使用次数）。10% 对照组记为 `email_send_logs` skipped + `error_msg=holdout`，`GET /app/recall-stats` 据此算增量——新召回链路要沿用 `recallMailer`，否则没有对照组。Spec `docs/superpowers/specs/2026-11-03-recall-offer-flatten-design.md`。

**`LicenseKeyBatch` is a different thing — don't fold it into campaigns**: 授权码批次自带渠道标签 (`sourceTag`)、兑换条件 (`recipientMatcher`)、过期时间，统计兑换率与兑换→付费转化；创建走审批。

## Device Account

- **The subscription follows the Apple ID** (App Review 5.1.1(v), 4.4.16 rejection; spec revision 2026-11-10): `/device-account/restore` admits a device into the subscription owner's account whatever the owner's identity — zero-identity owner = move (cap 1), identity owner = join (`checkDeviceLimitOrKick`). Never reintroduce "owner has an identity → 409004 / ask to sign in". A device held by an identity account is switched only when the request is authenticated as that account (route carries `AuthOptional`).
