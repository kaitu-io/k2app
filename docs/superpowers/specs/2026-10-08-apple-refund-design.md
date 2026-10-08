# Apple 退款收回（开途 + Overleap）设计 — A 期补充（A2）

> 状态：设计 v2（合入 review 第 1 轮：后端 M1–M6/m1–m9、对抗 B1–B10/L1–L3/C1–C5）。随 A 期（`feat/overleap-trial-payment`）一起合并、一起部署。
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

### 3.1 新表

`SubscriptionCredit` 有 4 处读取方按 `credited_seconds` 求和（`orderEntitlementSecondsInTx`、funnel facts ×2、retention），不能写负数行。

```go
// AppleRefund：一笔 Apple 交易的退款处理记录（幂等键 TransactionID）。
type AppleRefund struct {
    ID, CreatedAt, UserID, SubscriptionID
    OriginalTransactionID string // index
    TransactionID         string // uniqueIndex
    RevocationDate        int64  // 秒（Apple 毫秒 /1000）；缺失记处理时刻
    RevocationReason      int32
    RevocationType        string // 自行解 JWS 读出，审计用
    RevocationPercentage  int32  // 毫单位，审计用
    Credited              bool   // 我们是否为这笔交易入过账
    CutSeconds            int64  // 收回的付费时长
    PrevPeriodEnd         int64  // 置 revoked 前的 CurrentPeriodEnd（撤销退款时还原）
    MarkedRevoked         bool
    ReversedAt            int64  // REFUND_REVERSED 处理时刻；0=未撤销
    Source                string // webhook | sweep | reconcile
    Note                  string
}

// InviteRewardGrant：一次"被邀请首购奖励"的发放记录。被邀请人唯一 → 每人一生一次；
// 记下触发它的那笔购买，退款时精确撤回双方。
type InviteRewardGrant struct {
    ID, CreatedAt
    InviteeUserID    uint64 // uniqueIndex
    InviterUserID    uint64
    InviteCodeID     uint64
    TriggerKind      string // apple_txn | order
    TriggerRef       string // Apple transactionId 或 order.ID；index(kind,ref)
    InviteeSeconds   int64  // 实发
    InviterSeconds   int64
    ReversedAt       int64
    InviteeCutSeconds, InviterCutSeconds int64 // 撤回时实扣（受 now 下限约束）
}
```

### 3.2 `PaidThrough` 扩展到 Apple

`creditAppleTransaction` 在真正入账后，**单独一条 `Update`** 写入（`Save(&sub)` 在 credit 计算之前，不能靠它）：

```
base         = max(入账前 user.ExpiredAt（邀请奖励重载之后）, now)
granted      = max(入账后 user.ExpiredAt（含 cover-through）− base, 0)
paid_through = max(sub.PaidThrough, now) + granted
```

与 Stripe 同口径。grace cover-through 不是付费时长、不动 `PaidThrough`；**已知偏差**：宽限期后续订成功时 `granted = 新期末 − graceEnd`，`PaidThrough` 比真实付费段少一个宽限窗（≤16 天），该期被退时少收这么多——接受，写进注释。存量行 `PaidThrough=0`：收回路径回落 `CurrentPeriodEnd`（复用 A 期 helper），并被 `periodLen` 上界兜住，无需回填。`model.go` 中"仅 Stripe 维护"注释改掉。

### 3.3 `applyAppleRefund(ctx, otx, txn, meta, source)`（取代 `revokeSubscription`）

`withDeadlockRetry` 单事务，锁序 **订阅 → 用户 → 订单 → 邀请人**（入账路径为 订阅→用户→邀请人→新建订单，插入新行不与已存在订单行的锁冲突）。已知可能的跨用户环：A 退款（锁 A→邀请人 B）与 B 自己入账（锁 B→其邀请人 A）同时发生 → InnoDB 检测死锁、回滚一方；webhook/verify 路径均在 `withDeadlockRetry` 内，对账与补漏改为同样包一层。接受。

