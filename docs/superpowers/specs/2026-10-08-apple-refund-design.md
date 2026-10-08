# Apple 退款收回（开途 + Overleap）设计 — A 期补充（A2）

> 状态：设计 v3（合入 review 第 1、2 轮；v3 结构性改动见 §3.0）。随 A 期（`feat/overleap-trial-payment`）一起合并、一起部署。
> 关联：`2026-10-07-overleap-payment-phase-a-design.md`（PaidThrough 模型、Stripe 收回）。

## 1. 现状与已证实的问题

入口：`api_apple_webhook.go` 的 `REFUND` / `REVOKE` → `revokeSubscription`（`logic_apple_iap.go`）。
现逻辑：锁用户 → 撤订单返现 → **仅当 `now < ExpiredAt <= sub.CurrentPeriodEnd`** 时把 `ExpiredAt` 扣到 now → 订阅标 `revoked`。

| # | 问题 | 影响 |
|---|------|------|
| P1 | 有任何叠加时长（赠送、卡密、网页购买、邀请奖励）→ `ExpiredAt > CurrentPeriodEnd` → **一天都不收回**。开途邀请奖励（线上 30 天）在 `creditAppleTransaction` 里**先于**购买时长入账，每个"被邀请首购"必然命中 | 被邀请用户买年付→退款→保留 395 天 |
| P2 | 退已消耗的旧期，整条订阅也被标 revoked，命中条件时连当前已付期一起扣 | 多收；违反 Overleap 条款 7.5 |
| P3 | 被退交易若当初漏入账，事后恢复购买 / 对账再入账照样发时长 | 白拿 |
| P4 | 漏收 `REFUND` webhook：对账 worker 只扫"周期 48h 内到期"的行，年付要等 11 个月才发现；且发现 Revoked 只打日志 | 实际永不收回 |
| P5 | 退款把 `IsFirstOrderDone` 翻回 false → 再买又是"首单" → 邀请双方再各领 30 天；可反复刷。另：奖励天数配置为 0 时防刷判据缺失 | 开途邀请奖励可刷 |
| P6 | 家庭共享 `REVOKE` 与 `REFUND` 同路，不看交易归属 | 理论误收 |
| P7 | 退款后同链重订阅，状态永停 revoked；且 `CurrentPeriodEnd` 仍是被退期末 → 新一期按续订 delta 只记 20 天、叠加在旧期末上**吞掉赠送时长** | 付费用户显示未订阅；赠送被吞；之后后台退款少扣 |
| P8 | `REFUND_REVERSED` 未处理 | 用户被收回后不恢复（英国消费者法下：已付费未供应） |
| P9 | 沙盒交易（`GetTransaction` 先正式后沙盒回退）照发邀请奖励、照置 `IsFirstOrderDone` | TestFlight 购买刷邀请奖励 |
| P10 | 开途 IAP 订单可被后台"退到钱包"（可提现 USDT），用户之后再找 Apple 退一次 | 双重退款，钱已提走才告警 |
| P11 | 分销返现冻结 30 天，Apple 退款窗口约 90 天 → 分销商先提现、后被退款，钱包扣成负数无人知 | 现金损失 |
| P12 | 退款不减分销商 `paid_user_count`；重买又 +1 | 10 个"退了再买"即自动升 L2（永久高比例） |

线上（2026-10-08 只读）：开途 14 笔 Apple 年付单、13 个有效订阅，真实 Apple 退款 0 笔；Overleap 0 个生产 Apple 订阅。**洞是真的，未被利用，无需追溯。**

Apple 现行事实（官方文档已核对）：退款交易带 `revocationDate`；2025-12 起带 `revocationType`（`REFUND_FULL`/`REFUND_PRORATED`/`FAMILY_REVOKE`）、`revocationPercentage`（自动续订按**剩余时间**折算）；`REFUND_REVERSED`/`REFUND_DECLINED`/`CONSUMPTION_REQUEST` 为 V2 通知；Get Notification History 支持 `onlyFailures=true`；Send Consumption Information 需用户事先 opt-in。qtoolkit v1.5.32 无 `REFUND_REVERSED` 常量（`NotificationType` 是 string，用字面量）、`TransactionInfo` 无 revocationType/Percentage（自行解 JWS payload 补读）。**未核实**：退款后同 Apple ID 重订阅是否沿用同一 `originalTransactionId`——设计两种都覆盖（新 otx 走首购绑定）。

