# Apple 退款收回（开途 + Overleap）设计 — A 期补充（A2）

> 状态：设计 v1，待 review。随 A 期（`feat/overleap-trial-payment`）一起合并、一起部署。
> 关联：`2026-10-07-overleap-payment-phase-a-design.md`（PaidThrough 模型、Stripe 收回）。

## 1. 现状与已证实的问题

入口：`api_apple_webhook.go` 的 `REFUND` / `REVOKE` → `revokeSubscription`（`logic_apple_iap.go`）。
现逻辑：锁用户 → 撤订单返现 → **仅当 `now < ExpiredAt <= sub.CurrentPeriodEnd`** 时把 `ExpiredAt` 扣到 now → 订阅标 `revoked`。

| # | 问题 | 证据 | 影响 |
|---|------|------|------|
| P1 | 用户有任何叠加时长（赠送、卡密、网页购买、邀请奖励）时，`ExpiredAt > CurrentPeriodEnd`，**一天都不收回** | 代码条件；开途 `invite.purchase_reward_days=30`，奖励在 `creditAppleTransaction` 里**先于**购买时长入账 → 每个"被邀请首购"都必然命中 | 开途：被邀请用户买年付 → Apple 退款 → 保留 395 天 |
| P2 | 退的是**已消耗的旧期**交易（续订后才退上一期），整条订阅被标 revoked，且若命中条件会把**当前已付的这一期**也扣掉 | 代码不看被退交易是哪一期 | 多收；Overleap 条款 7.5 只允许"结束该笔付款买到的服务" |
| P3 | 被退款的交易若当初漏入账，事后客户端恢复购买 / 对账再入账时**照样发时长** | `creditAppleTransaction` 不看 `RevocationDate` | 退款后白拿 |
| P4 | 漏收 `REFUND` webhook 时，对账 worker 看到 Apple 状态 Revoked 只打 error 日志 | `reconcileAppleSubscription` 末段 | 永不收回 |
| P5 | Apple 退款会把 `IsFirstOrderDone` 翻回 false → 再买一单又算"首单" → **邀请双方各再领 30 天**；可"买—退—买—退"反复刷 | `revokeIAPOrderCashbackInTx` + `grantInvitePurchaseRewardInTx` 只看 `IsFirstOrderDone` | 开途邀请奖励可刷 |
| P6 | `REVOKE`（家庭共享撤销）与 `REFUND` 同路处理，不区分被撤交易的归属 | webhook switch | 理论上可误收购买者本人的时长 |
| P7 | 退款后同一订阅链（同 originalTransactionId）重新订阅，状态永远停在 revoked（`deriveVerifiedStatus` 不复活）→ app 显示"未订阅" | 代码 | 付费用户看不到自己的订阅 |
| P8 | `REFUND_REVERSED`（Apple 撤销了退款）未处理 | webhook default 分支 | 用户被收回后不会恢复 |

线上（2026-10-08 只读查询）：开途 14 笔 Apple 年付订单、13 个有效订阅，真实 Apple 退款 0 笔（唯一"已退款"是沙盒作废）；13 个里 1 个当前有叠加时长。Overleap 0 个生产 Apple 订阅。**洞是真的，但目前没有被利用，无需追溯处理。**

Apple 现行事实（官方文档，已核对）：
- `REFUND` 交易带 `revocationDate`；2025-12 起另带 `revocationType`（`REFUND_FULL` / `REFUND_PRORATED` / `FAMILY_REVOKE`）与 `revocationPercentage`（自动续订按剩余时间折算）。qtoolkit `TransactionInfo` 尚无后两个字段——**本设计不依赖它们**（见 §3 规则）。
- `REFUND_REVERSED`、`REFUND_DECLINED`、`CONSUMPTION_REQUEST`（2024-04 起覆盖自动续订）均为 V2 通知。
- Send Consumption Information 要求**用户事先明确同意**（opt-in），12 小时内回复。

## 2. 两个品牌的用户差异 → 策略

