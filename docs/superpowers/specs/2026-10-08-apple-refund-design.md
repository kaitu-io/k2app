# Apple 退款收回（开途 + Overleap）设计 — A 期补充（A2）

> 状态：设计 v7（合入 review 第 1–6 轮；结构性改动见 §3.0）。随 A 期（`feat/overleap-trial-payment`）一起合并、一起部署。
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
    ReversedSignedAt      int64  // 最近一次被采纳的撤销退款证据时刻（毫秒）——只用于判先后
    RestoredAt            int64  // 撤销退款实际恢复的处理时刻（秒）——只用于再退款封顶
    ConflictAlertedAt     int64  // [APPLE-REFUND-CONFLICT] 已告警时刻；非 0 不再重复告警
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
   - r 存在且 `r.Active` → 不重复收回，但若 `n.signedAt` 是 **Apple 给出的时刻**（通知 `signedDate` 或真实 `revocationDate`，非 now 兜底）则 `RefundSignedAt = max(RefundSignedAt, n.signedAt)`（记下最新的退款证据，供 §3.8 探测冷却与撤销时间戳使用，review 第 5 轮 F4）。now 兜底只用于新建行——否则每周对账会把冷却期不断推后，探测永不执行（review 第 6 轮 B2）；
   - r 存在且 `!r.Active`（曾被撤销）→ 仅当 `n.signedAt > r.ReversedSignedAt`（毫秒，严格大于）采纳（Apple 撤销后再次退款）；否则 no-op（迟到的旧 REFUND）。
3. 锁用户。
4. `credited := SubscriptionCredit{apple, txn.TransactionId}` 存在。
5. `ord := revokeIAPOrderCashbackInTx(...)` 返回 `{Found, AlreadyRefunded, WalletRefunded, OtherPaidCount}`，`OtherPaidCount` 在短路前计算；`OtherPaidCount==0` 时无论是否短路都把 `is_first_order_done` 置 false（撤销退款曾置回 true）。**此步不扣任何时长**。
6. 付费时长：
   ```
   now       = 处理时刻
   tExp      = txn.ExpiresDate/1000   // ==0（不应发生）：error 告警，tExp := sub.CurrentPeriodEnd、newer := false、periodLen := paid−now
   periodLen = tExp − txn.PurchaseDate/1000
   paid      = PaidThrough>0 ? PaidThrough : CurrentPeriodEnd
   eligible  = credited && n.revocationType≠FAMILY_REVOKE && txn.ownership≠FAMILY_SHARED
               && !ord.WalletRefunded && tExp>now
   newer     = sub.CurrentPeriodEnd > tExp      // 更新的一期已入账（Apple 最多提前 24h 扣续费；迟到处理；复活覆盖过 CPE）。只定义一次，第 8 步复用
   cut       = !eligible ? 0
             : newer     ? max(0, min(tExp−now, user.ExpiredAt−now))
             :             max(0, min(paid−now, user.ExpiredAt−now, periodLen))
   ```
   - `newer` 分支（review 第 4 轮 V1）：账本里被退期之后还有新付的一期，`paid−now` 包含新一期，不能用；只收被退期在日历上剩下的 `tExp−now`（此分支 `periodLen` 冗余：`tExp−now ≤ periodLen` 恒成立，注释写明以免被"修"）。`paid_through = paid − cut` 在此分支同样正确（存量 `PT=0` 行写成 `CPE−cut`，之后即为有效值）。被退期本身是最新一期时，才用付费计数口径。
   - **再次退款**（r 曾被撤销后又被采纳，**且 `r.CutSeconds>0 && r.RestoredAt>0`**——上一轮确实收过、也确实还过；否则按上面的首次退款公式算，含 `tExp` 门与两个分支。撤销先到建的占位行、或首次退款时尚未入账而 cut=0 的行都属后者，review 第 6 轮最终项）：不走上面的 `eligible` 时间门与两个分支（撤销时还回的时长可能落在 `tExp` 之后，按 `tExp>now` 门会一天都收不回，review 第 6 轮 G2），改为 `cut = min(cap, user.ExpiredAt−now)`，其余条件（credited、非家庭共享、非钱包退款）照旧；`cap` 为 `max(0, r.CutSeconds − (now − r.RestoredAt))`（用实际恢复时刻，不用证据时刻——探测发现的撤销其证据时刻是退款时刻，会多算已用天数，review 第 6 轮 B1）；`paid_through = max(paid_through, now) − cut` 不低于 now。——最多收回撤销时还回去、尚未用掉的部分；不让期间复活的新一期被多扣（review 第 5 轮 m2）。
   - `tExp>now` 门：旧期已消耗 → 0。`periodLen` 上界：只收一期。`paid` 上界：不碰赠送。
   - `REFUND_PRORATED` 同 `REFUND_FULL`（Apple 按剩余时间折算）；若 `revocationPercentage` 与 `(tExp−now)/periodLen` 相差 >10 个百分点 → 告警 `[APPLE-REFUND-PCT]`（捕捉将来非时间口径的部分退款，防多收）。
   - 写库定向：`expired_at = expired_at − cut`（`gorm.Expr`）、`paid_through = paid − cut`；**禁止 `Save(&user)`**（会覆盖第 5 步翻回的 `IsFirstOrderDone`）。