## 2. 两个品牌的用户差异 → 策略

| | 开途 | Overleap |
|---|---|---|
| 用户 | 大陆用户、海外 Apple ID；"苹果退款教程"流传，滥用是现实风险 | 欧美用户；Apple 为商户，代执行 14 天撤回 |
| 在售 | 只有年付 | Apple 渠道 C 期才上线（可能有月付） |
| 叠加时长 | 多：邀请（双方各 30 天）、卡密、活动、网页购买 | 少：邀请关闭 |
| 钱的出口 | 钱包可提现、分销返现 | 无钱包、无分销 |
| 主要风险 | **少收**：白嫖、刷邀请、分销套现 | **多收**：扣到已付期、退款撤销后不恢复 |

**时长算术一条规则，代码不写 `if brand`**：

> **收回被退交易自身尚未消耗的付费时长；不碰赠送；不追讨已消耗的旧期；退款被撤销就还回去。**

开途特有的风险全部挂在**开途独有的功能**上（邀请、钱包、分销）——Overleap 这三项本就关闭（`features.invite/wallet=false`，无分销商），修复天然只作用于开途，不需要品牌分支。

## 3. 设计

### 3.0 两条结构性原则（v3）

1. **退款不改 `current_period_end`，只改状态。** v2 的"退款时把周期末设成 now"会在"退款→隔 N 天重订阅"时多发 N 天、在"退款被撤销"时把周期末改坏（review 第 2 轮 B1/B2）。改为：**退款之后发生的新付款，按首购口径入账**（时长 = 该期本身，叠在 `max(ExpiredAt, now)` 之上），不经 `applyRenewalCredit` 的 delta——周期末是多少都无所谓。
2. **退款 / 撤销退款的先后以 Apple 签名时间判定，不以到达顺序。** webhook 实时到达、补漏任务事后回放、Apple 重投，三者顺序都不可信；用通知 payload 的 `signedDate` 比较。

### 3.1 新表与新列

`SubscriptionCredit` 有 4 处读取方按 `credited_seconds` 求和，不能写负数行，故新表：

```go
// AppleRefund：一笔 Apple 交易的退款状态（每笔交易一行，TransactionID 唯一）。
type AppleRefund struct {
    ID, CreatedAt, UpdatedAt, UserID, SubscriptionID
    OriginalTransactionID string // index
    TransactionID         string // uniqueIndex
    RefundSignedAt        int64  // 最近一次被采纳的 REFUND 通知 signedDate（秒）
    ReversedSignedAt      int64  // 最近一次被采纳的 REFUND_REVERSED 通知 signedDate（秒）
    Active                bool   // 当前是否处于"已退款"状态（= RefundSignedAt > ReversedSignedAt）
    RevocationDate        int64  // 秒；Apple 缺失时记处理时刻
    RevocationReason      int32
    RevocationType        string // 自行解 JWS 补读，审计
    RevocationPercentage  int32  // 毫单位，审计
    Credited              bool
    CutSeconds            int64  // 本轮退款收回的付费时长（撤销时据此还）
    MarkedRevoked         bool   // 本轮退款把订阅置为 revoked
    Source                string // webhook | sweep | reconcile
    Note                  string
}

// InviteRewardGrant：一次"被邀请首购奖励"。被邀请人唯一 → 一生一次；记录触发它的那笔购买。
type InviteRewardGrant struct {
    ID, CreatedAt, UpdatedAt
    InviteeUserID uint64 // uniqueIndex
    InviterUserID uint64
    InviteCodeID  uint64
    TriggerKind   string // apple_txn | order ；index(kind, ref)
    TriggerRef    string
    InviteeSeconds, InviterSeconds       int64 // 实发
    Reversed                             bool
    InviteeCutSeconds, InviterCutSeconds int64 // 撤回时实扣
}

// Order 新列
RetailerCountedID uint64 // 这笔单计入了哪个分销商配置的 paid_user_count；0 = 未计入（含全部存量单）
```