| | 开途 | Overleap |
|---|---|---|
| 用户 | 中国大陆用户，用海外 Apple ID 购买；网上流传"苹果退款教程"，退款滥用是现实风险 | 欧美用户；Apple 是商户，欧盟/英国 14 天撤回由 Apple 执行 |
| 在售商品 | 只有年付（14/14 单是"1年套餐"） | Apple 渠道尚未上线（C 期 iOS 试用后才有） |
| 叠加时长来源 | 多：邀请奖励（双方各 30 天）、卡密、活动、网页购买 | 少：邀请功能关闭（`features.invite=false`） |
| 主要风险 | **少收**（白嫖、刷邀请奖励） | **多收**（扣掉已付期 → 投诉/违反条款 7.5） |

**结论：一条规则同时满足两边，代码里不按品牌分支。**

> **收回被退款交易自身尚未消耗的付费时长；不碰赠送时长，不追讨已消耗的旧期。**

- 开途要的"别让人白嫖"：P1 修好后，退当期年付必收回整期剩余（不论叠了多少赠送）；邀请奖励只在开途存在，单独按 §3.4 处理（品牌差异由功能开关天然体现，不写 `if brand`）。
- Overleap 要的"别多收"：只扣这笔交易买到、且还没用掉的部分——正是条款 7.5 "the access it paid for ends"。旧期已消耗不追讨（单方抵扣在英国消费者法下站不住）。
- 开途只卖年付，Apple 退款窗口远短于一年，"续订后退上一期"实际不会发生；所以"不追讨旧期"对开途几乎无损，对 Overleap（C 期可能有月付）是必要保护。

## 3. 设计

### 3.1 新表 `AppleRefund`（不复用 `SubscriptionCredit`）

`SubscriptionCredit` 有 4 处读取方按 `credited_seconds` 求和/统计（`orderEntitlementSecondsInTx`、funnel facts ×2、retention），写入负数行会污染它们。独立表：

```go
type AppleRefund struct {
    ID                    uint64 `gorm:"primarykey"`
    CreatedAt             time.Time
    UserID                uint64 `gorm:"not null;index"`
    SubscriptionID        uint64 `gorm:"not null;index"`
    OriginalTransactionID string `gorm:"type:varchar(64);not null;index"`
    TransactionID         string `gorm:"type:varchar(64);not null;uniqueIndex"` // 幂等键
    RevocationDate        int64  // Apple revocationDate（秒）；缺失记 now
    RevocationReason      int32
    Ownership             string `gorm:"type:varchar(24)"`
    Credited              bool   // 这笔交易是否曾由我们入账
    CutSeconds            int64  // 收回的付费时长
    InviteCutSeconds      int64  // 收回的被邀请购买奖励
    InviteHistoryID       uint64 // 被收回的那条 invited_reward 历史行
    MarkedRevoked         bool   // 本次是否把订阅置为 revoked
    Source                string `gorm:"type:varchar(16)"` // webhook | reconcile
    Note                  string `gorm:"type:varchar(255)"` // 跳过原因等
}
```

### 3.2 `PaidThrough` 扩展到 Apple

A 期的 `Subscription.PaidThrough`（"已付费、已入账的权益覆盖到的时刻"）目前只有 Stripe 维护。`creditAppleTransaction` 在真正入账（非 `alreadyCredited`）后同样维护：

```
base     = max(入账前 user.ExpiredAt（邀请奖励重载之后）, now)
granted  = max(入账后 user.ExpiredAt（含 cover-through）− base, 0)
paid_through = max(sub.PaidThrough, now) + granted
```

与 Stripe 同口径（含 cover-through 修正量）。grace 宽限期 cover-through（`applyRenewalInfo`）**不是**付费时长，不动 `PaidThrough`。存量行 `PaidThrough=0`：收回路径回落 `CurrentPeriodEnd`（复用 A 期 helper），且被 §3.3 的 `tExp` 上界兜住，**无需回填**。

### 3.3 `applyAppleRefund(ctx, otx, txn *appstore.TransactionInfo, source)`（取代 `revokeSubscription`）

单事务、`withDeadlockRetry`，锁序 **订阅 → 用户 → 订单**（与入账路径一致）：