1. `FOR UPDATE` 锁订阅（apple, otx）；不存在 → 返回。
2. 幂等：`AppleRefund{TransactionID}` 已存在 → no-op（REFUND/REVOKE 并发在订阅锁上串行）。
3. 锁用户。
4. `credited := SubscriptionCredit{apple, txn.TransactionId}` 存在。
5. `ord := revokeIAPOrderCashbackInTx(...)`，改为返回 `{Found, AlreadyRefunded, WalletRefunded, OtherPaidCount}`；`OtherPaidCount` 在短路之前就算（M2）。
6. 付费时长：
   ```
   now      = 处理时刻
   tExp     = txn.ExpiresDate/1000
   periodLen= tExp − txn.PurchaseDate/1000
   paid     = PaidThrough>0 ? PaidThrough : CurrentPeriodEnd
   eligible = credited && revocationType≠FAMILY_REVOKE && ownership≠FAMILY_SHARED && !ord.WalletRefunded && tExp>now
   cut      = eligible ? max(0, min(paid−now, user.ExpiredAt−now, periodLen)) : 0
   ```
   - `tExp>now` 门：旧期已消耗 → 0（P2）。用门而非 `min(tExp,…)` 上界：入账晚到时账本付费段整体后移，`tExp` 上界会少收一个"迟到窗"（M4）。
   - `periodLen` 上界：只收这一期，迟到两期也不会多扣。
   - `paid` 上界不碰赠送（P1）。
   - `ExpiresDate==0`（不应发生）：error 告警，`periodLen` 取 `paid−now`。
   - `REFUND_PRORATED` 与 `REFUND_FULL` 同处理：Apple 对自动续订按剩余时间折算，退的正是剩余部分。
   - 写库一律定向：`Update("expired_at", gorm.Expr("expired_at - ?", cut))`、`paid_through = paid − cut`；**不得 `Save(&user)`**（会用旧快照覆盖第 5 步翻回的 `IsFirstOrderDone`，M1）。
7. 邀请奖励撤回：`InviteRewardGrant{TriggerKind: apple_txn, TriggerRef: txn.TransactionId, ReversedAt: 0}` 存在 → 调 `reverseInviteGrantInTx`：按 id 锁邀请人，双方各扣 `min(实发, ExpiredAt−now)`（不扣到 now 以下），写历史、记 `ReversedAt`。精确归因，不依赖订单是否存在、不依赖"还有没有别的付费单"（B7、B8、m2）。
8. 分销计数（P12）：新增列 `Order.RetailerCountedID uint64`（计入的分销商配置 id；0=未计入），由 `processRetailerCashbackInTx` 在 `incrementPaidUserCountInTx` 成功后写入。退款时（Apple 与后台网页退款共用 `refundCashbackInTx` 旁的同一 helper）若 `RetailerCountedID>0` → 该配置 `paid_user_count − 1`（`GREATEST(…,0)` 原子更新）并清零该列；等级不自动降，告警。精确到单、不靠"首单"推断：重买后 `isUserFirstPaidOrderInTx` 判为首单会再 +1 并记在新单上，净值一致。
9. 历史：`cut>0` 写 `UserProHistory{Type: refund, Days: -ceil(cut/86400)}`（避免显示 "-0 天"），Reason 沿用现有中文内部口径（见 §5 C4）。
10. 订阅：若 `eligible`（当前期被退）→ `status=revoked`、`PrevPeriodEnd=CurrentPeriodEnd`、**`current_period_end=now`**、`MarkedRevoked=true`（P7：此后同链重订阅按 delta = 新期末 − now 入整期、叠在赠送之上，不吞赠送、`credited_seconds`≈整期）。
11. 写 `AppleRefund`。提交后告警 `[APPLE-REFUND]`：品牌、用户、该用户历史 Apple 退款次数、cut 天数、邀请撤回、来源。

### 3.4 邀请奖励：每人一次、沙盒不发（P5、P9）