6b. 退款后续动作：`onPaidOrderRefundedInTx(userID, keys{order: ord.ID（若有）, apple_txn: txn.TransactionId}, order)`（§3.5）。**在付费时长扣完之后执行**，邀请扣减基于重新读取的 `expired_at`（review 第 3 轮 M1）；无论订单是否已被标退款都执行（各子动作自带幂等：grant 的 `Reversed`、订单的 `RetailerCountedID`）。
7. 历史：`cut>0` 写 `UserProHistory{refund, Days: −ceil(cut/86400)}`。
8. 状态：`eligible && !newer`（再次退款时：其余条件成立且 `!newer`）→ `status='revoked'`、`MarkedRevoked=true`。`CurrentPeriodEnd > tExp` 说明退款处理迟到、期间已有更新的一期入账（续订或重订阅），订阅仍有效，不置 revoked（review 第 3 轮 M2）。**不改 `current_period_end`。**
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

离开 revoked 的唯一入口：新 helper `deriveActiveOrExpired(periodEnd, now)`（`periodEnd>now` → active，否则 expired）。现有 `deriveVerifiedStatus` 对 revoked 恒返回 revoked（防重放复活，保留），**复活（本节）与撤销退款（§3.6）都必须调新 helper**，不得传 `sub.Status` 给旧函数——否则静默不生效（review 第 4 轮 M2）。

终态守卫（防止 revoked 被普通状态事件覆盖，丢失 §3.4 依赖的标记）：
- `applyRenewalInfo` 的 status UPDATE 与两条 grace cover-through UPDATE 加 `AND status<>'revoked'`；
- webhook `EXPIRED/GRACE_PERIOD_EXPIRED` 的 `setSubStatus` 改为 `WHERE id=? AND status<>'revoked'`。

### 3.5 退款后续动作统一 helper：`onPaidOrderRefundedInTx(userID, keys, order)`

Apple 退款（§3.3 第 6b 步）与后台网页退款（`ProcessOrderRefund`，在其权益扣减写库**之后**调用）共用，保证同一语义（review 第 2 轮 R1）。`keys` = 这笔购买的全部锚点：网页单 `{order: id}`；Apple 单 `{order: id（订单建成时）, apple_txn: 交易号}`。

1. **邀请奖励**：查 `InviteRewardGrant` 中 `(kind, ref) ∈ keys` 且 `!Reversed` 的行（任一锚点命中即可——review 第 3 轮 N1：改锚到 IAP 单后、或 Apple 建单失败时，都还能找到）：
   - 后台退款请求带 `keepInviteRewards=true`（默认 false；作为字段写进 `order_refund` 审批单的 params JSON，执行回调从 params 读出传给 `ProcessOrderRefund`，审批记录即留痕；用于服务故障类善意退款）→ 跳过本项。
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
  - `restore = r.CutSeconds`（全额还；Apple 撤销退款即重新收了这笔钱，退款到撤销之间的空窗不让用户承担——对账发现时可能晚 7 天，按 `tExp−now` 封顶会让用户永久损失这段，review 第 4 轮 V4）；`>0` 时 `expired_at = max(expired_at, now) + restore`、`paid_through = max(paid_through, now) + restore`，写正向历史（"Apple 撤销退款，恢复会员"）。
  - 状态：仅当订阅**仍是** `revoked` 且 `r.MarkedRevoked` → `deriveActiveOrExpired(current_period_end, now)`（期间若已复活，不动状态，两笔付款的时长都保留）。
  - **邀请奖励自动恢复**：本笔购买锚点下 `Reversed=true` 的 grant → 双方各加回实扣（`max(expired_at, now)+x`）、正向历史、`Reversed=false`、清实扣。这样之后若 Apple 再次退款，§3.5 能再次撤回（review 第 3 轮 N4：若交给运营手工补，`Reversed` 仍为 true，再退款时永远收不回）。
  - 用户 `is_first_order_done = true`。
  - `r.Active=false, ReversedSignedAt=n.signedAt, RestoredAt=now`。
  - **不自动反转**：订单退款标记、返现、分销计数。告警 `[APPLE-REFUND-REVERSED]` 列出手工步骤。已接受的后果：订单仍为已退款，买家下一笔会被 `isUserFirstPaidOrderInTx` 当首单（首单返现比例）；若运营手工恢复了返现而 Apple 再次退款，第二次退款不会再撤返现（订单已标退款）——告警文案提示运营同步处理。理由：极少发生，自动反转牵涉钱包余额与分销等级，出错代价高于人工。