### 3.2 `PaidThrough` 扩展到 Apple

`creditAppleTransaction` 真正入账后，**单独一条 `Update`** 写入（`Save(&sub)` 发生在 credit 计算之前）：

```
base         = max(入账前 user.ExpiredAt（邀请奖励重载之后）, now)
granted      = max(入账后 user.ExpiredAt（含 cover-through）− base, 0)
paid_through = max(sub.PaidThrough, now) + granted
```

与 Stripe 同口径。grace cover-through 不动 `PaidThrough`。已接受偏差（写进代码注释）：
- 宽限期后续订成功：`PaidThrough` 比真实付费段少一个宽限窗（≤16 天）→ 该期被退时少收至多 16 天。
- 赠送在购买前、晚退款：`PaidThrough` 是"付费时长计数"，按先消耗付费段计 → 用户保留 min(赠送, 已用) 天（与 Apple `REFUND_PRORATED` 的剩余时间口径一致）。
- 存量 `PaidThrough=0`：收回回落 `CurrentPeriodEnd`，被 `periodLen` 兜住；无需回填。
- 免费试用交易（C 期才有）：本期不处理；C 期要求试用期交易不计入 `PaidThrough`（列入 C 期设计必做项）。

`model.go` 的"仅 Stripe 维护"注释改掉。

### 3.3 `applyAppleRefund(ctx, otx, txn, n, source)`（取代 `revokeSubscription`）

`n` = 通知元数据（`signedDate`、解出的 `revocationType/Percentage`）。`withDeadlockRetry` 单事务。锁序 **订阅 → 用户 → 订单 → 邀请人 → 分销配置**；入账路径为 订阅 → 用户 → 邀请人 → 新建订单，插入新行不与已有订单行冲突。已知跨用户环（A 退款锁到邀请人 B，B 同时入账锁到其邀请人 A）由 InnoDB 检测并回滚一方，所有调用方（webhook、verify、对账、补漏）都包 `withDeadlockRetry`。

1. `FOR UPDATE` 锁订阅（apple, otx）；不存在 → 返回。
2. 采纳判定（顺序无关）：读 `AppleRefund{TransactionID}` 为 r。
   - r 不存在 → 采纳；
   - r 存在且 `r.Active` → no-op（重投、REFUND/REVOKE 并发）；
   - r 存在且 `!r.Active`（曾被撤销）→ 仅当 `n.signedDate > r.ReversedSignedAt` 采纳（Apple 撤销后再次退款）；否则 no-op（迟到的旧 REFUND）。
3. 锁用户。
4. `credited := SubscriptionCredit{apple, txn.TransactionId}` 存在。
5. `ord := revokeIAPOrderCashbackInTx(...)` 返回 `{Found, AlreadyRefunded, WalletRefunded, OtherPaidCount}`，`OtherPaidCount` 在短路前计算；其内部的退款后续动作统一走 §3.5 的 `onPaidOrderRefundedInTx`。
6. 付费时长：
   ```
   now       = 处理时刻
   tExp      = txn.ExpiresDate/1000
   periodLen = tExp − txn.PurchaseDate/1000
   paid      = PaidThrough>0 ? PaidThrough : CurrentPeriodEnd
   eligible  = credited && n.revocationType≠FAMILY_REVOKE && txn.ownership≠FAMILY_SHARED
               && !ord.WalletRefunded && tExp>now
   cut       = eligible ? max(0, min(paid−now, user.ExpiredAt−now, periodLen)) : 0
   ```
   - `tExp>now` 门：旧期已消耗 → 0。`periodLen` 上界：只收一期。`paid` 上界：不碰赠送。
   - `ExpiresDate==0`：error 告警，`periodLen := paid−now`。
   - `REFUND_PRORATED` 同 `REFUND_FULL`（Apple 按剩余时间折算）；若 `revocationPercentage` 与 `(tExp−now)/periodLen` 相差 >10 个百分点 → 告警 `[APPLE-REFUND-PCT]`（捕捉将来非时间口径的部分退款，防多收）。
   - 写库定向：`expired_at = expired_at − cut`（`gorm.Expr`）、`paid_through = paid − cut`；**禁止 `Save(&user)`**（会覆盖第 5 步翻回的 `IsFirstOrderDone`）。