1. `FOR UPDATE` 锁订阅行（apple, otx）。不存在 → 返回（webhook 层已过滤）。
2. 幂等：`AppleRefund{TransactionID}` 已存在 → 返回 no-op（覆盖 Apple 重投、REFUND 与 REVOKE 并发——二者在订阅行锁上串行）。
3. 锁用户行。
4. `credited := SubscriptionCredit{apple, txn.TransactionId}` 存在。
5. `ord := revokeIAPOrderCashbackInTx(...)`——改为返回结果 `{Found, AlreadyRefunded, WalletRefunded, OtherPaidCount}`，其余语义不变（撤返现、标退款、必要时翻 `IsFirstOrderDone`、双退哨兵）。
6. 付费时长收回：
   ```
   eligible = credited && txn.InAppOwnershipType != FAMILY_SHARED && !ord.WalletRefunded
   tExp     = txn.ExpiresDate/1000
   paid     = PaidThrough>0 ? PaidThrough : CurrentPeriodEnd
   cut      = eligible ? max(0, min(tExp, paid, user.ExpiredAt) − now) : 0
   user.ExpiredAt −= cut;  若 cut>0：sub.PaidThrough = paid − cut
   ```
   - `tExp` 上界 = 只收这笔交易自己覆盖的时间：已过期的旧期 → 0（P2）。
   - `paid` 上界 = 不碰赠送（P1）：赠送在付费之前叠加时，`PaidThrough` 只记付费段；在之后叠加时，`ExpiredAt − cut` 恰好留下赠送段。
   - `WalletRefunded`（此单此前已被后台退到钱包，`ProcessOrderRefund` 已扣过权益）→ 不再扣，避免双扣；双退哨兵照旧告警。
   - 未入账（`credited=false`）→ 没发过，不扣（P6 家庭共享交易我们从不入账，天然落此分支）。
7. 邀请购买奖励收回（只有开途会命中）：条件 `ord.Found && ord.OtherPaidCount == 0`（退完后该用户已无有效付费单——"购买奖励以保留一笔购买为前提"）且存在该用户的 `invited_reward` 历史行 R、且没有任何 `AppleRefund.InviteHistoryID = R.ID`：
   `inviteCut = min(R.Days*86400, user.ExpiredAt − now)`，扣减并记录 `InviteHistoryID`。
   **邀请人那 30 天不自动收回**：邀请人历史行只记 `reference_id=邀请码`，无法精确归到这个被邀请人；邀请人是第三方。改为告警 `[INVITE-REFUND]`（被邀请人、邀请人、天数），由运营判断是否手工处理。
8. 历史：`cut>0` / `inviteCut>0` 各写一条 `UserProHistory{Type: refund, Days: -floor(s/86400)}`，`Reason` 按品牌给用户可读文案（开途中文、Overleap 英文；ProHistory 页原样显示 reason）。
9. 订阅状态：`credited && ownership≠FAMILY_SHARED && tExp > now` → `revoked`，`MarkedRevoked=true`；旧期退款不改状态（Apple 侧订阅仍有效）。
10. 写 `AppleRefund` 行。提交后发 `[APPLE-REFUND]` 告警（品牌、用户、交易、cut 天数、是否命中邀请、source）——量很小，开途用来盯滥用模式。

### 3.4 防刷：被邀请购买奖励每人一次（P5）

`grantInvitePurchaseRewardInTx`：被邀请人已有任何 `invited_reward` 历史行 → 跳过（双方都不发）。`invited_reward` 只在这一处写入（已 grep 证实），判据干净。网页订单路径一并受益（后台退款后重买也不再重复发）。

### 3.5 入账门：已退款交易永不入账（P3）

`creditAppleTransaction`：把去重查询提前到 upsert 之前；`info.RevocationDate > 0 && !alreadyCredited` → 记 warn 并 `return nil`，**不建订阅行、不改任何状态**。已入账的被退交易走原 `alreadyCredited` 分支（只刷新 plan-state，不复活——见 3.6）。

### 3.6 退款后重新订阅复活状态（P7）