`REFUND_DECLINED`、`CONSUMPTION_REQUEST` → info 日志。

### 3.7 邀请奖励：一生一次、沙盒不发

`grantInvitePurchaseRewardInTx(ctx, tx, userID, plan, trigger{Kind, Ref, Production})`：
- `!Production` → 不发。
- 一生一次：被邀请人已有 `InviteRewardGrant`，或存在 `UserProHistory{user_id=被邀请人, type=invited_reward}`（存量判据，走 `(user_id, …)` 前缀索引）→ 不发。判据不依赖天数配置。
- 发放后同事务写 grant（实发秒数）。
- 网页路径（`MarkOrderAsPaid` → `handleInvitePurchaseRewardInTx`）传 `trigger{order, order.ID, true}`；Apple 首购传 `trigger{apple_txn, txnID, env==Production}`。

### 3.8 漏通知：扩大对账，向 Apple 问现状

不做 Notification History 回放（§3.0 第 3 条）。现有每日对账只扫"周期 48h 内到期"的活跃行。新增 **Apple 专属、仅生产环境**的扫描，并入现有每日对账任务（Stripe 侧不变）。分批：`bucket = id % 7 == (UTC 纪元天数 % 7)`；与现有 48h 临期扫描取并集按 id 去重，同一轮每条至多对账一次。

1. **订阅巡检**（每条每周至少一次）：`provider=apple AND environment=Production AND bucket AND (status IN (active, grace, billing_retry) OR (status='expired' AND current_period_end > now−120 天) OR 存在 Active AppleRefund（RevocationDate ≤180 天，任意状态）)` → `reconcileAppleSubscription`。
   - 覆盖 expired：订阅本地已过期后才被退款（cut=0，但订单/返现/邀请/分销照撤）。
   - 覆盖 revoked：漏收"退款后同链重订阅"的通知时，经最新交易进入 §3.4 复活（review 第 4 轮 V2）。
2. **`reconcileAppleSubscription` 内的顺序（review 第 4 轮 M4）**：拉到状态后**先判 Revoked**：
   - `st.status == Revoked` 且 `st.txn != nil` → 只调 `applyAppleRefund(…, "reconcile")`（`st.txn == nil` → error 日志、跳过）（证据时刻取 `st.txn.RevocationDate`，缺失取 now），**跳过** `applyRenewalInfo` 与 Expired 分支（避免对被退交易做宽限延长）；
   - `st.status == Revoked` 但本地 `AppleRefund` 为非 Active、且本次证据被第 2 步判为迟到 → 告警 `[APPLE-REFUND-CONFLICT]`（我们认为已撤销、Apple 说仍退款；可能是探测读到了旧数据；每条记录只告警一次，记 `ConflictAlertedAt`），**不自动处理**，人工核对（review 第 6 轮 G3）。
   - 否则按现有顺序：`creditAppleTransaction(st.txn)`（`errAppleTxnRevoked` 视为非致命）→ `applyRenewalInfo` → Expired 分支。
   - 入口"revoked 行直接跳过"的守卫改为：revoked 行仍查询，但**只**允许走 `creditAppleTransaction`（复活入账）与撤销退款探测；**跳过 `applyRenewalInfo` 与 Expired 分支**（否则 Apple 报 Expired 时会把 revoked 改成 expired，丢失复活判据，review 第 5 轮 M1）。