7. 历史：`cut>0` 写 `UserProHistory{refund, Days: −ceil(cut/86400)}`。
8. 状态：`eligible` → `status='revoked'`、`MarkedRevoked=true`。**不改 `current_period_end`。**
9. upsert r：`Active=true, RefundSignedAt=n.signedDate, CutSeconds=cut, MarkedRevoked, Credited, Revocation*`…
10. 提交后告警 `[APPLE-REFUND]`：品牌、用户、该用户 Apple 退款累计次数（`AppleRefund` 计数）、cut、邀请撤回、分销计数变化、来源。开途 30 天内 ≥3 次 → 告警标注"考虑提前上线 Consumption 应答"。

### 3.4 入账：已退款交易不入账、退款后的新付款按首购入账

`creditAppleTransaction` 改动（去重查询提前到 upsert 之前）：

- **门**：`info.RevocationDate>0 && !alreadyCredited` → 返回 `errAppleTxnRevoked`，不建行、不改状态。调用点处理：
  - verify 端点 → 用户可读错误"该购买已退款"；
  - webhook（`DID_RENEW/SUBSCRIBED/OFFER_REDEEMED` 经 `verifyAndGrantTransaction`）→ `errors.Is` 视为已处理，200；
  - 对账 `reconcileAppleSubscription` → `errors.Is` 视为非致命，**继续**走后面的 Revoked 分支（否则从未入账的被退交易永远到不了收回逻辑）。
- **复活**：`revival := sub.Status=='revoked' && !alreadyCredited && info.RevocationDate==0 && info.PurchaseDate/1000 > lastActiveRefundAt`，其中 `lastActiveRefundAt = max(RevocationDate) of AppleRefund{otx, Active, MarkedRevoked}`（无则 0）。
  - `revival` 时：**按首购口径入账**——`creditSeconds = newPeriodEnd − PurchaseDate/1000`，`applyGiftCredit`；`kind="purchase"`；状态按非 revoked 推导；`auto_renew` 取 renewal info 真值（缺失时 true）。不发邀请奖励（非 isFirst 行，且有一生一次门）。
  - 被退交易本身、退款前旧交易重放：不满足 `revival`，维持 revoked。
  - 若重订阅生成了**新 otx**：走原 isFirst 绑定路径（appAccountToken 校验），无需特殊处理。
- 沙盒交易：不置 `IsFirstOrderDone`、不改 Tier、不发邀请奖励；权益照发（见 §6）。

终态守卫（防止 revoked 被普通状态事件覆盖，丢失 §3.4 依赖的标记）：
- `applyRenewalInfo` 的 status UPDATE 与两条 grace cover-through UPDATE 加 `AND status<>'revoked'`；
- webhook `EXPIRED/GRACE_PERIOD_EXPIRED` 的 `setSubStatus` 改为 `WHERE id=? AND status<>'revoked'`。

### 3.5 退款后续动作统一 helper：`onPaidOrderRefundedInTx(order)`

Apple 退款（`revokeIAPOrderCashbackInTx`）与后台网页退款（`ProcessOrderRefund`）**都调用**，保证两条路径同一语义（review 第 2 轮 R1）：

1. **邀请奖励**：找 `InviteRewardGrant{TriggerKind, TriggerRef}`（Apple 单 = `apple_txn`/交易号；网页单 = `order`/订单 id），`!Reversed` 时：
   - 先尝试**改锚**：被邀请人若还持有另一笔"合格购买"（同用户、已付、未退款、生产、套餐月数 ≥ `MinRewardMonths` 的订单）→ 把 grant 的 trigger 改指向它，不撤回（R5：条件"保留一笔购买"仍成立）。
   - 否则撤回：按 id 锁邀请人；双方各扣 `min(实发, ExpiredAt−now)`（不低于 now）；各写 `refund` 历史（reason 写明"被邀请人退款，邀请奖励收回"）；`Reversed=true` 并记实扣。