`grantInvitePurchaseRewardInTx(ctx, tx, userID, plan, trigger)`：
- 新增入参 `trigger{Kind, Ref, Production bool}`；**非生产（沙盒）→ 不发**。
- 被邀请人已有 `InviteRewardGrant`，或已有 legacy `invited_reward` / 作为被邀请人的历史 → 不发（判据不依赖天数配置，m3）。
- 发放后写 `InviteRewardGrant`（两条历史行与之同事务）。
- `creditAppleTransaction`：沙盒交易不置 `IsFirstOrderDone`、不改 Tier（权益照发——沙盒测试需要，见 §6 另报）。
- 网页订单路径（`MarkOrderAsPaid`）传 `trigger{order, order.ID, true}`。

### 3.5 入账门：已退款交易永不入账（P3、B10）

`creditAppleTransaction`：去重查询提到 upsert 之前；`info.RevocationDate>0 && !alreadyCredited` → 返回 `errAppleTxnRevoked`，**不建行、不改状态**。verify 端点把它映射为用户可读错误（"该购买已退款"）；webhook / 对账 / 补漏把它当已处理（200，不重试）。已入账的被退交易走原 `alreadyCredited` 分支，只刷新 plan-state。

### 3.6 退款后重订阅复活（P7）

状态推导：`sub.Status==revoked && !alreadyCredited && info.RevocationDate==0 && info.PurchaseDate/1000 > max(AppleRefund.RevocationDate WHERE otx AND MarkedRevoked AND ReversedAt=0)` → 按非 revoked 推导，并按 renewal info 写真实 `auto_renew`。被退交易本身、退款前旧交易重放都不复活。Stripe 的 revoked 终态门不受影响。

`applyRenewalInfo` 的 status 与 grace cover-through 两条 UPDATE 加 `AND status<>'revoked'`（m1：与 REFUND 赛跑时不把 revoked 覆盖成 grace、不再延长权益）。

### 3.7 退款被撤销：自动恢复（P8、L1）

`REFUND_REVERSED`（字面量）→ `reverseAppleRefund`：锁订阅→用户；`AppleRefund{TransactionID, ReversedAt=0}`：
- 还时长 `min(CutSeconds, tExp−now)`（>0 才还），`paid_through += 还的量`；
- 若 `MarkedRevoked` 且 `tExp>now` → `status` 按非 revoked 推导、`current_period_end = PrevPeriodEnd`；
- 邀请撤回 → 按 grant 记录还给双方（`min(实扣, …)`），清 `ReversedAt`；
- 记 `ReversedAt`；订单退款标记与返现**不自动反转**，告警 `[APPLE-REFUND-REVERSED]` 由运营核对。

`REFUND_DECLINED`、`CONSUMPTION_REQUEST` → info 日志。

### 3.8 漏通知补漏（P4、B9）

- 抽出 `processAppleNotification(ctx, signedPayload, source)`：webhook 与补漏共用（含 LastEventID 去重）。
- 新 asynq 定时任务（每日）：对每个配置了 bundleId 的品牌，调 **Get Notification History**（`onlyFailures=true`，窗口最近 3 天，分页），逐条回放。Apple 侧"投递失败"的才回来，量极小。
- 对账：Apple 状态 Revoked 且本地非 revoked → 用 `st.txn` 调 `applyAppleRefund(..., "reconcile")`；无 `revocationDate` 时以 now 计（Apple 状态 5 是权威）。

### 3.9 钱与分销（开途独有功能上的修复）

- **IAP 订单不再走后台钱包退款**（P10）：`ProcessOrderRefund` 与后台预校验拒绝 `channel=apple_iap`，提示"App Store 订单请让用户向 Apple 申请退款"。消除双重退款与可提现补偿。
- **IAP 返现冻结 90 天**（P11）：`processRetailerCashback` 对 `apple_iap` 订单 `freezeDays=90`（覆盖 Apple 退款窗口）；网页订单仍 30 天。
- `refundCashbackInTx` 使钱包余额 < 0 时告警 `[CASHBACK-NEGATIVE]`。

## 4. 文案与客服

