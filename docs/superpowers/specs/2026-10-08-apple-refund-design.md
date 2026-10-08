# Apple 退款收回（开途 + Overleap）设计 — A 期补充（A2）

> 状态：设计 v4（合入 review 第 1–3 轮；结构性改动见 §3.0）。随 A 期（`feat/overleap-trial-payment`）一起合并、一起部署。
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
2. **退款 / 撤销退款的先后以 Apple 签名时间判定，不以到达顺序。** webhook 实时到达、对账事后发现、Apple 重投，顺序都不可信；用 Apple 签名时间比较（**毫秒**存储，避免同秒并列）。
3. **漏通知靠"向 Apple 问现状"补，不回放历史通知（v4）。** v3 的 Notification History 回放会把旧的状态类通知（EXPIRED、续订状态）重新施加到已变化的订阅上（review 第 3 轮 B1），还牵出去重、证书过期、错误映射一串问题。改为扩大现有对账：对账拿的是 Apple 的**当前真相**，天然幂等、无顺序问题（§3.8）。

### 3.1 新表与新列

`SubscriptionCredit` 有 4 处读取方按 `credited_seconds` 求和，不能写负数行，故新表：

```go
// AppleRefund：一笔 Apple 交易的退款状态（每笔交易一行，TransactionID 唯一）。
type AppleRefund struct {
    ID, CreatedAt, UpdatedAt, UserID, SubscriptionID
    OriginalTransactionID string // index
    TransactionID         string // uniqueIndex
    RefundSignedAt        int64  // 最近一次被采纳的退款证据时刻（毫秒）：通知 signedDate；对账发现时取 revocationDate
    ReversedSignedAt      int64  // 最近一次被采纳的撤销退款证据时刻（毫秒）
    Active                bool   // 当前是否处于"已退款"状态（= RefundSignedAt > ReversedSignedAt）
    RevocationDate        int64  // 毫秒（Apple 原值）；缺失时记处理时刻
    RevocationReason      int32
    RevocationType        string // 自行解 JWS 补读，审计
    RevocationPercentage  int32  // 毫单位，审计
    Credited              bool
    CutSeconds            int64  // 本轮退款收回的付费时长（撤销时据此还）
    MarkedRevoked         bool   // 本轮退款把订阅置为 revoked
    Source                string // webhook | reconcile
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

`n` = 证据元数据：`signedAt`（毫秒；webhook 取通知 payload `signedDate`，对账取 `txn.RevocationDate`，缺失取 now）、解出的 `revocationType/Percentage`（对账路径为空）。`withDeadlockRetry` 单事务。锁序 **订阅 → 用户 → 订单 → 邀请人 → 分销配置**；入账路径为 订阅 → 用户 → 邀请人 → 新建订单，插入新行不与已有订单行冲突。已知跨用户环（A 退款锁到邀请人 B，B 同时入账锁到其邀请人 A）由 InnoDB 检测并回滚一方，所有调用方（webhook、verify、对账）都包 `withDeadlockRetry`。

1. `FOR UPDATE` 锁订阅（apple, otx）；不存在 → 返回。
2. 采纳判定（顺序无关）：读 `AppleRefund{TransactionID}` 为 r。
   - r 不存在 → 采纳；
   - r 存在且 `r.Active` → no-op（重投、REFUND/REVOKE 并发）；
   - r 存在且 `!r.Active`（曾被撤销）→ 仅当 `n.signedAt > r.ReversedSignedAt`（毫秒，严格大于）采纳（Apple 撤销后再次退款）；否则 no-op（迟到的旧 REFUND）。
3. 锁用户。
4. `credited := SubscriptionCredit{apple, txn.TransactionId}` 存在。
5. `ord := revokeIAPOrderCashbackInTx(...)` 返回 `{Found, AlreadyRefunded, WalletRefunded, OtherPaidCount}`，`OtherPaidCount` 在短路前计算；`OtherPaidCount==0` 时无论是否短路都把 `is_first_order_done` 置 false（撤销退款曾置回 true）。**此步不扣任何时长**。
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
6b. 退款后续动作：`onPaidOrderRefundedInTx(userID, keys{order: ord.ID（若有）, apple_txn: txn.TransactionId}, order)`（§3.5）。**在付费时长扣完之后执行**，邀请扣减基于重新读取的 `expired_at`（review 第 3 轮 M1）；无论订单是否已被标退款都执行（各子动作自带幂等：grant 的 `Reversed`、订单的 `RetailerCountedID`）。
7. 历史：`cut>0` 写 `UserProHistory{refund, Days: −ceil(cut/86400)}`。
8. 状态：`eligible && sub.CurrentPeriodEnd <= tExp` → `status='revoked'`、`MarkedRevoked=true`。`CurrentPeriodEnd > tExp` 说明退款处理迟到、期间已有更新的一期入账（续订或重订阅），订阅仍有效，不置 revoked（review 第 3 轮 M2）。**不改 `current_period_end`。**
9. upsert r：`Active=true, RefundSignedAt=n.signedAt, CutSeconds=cut, MarkedRevoked, Credited, Revocation*`…
10. 提交后告警 `[APPLE-REFUND]`：品牌、用户、该用户 Apple 退款累计次数（`AppleRefund` 计数）、cut、邀请撤回、分销计数变化、来源。开途 30 天内 ≥3 次 → 告警标注"考虑提前上线 Consumption 应答"。

### 3.4 入账：已退款交易不入账、退款后的新付款按首购入账

`creditAppleTransaction` 改动（去重查询提前到 upsert 之前）：

- **门**：`info.RevocationDate>0 && !alreadyCredited` → 返回 `errAppleTxnRevoked`，不建行、不改状态。调用点处理：
  - verify 端点 → 用户可读错误"该购买已退款"；
  - webhook（`DID_RENEW/SUBSCRIBED/OFFER_REDEEMED` 经 `verifyAndGrantTransaction`）→ `errors.Is` 视为已处理，200；
  - 对账 `reconcileAppleSubscription` → `errors.Is` 视为非致命，**继续**走后面的 Revoked 分支（否则从未入账的被退交易永远到不了收回逻辑）。
- **复活**：`revival := sub.Status=='revoked' && !alreadyCredited && info.RevocationDate==0 && info.PurchaseDate > lastActiveRefundAt`，其中 `lastActiveRefundAt = max(RevocationDate) of AppleRefund{otx, Active, MarkedRevoked}`（无则 0）。
  - `revival` 时：**按首购口径入账**——`creditSeconds = newPeriodEnd − PurchaseDate/1000`，`applyGiftCredit`；`kind="purchase"`（漏斗会把它算作一次新转化——它确实是一次新付款，写进 funnel 文档）；**`current_period_end = newPeriodEnd`（覆盖，不取 max）**——否则退年付后改订月付，旧的年付期末会让之后每次月续订 delta≤0（review 第 3 轮 N7）；状态按非 revoked 推导；`auto_renew=true`（函数内无 renewal info，后续 `DID_CHANGE_RENEWAL_STATUS` / 对账纠正）。不发邀请奖励。
  - 比较单位：`info.PurchaseDate`（毫秒）与 `AppleRefund.RevocationDate`（毫秒）直接比，严格大于。
  - 被退交易本身、退款前旧交易重放：不满足 `revival`，维持 revoked。
  - 若重订阅生成了**新 otx**：走原 isFirst 绑定路径（appAccountToken 校验），无需特殊处理。
- 沙盒交易：不置 `IsFirstOrderDone`、不改 Tier、不发邀请奖励；权益照发（见 §6）。

终态守卫（防止 revoked 被普通状态事件覆盖，丢失 §3.4 依赖的标记）：
- `applyRenewalInfo` 的 status UPDATE 与两条 grace cover-through UPDATE 加 `AND status<>'revoked'`；
- webhook `EXPIRED/GRACE_PERIOD_EXPIRED` 的 `setSubStatus` 改为 `WHERE id=? AND status<>'revoked'`。

### 3.5 退款后续动作统一 helper：`onPaidOrderRefundedInTx(userID, keys, order)`

Apple 退款（§3.3 第 6b 步）与后台网页退款（`ProcessOrderRefund`，在其权益扣减写库**之后**调用）共用，保证同一语义（review 第 2 轮 R1）。`keys` = 这笔购买的全部锚点：网页单 `{order: id}`；Apple 单 `{order: id（订单建成时）, apple_txn: 交易号}`。

1. **邀请奖励**：查 `InviteRewardGrant` 中 `(kind, ref) ∈ keys` 且 `!Reversed` 的行（任一锚点命中即可——review 第 3 轮 N1：改锚到 IAP 单后、或 Apple 建单失败时，都还能找到）：
   - 后台退款请求带 `keepInviteRewards=true`（默认 false，进审批参数留痕；用于服务故障类善意退款）→ 跳过本项。
   - **改锚**：被邀请人若还持有另一笔"合格购买"——同用户、`id != 当前单`、已付、未退款、`PayAmount > 0`、套餐（`order.GetPlan()`）月数 ≥ `MinRewardMonths`——取最早一笔；该单为 IAP 单时锚点写 `(apple_txn, 其交易号)`，否则 `(order, id)`；不撤回。
   - 否则撤回：按 id 锁邀请人；重新读取双方 `expired_at`；各扣 `min(实发, expired_at − now)`，SQL 为 `expired_at = GREATEST(expired_at − ?, ?now)`；各写 `refund` 历史（reason：被邀请人退款，邀请奖励收回）；`Reversed=true` 并记实扣。
2. **分销计数**：若 `order` 非空且 `order.RetailerCountedID>0`：
   - 买家还有其它已付未退款订单 → 把计数标记**转移**到其中最早的一笔（写同值），不减（被转到的单可能是短期套餐，买家仍在付费，可接受）；
   - 否则 → `paid_user_count = GREATEST(paid_user_count−1, 0)`；若该分销商当前等级来自 `auto_upgrade` 且计数跌破升级门槛 → 告警 `[RETAILER-COUNT-DROP]`（**不自动降级**，业务决策）。
   - 清零本单 `RetailerCountedID`。
   - 写入侧：`processRetailerCashbackInTx` 在 `incrementPaidUserCountInTx` 成功后**立即**写本单 `RetailerCountedID`（在分成比例为 0 的早退之前）。
3. 返现撤回仍由 `refundCashbackInTx`（不在 helper 内）；其使钱包余额 <0 时告警 `[CASHBACK-NEGATIVE]`。

行为变化（写进发布说明）：后台网页退款从此也会撤回邀请奖励（可用 `keepInviteRewards` 豁免）、调整分销计数。存量订单 `RetailerCountedID=0` 永不减；存量邀请奖励无 grant 行，不会撤回（一生一次门对它们仍有效，见 §3.7）。

### 3.6 撤销退款：`REFUND_REVERSED`（字面量）→ `reverseAppleRefund(ctx, otx, txn, n)`

`withDeadlockRetry`，锁订阅 → 用户 → 邀请人。读 r：

- r 不存在（撤销的证据先于退款到达）→ 建 r：`Active=false, ReversedSignedAt=n.signedAt`。之后到达的、证据时刻更早的 REFUND 按 §3.3 第 2 步判为迟到 → no-op。
- r 存在且 `!r.Active` → no-op（若 `n.signedAt` 更大只更新时间戳）。
- r 存在且 `r.Active` 且 `n.signedAt > r.RefundSignedAt` → 恢复：
  - `restore = min(r.CutSeconds, max(tExp−now, 0))`；`>0` 时 `expired_at = max(expired_at, now) + restore`、`paid_through = max(paid_through, now) + restore`，写正向历史（"Apple 撤销退款，恢复会员"）。
  - 状态：仅当订阅**仍是** `revoked` 且 `r.MarkedRevoked` 且 `tExp>now` → 按非 revoked 推导（期间若已复活，不动状态，两笔付款的时长都保留）。
  - **邀请奖励自动恢复**：本笔购买锚点下 `Reversed=true` 的 grant → 双方各加回实扣（`max(expired_at, now)+x`）、正向历史、`Reversed=false`、清实扣。这样之后若 Apple 再次退款，§3.5 能再次撤回（review 第 3 轮 N4：若交给运营手工补，`Reversed` 仍为 true，再退款时永远收不回）。
  - 用户 `is_first_order_done = true`。
  - `r.Active=false, ReversedSignedAt=n.signedAt`。
  - **不自动反转**：订单退款标记、返现、分销计数。告警 `[APPLE-REFUND-REVERSED]` 列出手工步骤。已接受的后果：订单仍为已退款，买家下一笔会被 `isUserFirstPaidOrderInTx` 当首单（首单返现比例）；若运营手工恢复了返现而 Apple 再次退款，第二次退款不会再撤返现（订单已标退款）——告警文案提示运营同步处理。理由：极少发生，自动反转牵涉钱包余额与分销等级，出错代价高于人工。

`REFUND_DECLINED`、`CONSUMPTION_REQUEST` → info 日志。

### 3.7 邀请奖励：一生一次、沙盒不发

`grantInvitePurchaseRewardInTx(ctx, tx, userID, plan, trigger{Kind, Ref, Production})`：
- `!Production` → 不发。
- 一生一次：被邀请人已有 `InviteRewardGrant`，或存在 `UserProHistory{user_id=被邀请人, type=invited_reward}`（存量判据，走 `(user_id, …)` 前缀索引）→ 不发。判据不依赖天数配置。
- 发放后同事务写 grant（实发秒数）。
- 网页路径（`MarkOrderAsPaid` → `handleInvitePurchaseRewardInTx`）传 `trigger{order, order.ID, true}`；Apple 首购传 `trigger{apple_txn, txnID, env==Production}`。

### 3.8 漏通知：扩大对账，向 Apple 问现状

不做 Notification History 回放（§3.0 第 3 条）。现有每日对账只扫"周期 48h 内到期"的行，年付漏一条 REFUND 要 11 个月才发现。新增两类 **Apple 专属**扫描，并入现有每日对账任务（Stripe 侧不变）：

1. **有效订阅巡检**：`provider=apple AND status IN (active, grace, billing_retry) AND environment=Production AND id % 7 = 今日序号 % 7` → 每条每周至少查一次 `GetAllSubscriptionStatuses`，走现有 `reconcileAppleSubscription`。其中 Apple 状态为 Revoked → `applyAppleRefund(…, "reconcile")`，证据时刻取 `st.txn.RevocationDate`（缺失取 now）。漏收的退款至多 7 天内收回。
2. **退款撤销巡检**：存在 `AppleRefund{Active, RevocationDate ≥ now−180 天}` 的订阅，同样按 `id % 7` 分批 → `GetTransaction(被退交易)`；若该交易已无 `revocationDate`（Apple 撤销了退款）→ `reverseAppleRefund(…)`，证据时刻取 now。
3. `reconcileAppleSubscription` 内调用 `creditAppleTransaction` 时 `errors.Is(err, errAppleTxnRevoked)` 视为非致命并继续执行 Revoked 分支；对账内的入账与收回都包 `withDeadlockRetry`。

对账基于 Apple 当前真相，重复执行幂等（`SubscriptionCredit` 去重、`AppleRefund` 采纳规则），没有顺序问题。量：开途现 13 条，每日 ≤2 次 Apple 调用；万级规模时每日约 1/7 订阅数，远低于 Apple 限流。

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
11. 顺序无关：REVERSED 先于 REFUND 到达 → REFUND 判迟到 no-op；REFUND→REVERSED→再次 REFUND（证据时刻更晚）→ 再收一次付费时长**和邀请奖励**（撤销时已自动恢复 grant）；同毫秒不采纳。
12. 对账：有效订阅按 `id%7` 分批覆盖；Apple Revoked（有 / 无 revocationDate）→ 收回；被退交易 revocationDate 消失 → 撤销退款；`errAppleTxnRevoked` 不中断对账；Stripe 扫描范围不变。
19. 迟到的退款（期间已续订或复活，`CurrentPeriodEnd > tExp`）→ 收回该期剩余但不置 revoked。
20. 邀请扣减在付费扣减之后、基于重读的 `expired_at`，两者合计不使 `expired_at < now`；`ProcessOrderRefund` 同。
21. 改锚：网页单退款→锚到 IAP 单写 `(apple_txn, 交易号)`→之后 Apple 退款能撤回；`id != 当前单`；`PayAmount=0` 的单不算合格购买；Apple 建单失败时按交易号仍能撤回。
22. `keepInviteRewards=true` 的后台退款不撤邀请奖励，审批参数留痕。
23. 退年付→改订月付（同组同链）→两次月续订，账户有赠送：每次续订都入整月，赠送不被吞（`current_period_end` 被覆盖）。
24. 撤销退款后 `is_first_order_done=true`；再次退款且无其它有效单 → 置回 false。
13. 终态守卫：DID_FAIL_TO_RENEW / EXPIRED 与 REFUND 赛跑 → 不复活、不延长、不把 revoked 改成 expired。
14. `PaidThrough`：首购、续订、cover-through、宽限期（不动）、存量 0、复活入账。
15. `ExpiresDate==0` → 告警 + 按 `paid−now`；`revocationPercentage` 偏差告警。
16. 分销：写入点在比例为 0 早退之前；退款减计数 / 有其它付费单时转移标记 / 存量 0 不减；跌破门槛告警；IAP 返现冻结 90 天；负余额告警。
17. `ProcessOrderRefund`、后台预校验、在途审批执行均拒绝 IAP 订单，消息明确。
18. `REFUND_REVERSED` 字面量 payload 经 webhook 端到端。