2. **分销计数**：若 `order.RetailerCountedID>0`：
   - 买家还有其它已付未退款订单 → 把计数标记**转移**到其中最早的一笔（该单 `RetailerCountedID` 置为同值），不减；
   - 否则 → `paid_user_count = GREATEST(paid_user_count−1, 0)`；若该分销商当前等级来自 `auto_upgrade` 且计数跌破升级门槛 → 告警 `[RETAILER-COUNT-DROP]`（**不自动降级**，由运营决定；自动降级会影响分销商关系，属业务决策）。
   - 清零本单 `RetailerCountedID`。
   - 写入侧：`processRetailerCashbackInTx` 在 `incrementPaidUserCountInTx` 成功后**立即**写本单 `RetailerCountedID`（在分成比例为 0 的早退之前）。
3. 返现撤回仍由 `refundCashbackInTx`；其使钱包余额 <0 时告警 `[CASHBACK-NEGATIVE]`。

行为变化（写进发布说明）：后台网页退款从此也会撤回邀请奖励、调整分销计数。存量订单 `RetailerCountedID=0` 永不减；存量邀请奖励无 grant 行，不会撤回（一生一次门对它们仍有效，见 §3.7）。

### 3.6 撤销退款：`REFUND_REVERSED`（字面量）→ `reverseAppleRefund`

`withDeadlockRetry`，锁订阅 → 用户。读 r：

- r 不存在（补漏回放时撤销先于退款到达）→ 建 r：`Active=false, ReversedSignedAt=n.signedDate`。之后到达的、`signedDate` 更早的 REFUND 按 §3.3 第 2 步被判为迟到 → no-op。正确。
- r 存在且 `!r.Active` → no-op（重投）；若 `n.signedDate > r.ReversedSignedAt` 只更新时间戳。
- r 存在且 `r.Active` 且 `n.signedDate > r.RefundSignedAt` → 恢复：
  - `restore = min(r.CutSeconds, max(tExp−now, 0))`；`>0` 时 `expired_at = max(expired_at, now) + restore`、`paid_through = max(paid_through, now) + restore`，写正向历史（"Apple 撤销退款，恢复会员"）。
  - 状态：仅当订阅**仍是** `revoked` 且 `r.MarkedRevoked` 且 `tExp>now` → 按非 revoked 推导（期间若已重订阅复活，则不动状态，两次付款的时长都保留——两次都付了钱）。
  - 用户 `is_first_order_done = true`。
  - `r.Active=false, ReversedSignedAt=n.signedDate`。
  - **不自动反转**：订单退款标记、返现、分销计数、邀请奖励。告警 `[APPLE-REFUND-REVERSED]` 列出这四项的具体手工步骤（邀请奖励用 `add_user_membership` 补回双方）。理由：极少发生，自动反转牵涉钱包余额与分销等级，出错代价高于人工。

`REFUND_DECLINED`、`CONSUMPTION_REQUEST` → info 日志。

### 3.7 邀请奖励：一生一次、沙盒不发

`grantInvitePurchaseRewardInTx(ctx, tx, userID, plan, trigger{Kind, Ref, Production})`：
- `!Production` → 不发。
- 一生一次：被邀请人已有 `InviteRewardGrant`，或存在 `UserProHistory{user_id=被邀请人, type=invited_reward}`（存量判据，走 `(user_id, …)` 前缀索引）→ 不发。判据不依赖天数配置。
- 发放后同事务写 grant（实发秒数）。
- 网页路径（`MarkOrderAsPaid` → `handleInvitePurchaseRewardInTx`）传 `trigger{order, order.ID, true}`；Apple 首购传 `trigger{apple_txn, txnID, env==Production}`。

### 3.8 漏通知补漏