3. **撤销退款探测**：对第 1 步覆盖到的、带 Active `AppleRefund` 且 `RefundSignedAt < (now−48h)×1000`（毫秒，以**最新**的 Apple 给出的退款证据计）的行，`GetTransaction(r.TransactionID)`；仅当 HTTP 成功、解出的 `TransactionId == r.TransactionID`、`Environment == Production`（qtoolkit 生产失败会回落沙盒，review 第 5 轮 F1）、且 `RevocationDate == 0` 时，才视为 Apple 撤销了退款 → `reverseAppleRefund`，**证据时刻 = `r.RefundSignedAt + 1ms`**（不用 now：用 now 会让之后任何早于扫描时刻的真实再退款被判为迟到而永久忽略，review 第 5 轮 F4）。任何错误一律跳过（不恢复）。48h 冷却防 Apple 读写不一致造成误恢复。
4. 对账内的入账与收回都包 `withDeadlockRetry`。

对账基于 Apple 当前真相，重复执行幂等（`SubscriptionCredit` 去重、`AppleRefund` 采纳规则），没有顺序问题。量：开途现 13 条，每日 ≤ 3 次 Apple 调用；万级规模时每日约 1/7 订阅数，远低于 Apple 限流。

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
- **漏收的旧期退款**（多期商品：被退的是已被新一期取代的交易，Apple 最新交易未被退）：对账看不到 Revoked，该期的订单/返现/邀请/分销不会被撤回。开途只卖年付、Apple 退款窗口远短于一年 → 实际不发生；Overleap 无返现/分销/邀请。接受，C 期上月付时复核。
- 退款超过 180 天后漏收的同链重订阅：对账不再扫这条 revoked 行，只能靠客户端 verify / 恢复购买自愈（app 启动时 StoreKit 监听会触发）。接受。
- 改锚：只要被邀请人仍持有任一合格购买（不论其会员是否已过期），奖励即保留——与条款"保留一笔合格购买"一致，有意为之。`is_first_order_done` 可能为 false 而 grant 仍有锚（锚点要求更严：`PayAmount>0`、月数门槛），两者口径不同是预期。
- 存量邀请奖励无 grant 行 → 不会被撤回（一生一次门仍拦重复领取）。量 ≤ 现有 IAP 单数，接受。

## 6. 范围外但须告知用户

沙盒交易照发**权益**（为 TestFlight / 沙盒测试设计）。若开途 TestFlight 对外公开，任何人可经沙盒购买白拿会员。线上现仅 2 个沙盒订阅。需用户确认 TestFlight 是否对外，再决定是否改为"沙盒权益只发白名单账号"。

## 7. 测试要点（每条做变异验证；DB 测试两种跑法各一次、0 SKIP）

**收回算术**
1. 赠送在购买前 / 后叠加 → 退当期：收回剩余付费段，赠送保留。
2. 入账迟到（付费段后移）→ 退当期：按 `PT−now` 收、不超过 `periodLen`。
3. 退已过期旧期 → cut=0、状态不变、订单/返现照撤。
4. **新一期已入账后退上一期**（提前 24h 扣续费 / 迟到处理，`CPE>tExp`）→ cut≈`tExp−now`、新一期时长完整、不置 revoked。
5. 家庭共享 / `FAMILY_REVOKE` / 未入账交易 → cut=0、不改状态。
6. `ExpiresDate==0` → 告警，以 `CurrentPeriodEnd` 作门、按 `paid−now` 收；`revocationPercentage` 偏差告警。
7. 存量 `PaidThrough=0` 行 → 由 `periodLen` 兜住，不扣赠送。

**幂等与顺序**
8. 重投；REFUND+REVOKE 并发（真 DB）→ 只处理一次。
9. REVERSED 先于 REFUND 到达 → REFUND 判迟到 no-op；REFUND→REVERSED→再次 REFUND（证据更晚）→ 再收付费时长**和邀请奖励**；同毫秒不采纳。
10. 终态守卫：DID_FAIL_TO_RENEW / EXPIRED 与 REFUND 赛跑 → 不复活、不延长、revoked 不被改成 expired / grace。