- 开途条款新增 7.5「App Store 购买」——中英两段都加（英文段不得出现裸 "Kaitu"，用 "we"/"the Service"）：由 Apple 收费与退款；7.1–7.3 的钱包退款不适用于 App Store 购买；Apple 退款后，该笔购买对应的剩余会员时长收回，因该笔购买发放的邀请奖励（邀请双方）一并收回；Apple 撤销退款后恢复。另增邀请奖励条款一句（以被邀请人保留购买为前提）。
- 分销规则（`retailer-rules.md`）：退款撤回返现；App Store 订单返现冻结 90 天；付费人数随退款扣减。顺手修英文段既有的裸 "Kaitu"。
- Overleap 条款 7.5 已覆盖，不改。
- 客服知识库（两品牌）：Apple 退款后会员变化、退款撤销自动恢复；开途补"App Store 订单不能退到钱包"。

## 5. 明确不做（及理由）

- **Send Consumption Information + 用户同意**：要 app 内 opt-in 界面、隐私政策与隐私标签变更；开途 iOS 上线以来 0 笔退款。建议作为独立项目，与 C 期（iOS 试用）同批做两个品牌的同意界面，再开 12h 应答器（Overleap 回 `GRANT_PRORATED`，开途按 1GB 用量口径 `DECLINE`/`GRANT_PRORATED`）。
- **屡退用户禁用 IAP、引导网页支付**：iOS app 内引导外部支付受审核指南 3.1.1 约束（非美区），且不能拒绝已扣款的购买入账；靠 `[APPLE-REFUND]` 告警里的历史退款次数 + 未来的 Consumption 应答处理。
- **C4 历史 reason 本地化**：现有入账 reason 全是中文内部口径，Overleap Apple 渠道 C 期才上线；改成 reason code + 客户端 i18n 列入 C 期。
- 网页订单后台退款路径维护 Apple `PaidThrough`：§3.9 之后 IAP 单不再走该路径，问题消失。

## 6. 范围外但须告知用户

沙盒交易照发**权益**（为 TestFlight / 沙盒测试设计，见 `creditAppleTransaction` 注释）。若开途 TestFlight 对外公开，任何人可经沙盒购买白拿会员。线上现仅 2 个沙盒订阅。需要用户确认 TestFlight 是否对外，再决定是否改为"沙盒权益只发白名单账号"。

## 7. 测试要点（每条做变异验证，DB 测试 0 SKIP）

1. 赠送在购买前 / 后叠加 → 退当期：收回整期剩余，赠送保留。
2. 入账迟到（账本付费段后移）→ 退当期：按 `PT−now` 收、不超过一期。
3. 退已过期旧期 → cut=0、状态不变、订单/返现照撤。
4. 重投；REFUND+REVOKE 并发（真 DB）→ 只处理一次。
5. 家庭共享 / `FAMILY_REVOKE` / 未入账交易 → cut=0、不改状态。
6. 第 5 步翻回 `IsFirstOrderDone` 后不被覆盖（M1）；`OtherPaidCount` 在已退款短路下也正确（M2）。
7. 被退交易事后入账（verify→错误；webhook/对账→200 不入账、不建行）。
8. 退款→同链重订阅（带 100 天赠送）：赠送保留、`credited_seconds`≈整期、状态 active、`auto_renew` 真实；被退交易重放不复活；新 otx 走首购绑定。
9. 邀请：首购发奖并写 grant；退款精确撤回双方；重买不再发；仅配置邀请人天数时仍防刷；沙盒不发、不置首单；legacy 历史行阻止重发。
10. `REFUND_REVERSED` 字面量 payload 经 webhook → 时长、状态、期末、邀请奖励还原；重复投递幂等；订单/返现不动且告警。
11. 补漏：Notification History 回放走同一处理、LastEventID 去重；对账发现 Revoked（有/无 revocationDate）。
12. DID_FAIL_TO_RENEW 与 REFUND 赛跑：不复活、不延长。
13. `PaidThrough`：首购、续订、cover-through、宽限期（不动）、存量 0。
14. `ExpiresDate==0` → 告警 + 按 `paid−now`。
15. 分销：退款后计数 −1（只在计入过时）；IAP 返现冻结 90 天；退款致负余额告警。
16. `ProcessOrderRefund` 与后台预校验拒绝 IAP 订单。