- 抽出 `processAppleNotification(ctx, signedPayload, source)`：JWS 校验 → 解析 → 查订阅 → LastEventID 去重 → 分派。webhook 与补漏共用。
- **Notification History 客户端**（qtoolkit 无，自写，放 `api/apple_notification_history.go`）：`POST https://api.storekit.itunes.apple.com/inApps/v1/notifications/history[?paginationToken=]`，Bearer = `appstore.GenerateJwtToken(bundleId)`，body `{startDate, endDate, onlyFailures:true}`（毫秒）；响应 `{notificationHistory:[{signedPayload}], hasMore, paginationToken}`。仅生产环境。
- 每日 asynq 定时任务：对每个配置了 bundleId 的品牌，窗口最近 **14 天**，拉全部页（单次上限 1000 条，超出告警），**按 payload `signedDate` 升序**逐条 `processAppleNotification(…, "sweep")`；单条失败记录并继续，整体失败告警。幂等由 LastEventID + `AppleRefund` 采纳规则保证。
- 对账：Apple 状态 Revoked 且本地非 revoked → `applyAppleRefund(…, "reconcile")`，`signedDate` 取 `st.txn.SignedDate`，`revocationDate` 缺失时取 now。对账内 `creditAppleTransaction` 包进 `withDeadlockRetry`。

### 3.9 开途独有的钱与分销修复

- **IAP 订单不再走后台钱包退款**（业务决策，明确写出）：`ProcessOrderRefund`、后台预校验、审批执行回调都拒绝 `channel=apple_iap`，消息"App Store 订单请让用户向 Apple 申请退款；需补偿请用会员天数"。已在途的 IAP 退款审批单执行时得到该明确失败而非 500。存量已退到钱包的 IAP 单仍由 §3.3 的 `WalletRefunded` 分支处理（测试保留）。`orderEntitlementSecondsInTx` 的 IAP 分支随之不可达，删除并更新注释。
- **IAP 返现冻结 90 天**：`processRetailerCashbackInTx` 对 `apple_iap` 订单 `freezeDays=90`（首单与续费都是，开途只卖年付）；网页 30 天；不追溯存量。
- `[CASHBACK-NEGATIVE]` 告警见 §3.5。

## 4. 文案与客服

- 开途条款新增 7.5「App Store 购买」——中英两段都加（英文段不出现裸 "Kaitu"）：由 Apple 收费与退款；7.1–7.3 的钱包退款不适用于 App Store 购买；Apple 退款后，该笔购买对应的剩余会员时长收回；Apple 撤销退款后恢复会员时长。另增邀请奖励条款：购买奖励以被邀请人保留一笔合格购买为前提，退款（任何渠道）后双方奖励收回，已用完的从当前会员时长中扣除。
- 分销规则（`retailer-rules.md`）：退款撤回返现、可致余额为负；App Store 订单返现冻结 90 天；付费人数随退款调整，自动升级的等级可能被复核。顺手修英文段既有的裸 "Kaitu"。
- Overleap 条款 7.5 已覆盖（"If Apple refunds a purchase, the access it paid for ends"），不改。
- 客服知识库：两品牌——Apple 退款后会员变化、撤销退款自动恢复；开途——App Store 订单不能退到钱包、补偿用会员天数（`add_user_membership`）、邀请人被收回奖励的解释、`[APPLE-REFUND-REVERSED]` 手工步骤。

## 5. 明确不做（及理由）