**入账门与复活**
11. 被退交易事后入账：verify→"已退款"错误；webhook→200 不入账不建行；对账→不报错并继续。
12. 退款→**隔 30 天**同链重订阅（有 100 天赠送）：`credited_seconds`≈一期、赠送保留、状态**确实**变为 active（经 `deriveActiveOrExpired`）、`auto_renew=true`；被退交易重放不复活；新 otx 走首购绑定。
13. 退款后 Apple 立即自动续订（PurchaseDate=旧 tExp）→ 复活并入整期。
14. 退年付→改订月付（同组同链）→两次月续订，有赠送：每次都入整月，赠送不被吞。
15. `PaidThrough`：首购、续订、cover-through、宽限期（不动）、复活入账、撤销退款后的数值。

**撤销退款**
16. 时长与 `PaidThrough` 按 `max(·, now)+CutSeconds` 全额还原、正向历史、`is_first_order_done=true`；仍 revoked 时状态**确实**复活；期间已复活时不动状态、两笔时长都在；邀请 grant 自动恢复；重复投递幂等。
17. 再次退款且无其它有效单 → `is_first_order_done` 回 false；再次退款的收回量不超过撤销时还回去且未用掉的部分（退款→复活→撤销→再退款 场景给出数值）。
17b. 撤销退款晚于 `tExp` 到达 → 仍全额还 `CutSeconds`（有意为之）。

**邀请与分销（helper 共用）**
18. 首购发奖写 grant；Apple 退款撤回双方；网页后台退款也撤回；`keepInviteRewards=true` 不撤且审批 params 留痕。
19. 改锚：持有另一合格购买时不撤回；网页单退款→锚到 IAP 单写 `(apple_txn, 交易号)`→之后 Apple 退款能撤回；`id != 当前单`；`PayAmount=0` 不算；Apple 建单失败时按交易号仍能撤回。
20. 一生一次：重买不再发；仅配置邀请人天数时仍防刷；沙盒不发、不置首单；存量 `invited_reward` 历史阻止重发。
21. 扣减顺序：邀请扣减在付费扣减之后、基于重读值，合计不使 `expired_at < now`；奖励已用完时从当前时长扣、不低于 now；`ProcessOrderRefund` 同。
22. 分销：写入点在比例为 0 早退之前；退款减计数 / 有其它付费单时转移 / 存量 0 不减；跌破门槛告警；IAP 返现冻结 90 天；负余额告警。

**对账**
23. 分批 bucket 覆盖 active / 近 120 天 expired / 带 Active 退款的 revoked；与 48h 扫描去重；仅生产环境；Stripe 范围不变。
24. Apple Revoked（有 / 无 revocationDate）→ 收回，且**不**执行 `applyRenewalInfo`（无宽限延长）。
25. 撤销退款探测：冷却期内不探测（以最新退款证据计）；响应交易号不符 / 非生产 / HTTP 错误 / 仍有 revocationDate → 不恢复；满足条件 → 恢复且证据时刻 = `RefundSignedAt+1ms`；**F4 序列**（退款→漏收撤销→再退款（r 仍 Active，只更新 RefundSignedAt）→探测）→ 再退款不被永久忽略；无 revocationDate 的对账反复发现同一 Active 行 → `RefundSignedAt` 不被 now 推后、探测照常执行。
25c. 退款→`tExp` 之后才撤销（全额还）→再退款 → 还回的时长被收回。
25e. 撤销先到（占位行 cut=0）→ 之后更晚的真实退款 → 按首次退款公式收回；首次退款时未入账（cut=0）→撤销→入账→再退款 → 按首次退款公式收回。
25d. 本地已撤销、Apple 仍报 Revoked 且证据迟到 → `[APPLE-REFUND-CONFLICT]` 告警、数据不变。
25b. 探测发现的撤销 → 再退款：封顶按 `RestoredAt` 计（给出数值：第 10 天收 355、第 100 天探测恢复、第 120 天再退 → 收约 335）。
26. revoked 行经对账拿到退款后新付款 → 复活；Apple 报 Expired 时 revoked 行保持 revoked。

**钱包与端到端**
27. `ProcessOrderRefund`、后台预校验、在途审批执行均拒绝 IAP 订单，消息明确；存量已退钱包的 IAP 单再遇 Apple 退款 → 不扣时长、邀请照撤、双退告警。
28. `REFUND_REVERSED` 字面量 payload 经 webhook 端到端。