`creditAppleTransaction` 状态推导：`sub.Status == revoked && !alreadyCredited && info.RevocationDate == 0 && info.PurchaseDate/1000 > max(AppleRefund.RevocationDate WHERE otx AND MarkedRevoked)` → 按非 revoked 推导（active/expired）。即"退款之后发生的新付款"才复活；被退交易本身、退款前的旧交易重放都不复活。Stripe 的 revoked 终态四道门不受影响（Apple 专属分支）。

### 3.7 其它通知与对账

- `REFUND`、`REVOKE` → `applyAppleRefund(..., "webhook")`。
- `REFUND_REVERSED`（P8）→ 查 `AppleRefund{TransactionID}`，告警 `[APPLE-REFUND-REVERSED]` 附收回秒数，运营用 MCP `add_user_membership` 手工恢复。理由：极少发生，且自动恢复要连带反撤返现、反标订单，复杂度不值得。
- `REFUND_DECLINED`、`CONSUMPTION_REQUEST` → 仅 info 日志（Consumption 见 §5）。
- 对账（P4）：Apple 状态 Revoked 且本地非 revoked，且 `st.txn.RevocationDate > 0` → `applyAppleRefund(..., "reconcile")`；否则保留原 error 日志。对账在此之前调用的 `creditAppleTransaction(st.txn)` 因 §3.5 不会给被退交易入账。

## 4. 文案与客服

- 开途条款（`web/public/legal/terms-of-service.md`）新增 7.5「App Store 购买」：由 Apple 收费、退款按 Apple 政策由 Apple 处理、我们无法退到钱包；Apple 退款后，该笔购买对应的剩余会员时长收回，因该购买获得的邀请奖励一并收回。Overleap 条款 7.5 已覆盖，不改。
- 客服知识库（开途 + Overleap）：Apple 退款后会员变化的解释与 `REFUND_REVERSED` 手工恢复流程。

## 5. 明确不做

- **Send Consumption Information**：需要 app 内 opt-in 同意界面 + 隐私政策 + 隐私标签更新；现在 0 笔真实退款，收益为零。等开途出现退款滥用信号（`[APPLE-REFUND]` 告警频次）再做；届时策略：Overleap 回 `GRANT_PRORATED`（与 14 天按天折算一致），开途按用量（条款 7.1 的 1GB 口径）回 `DECLINE` 或 `GRANT_PRORATED`。
- `revocationType` / `revocationPercentage` 解析：规则不需要；等 qtoolkit 升级时顺手入表做审计。
- 自动收回邀请人奖励：无法精确归因，告警代替。
- 网页订单后台退款路径（`ProcessOrderRefund`）收回邀请奖励、维护 Apple `PaidThrough`：后台退款由客服人工决定，可手工处理；已知残留：IAP 单被后台退到钱包后，该订阅 `PaidThrough` 偏大，之后若再有 Apple 退款，`min(…, ExpiredAt)` 可能扣到赠送——双退本身会触发 `[DOUBLE-REFUND]` 告警，人工兜底。

## 6. 测试要点（每条做变异验证）

1. 赠送在购买**前**叠加（邀请首购）→ 退当期：收回整期剩余，赠送保留；若无其它付费单，被邀请奖励也收回。
2. 赠送在购买**后**叠加 → 同上，赠送保留。
3. 退已过期旧期（有更新一期在跑）→ cut=0、状态不变、订单/返现照撤。
4. 重投 / REFUND+REVOKE 并发 → 只处理一次。
5. 家庭共享交易 / 未入账交易 → cut=0、不改状态。
6. 订单已被后台退到钱包 → 不再扣权益、双退告警。
7. 被退交易事后入账（verify / 对账）→ 不发时长、不建行。
8. 退款后同链新付款 → 状态复活为 active 并入账；被退交易重放不复活。
9. 被邀请人退款后重买 → 双方都不再发奖励。
10. 对账发现 Revoked → 走同一收回路径；Apple 未给 revocationDate → 仅告警。
11. `PaidThrough` 维护：首购、续订、cover-through、宽限期（不动）。
12. 存量 `PaidThrough=0` 行退款 → 由 `tExp` 兜住，不扣赠送。
13. `REFUND_REVERSED` → 告警含收回秒数，不改数据。