- **Send Consumption Information + 用户同意**：要 app 内 opt-in 界面、隐私政策与隐私标签变更；开途 iOS 至今 0 笔退款。独立项目，与 C 期（iOS 试用）同批做两品牌同意界面，再开 12h 应答器（Overleap 回 `GRANT_PRORATED`，开途按 1GB 用量口径 `DECLINE`/`GRANT_PRORATED`）。**提前触发条件**：开途 30 天内 Apple 退款 ≥3 次（§3.3 第 10 步告警会标注）。
- **屡退用户禁用 IAP**：iOS app 内引导外部支付受审核指南 3.1.1 约束（非美区），且已扣款的购买不能拒绝入账；以告警中的累计退款次数 + 未来 Consumption 应答处理。已知残留："用约 80 天→退款→换 Apple ID"在 Consumption 上线前仍可行，每轮约年费的 20–25%，靠 Apple 自己的退款审核约束。
- **撤销退款时自动反转订单 / 返现 / 分销 / 邀请**：见 §3.6。
- **分销商自动降级**：告警代替，业务决策。
- **C4 历史 reason 本地化**：现有入账 reason 都是中文内部口径；Overleap Apple 渠道 C 期才上线，改 reason code + 客户端 i18n 列入 C 期。
- 存量邀请奖励无 grant 行 → 不会被撤回（一生一次门仍拦重复领取）。量 ≤ 现有 IAP 单数，接受。

## 6. 范围外但须告知用户

沙盒交易照发**权益**（为 TestFlight / 沙盒测试设计）。若开途 TestFlight 对外公开，任何人可经沙盒购买白拿会员。线上现仅 2 个沙盒订阅。需用户确认 TestFlight 是否对外，再决定是否改为"沙盒权益只发白名单账号"。

## 7. 测试要点（每条做变异验证；DB 测试两种跑法各一次、0 SKIP）

1. 赠送在购买前 / 后叠加 → 退当期：收回剩余付费段，赠送保留。
2. 入账迟到（付费段后移）→ 退当期：按 `PT−now` 收、不超过 `periodLen`。
3. 退已过期旧期 → cut=0、状态不变、订单/返现照撤。
4. 重投；REFUND+REVOKE 并发（真 DB）→ 只处理一次。
5. 家庭共享 / `FAMILY_REVOKE` / 未入账交易 → cut=0、不改状态。
6. `IsFirstOrderDone` 翻回后不被覆盖；`OtherPaidCount` 在已退款短路下也正确。
7. 被退交易事后入账：verify→"已退款"错误；webhook→200 不入账不建行；对账→不报错且继续执行 Revoked 收回。
8. 退款→**隔 30 天**同链重订阅（账户上有 100 天赠送）：`credited_seconds`≈一期、赠送保留、状态 active、`auto_renew` 真值；被退交易重放不复活；新 otx 走首购绑定。
9. 邀请：首购发奖写 grant；Apple 退款撤回双方；**网页后台退款也撤回**；持有另一笔合格购买时改锚不撤回；重买不再发；仅配置邀请人天数时仍防刷；沙盒不发、不置首单；存量 `invited_reward` 历史阻止重发；奖励已用完时从邀请人当前时长扣、不低于 now。
10. 撤销退款：时长与 `PaidThrough` 按 `max(·, now)+restore` 还原、正向历史、`IsFirstOrderDone=true`；仍 revoked 时状态复活；**期间已重订阅**时不动状态与周期末、两次时长都在；重复投递幂等。
11. 顺序无关：REVERSED 先于 REFUND 到达（补漏回放）→ REFUND 判迟到 no-op；REFUND→REVERSED→再次 REFUND（signedDate 更晚）→ 再收一次。
12. 补漏：History 客户端分页、按 signedDate 排序回放、LastEventID 去重、单条失败不中断；对账发现 Revoked（有 / 无 revocationDate）。
13. 终态守卫：DID_FAIL_TO_RENEW / EXPIRED 与 REFUND 赛跑 → 不复活、不延长、不把 revoked 改成 expired。
14. `PaidThrough`：首购、续订、cover-through、宽限期（不动）、存量 0、复活入账。
15. `ExpiresDate==0` → 告警 + 按 `paid−now`；`revocationPercentage` 偏差告警。
16. 分销：写入点在比例为 0 早退之前；退款减计数 / 有其它付费单时转移标记 / 存量 0 不减；跌破门槛告警；IAP 返现冻结 90 天；负余额告警。
17. `ProcessOrderRefund`、后台预校验、在途审批执行均拒绝 IAP 订单，消息明确。
18. `REFUND_REVERSED` 字面量 payload 经 webhook 端到端。
