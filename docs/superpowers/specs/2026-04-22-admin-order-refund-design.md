# Admin Order Refund Design

**Date**: 2026-04-22
**Author**: David (+ Claude)
**Status**: Design (approved for implementation planning)

## Motivation

现有 `ProcessOrderRefund` (`api/logic_order.go:95-134`) 只翻 `orders.is_paid = false` + 扣分销商返现，**不给用户钱包打款**，也不撤销授权，路由甚至没注册。管理员事实上无法从后台对一笔订单发起"完整退款"。

同时 `is_paid = false` 这种"伪退款"破坏了多处统计的语义：首单判断、活动表现、营收 dashboard 都会把退款订单从"已付"集合里直接抹掉，导致历史报表不稳定、首单奖励可能被错判触发。

本设计定义一个**符合实际支付审计和会计口径**的完整退款流程：
- 订单保留 `is_paid = true` 的历史事实，另起 refund 字段
- 全额退款进用户钱包，通过现有提现通道出账（不原路退回）
- 完全撤销用户授权（扣 `ExpiredAt`、写反向 `UserProHistory`、必要时翻回 `IsFirstOrderDone`）
- 走 `SubmitApproval` 双管理员审批（超管自动执行）
- 全链路留 5 条关联审计记录

## Scope

**In scope**
- Admin 后台发起的订单退款
- 订单状态字段扩展
- 钱包打款 + 授权撤销 + 分销商返现回扣的事务性协调
- Approval 集成 + 审计链
- 营收/活动/首单等统计 callsite 的语义修正

**Out of scope**
- 部分退款（首版只支持全额；字段 `RefundAmount` 预留便于未来无破坏升级）
- 原路退回至支付渠道（wordgate 上游）—— 只进钱包，提现通道出账
- 前端 UI（webapp admin 退款按钮、退款列表、用户订单页的退款展示）—— 本 spec 不涵盖
- 自动化退款（客户自助）—— 仅管理员手动触发

## Key Decisions

| # | Choice | Rationale |
|---|---|---|
| A | 订单保留 `is_paid=true`，新增 `IsRefunded / RefundedAt / RefundAmount / RefundReason` 字段 | 保历史事实，统计不破；refund 过滤显式、查询分两类（accounting vs eligibility） |
| B1 | 只支持全额退款 | 覆盖现有场景（测试/欺诈/售后全退）；`RefundAmount` 字段仍保留，未来升级部分退款不改表 |
| C1 | 完全撤销授权（扣 ExpiredAt + 反向 UserProHistory + 翻回 IsFirstOrderDone） | 与 B1 全额对称；补偿场景走人工，不占程序逻辑 |
| D1 | 退款走 `SubmitApproval`（双管理员审批） | 与 withdraw 架构一致；非超管 admin 受约束；超管自动执行无摩擦 |
| E1 | 钱包 refund 不冻结（`FrozenUntil=NULL`） | 退款已过 approval 门禁，再加 30 天冻结影响提现体验 |
| E2 | 不调 wordgate 上游原路退回 | 简化支付对接，所有退款统一走"进钱包→提现"流程 |
| E3 | 保留现有 `WalletChangeTypeRefund`（分销商返现作废），新增 `WalletChangeTypeOrderRefund`（用户退款进钱包） | 避免语义冲突；两条记录独立存在 |
| F | 财务统计"不过滤 refunded"（按 `paid_at`），"退款"独立按 `refunded_at` 报告 | 符合真实会计口径，历史报表不可变；业务资格判断（首单/活动 matcher）另走"过滤 refunded"路径 |
| G | 退款只撤销订单直接关联的 `VipPurchase`（`reference_id = orderID`），**保留** 邀请奖励（`VipInvitedReward` / `VipInviteReward`） | 邀请奖励以 `inviteCodeID` 关联、牵涉第二个用户；属"邀请关系"奖励非"购买对价"，退款不连带撤销；特殊补偿人工处理 |

---

## § 1. Data Model

### 1.1 `Order` 表（`api/model.go:248`）新增 4 个字段

```go
type Order struct {
    // ... 现有字段不变
    IsPaid       *bool      `gorm:"default:false"`
    PaidAt       *time.Time

    // 新增
    IsRefunded   *bool      `gorm:"default:false;index"`        // NULL/false=未退款，true=已退款
    RefundedAt   *time.Time                                      // 退款完成时间（approve 通过、callback 执行完）
    RefundAmount uint64     `gorm:"not null;default:0"`          // 已退金额（美分）；B1 下 = PayAmount
    RefundReason string     `gorm:"type:varchar(500)"`           // 管理员填写的原因
}
```

**索引**：`is_refunded` 加独立 index（大量"有效订单"查询要 `WHERE is_paid AND NOT is_refunded`）。

**迁移**：走现有 `AutoMigrate`。历史数据 `is_refunded = NULL/false`、`refund_amount = 0` 都是正确默认。

### 1.2 `WalletChange` 新增一个 type 常量（`api/model_wallet.go`）

```go
const (
    WalletChangeTypeIncome      WalletChangeType = "income"        // 现有 — 分销商返现，30天冻结
    WalletChangeTypeWithdraw    WalletChangeType = "withdraw"      // 现有 — 提现扣款
    WalletChangeTypeRefund      WalletChangeType = "refund"        // 现有 — 分销商返现作废（负数，从分销商钱包扣）
    WalletChangeTypeOrderRefund WalletChangeType = "order_refund"  // 新增 — 订单退款进用户钱包（正数）
)
```

**唯一索引兜底**：`WalletChange` 已有 `uniqueIndex:idx_type_order` 在 `(type, order_id)` 上，新 type 自动受保护（同一 order 只能打一次 `order_refund`）。

### 1.3 `VipChangeType` 新增一个常量（`api/model.go:37`）

```go
const (
    VipPurchase      VipChangeType = "purchase"        // 现有
    VipInviteReward  VipChangeType = "invite_reward"
    VipInvitedReward VipChangeType = "invited_reward"
    VipSystemGrant   VipChangeType = "system_grant"
    VipSurveyReward  VipChangeType = "survey_reward"
    VipRefund        VipChangeType = "refund"          // 新增 — 订单退款导致的授权撤销
)
```

---

## § 2. Refund Flow（事务内步骤）

**触发链**：
```
Admin 点退款按钮
  → POST /app/orders/:uuid/refund
  → api_admin_refund_order (handler)
  → 预校验 order + 调 SubmitApproval("order_refund", params, summary)
    → 超管：同步执行 → executeApprovalOrderRefund → ProcessOrderRefund
    → 非超管：写 pending approval → 等另一个 admin approve
                                  → approval 框架调 callback
                                  → executeApprovalOrderRefund → ProcessOrderRefund
```

**`ProcessOrderRefund(ctx, orderID, reason, operatorID uint64) error` 事务内**（`api/logic_order.go:97`，签名扩展）：

```go
tx.Transaction:
  1. 加行锁加载订单
     tx.Clauses(clause.Locking{Strength: "UPDATE"}).Preload("User").First(&order, orderID)
     校验:
       - order.IsPaid == true (否则 "订单未支付，无法退款")
       - order.IsRefunded != true (否则 "订单已退款")
       - order.RefundAmount == 0 (兜底)

  2. 撤销授权
     - 查询该订单直接关联的正向购买记录：
         SELECT COALESCE(SUM(days), 0) FROM user_pro_histories
         WHERE user_id = ? AND type = 'purchase' AND reference_id = ? AND days > 0
       得到 N（订单直接加的天数）
     - 注意：**不撤销邀请奖励**。被邀请人的 VipInvitedReward / 邀请人的 VipInviteReward
       使用 `ReferenceID = inviteCodeID`（而非 orderID，见 `logic_invite.go:131,141`），
       且邀请人是另一个用户。设计上视这些为"邀请关系"奖励而非"购买对价"，
       退款不连带撤销；若有特殊补偿需求，人工处理。
     - user.ExpiredAt -= N * 86400 (秒)
       若 < now(): 保持新值即可，自然过期，不人工设 0
     - tx.Create(&UserProHistory{
         UserID: user.ID,
         Type: VipRefund,
         ReferenceID: orderID,
         Days: -N,
         Reason: fmt.Sprintf("订单退款撤销授权 - 订单 %s，原因：%s", order.UUID, reason),
       })
     - 若 user.IsFirstOrderDone == true：
         count := COUNT(orders WHERE user_id = ? AND is_paid = true
                        AND (is_refunded IS NULL OR is_refunded = false)
                        AND id != order.ID)
         if count == 0: user.IsFirstOrderDone = false
     - tx.Select("ExpiredAt", "IsFirstOrderDone").Updates(&user)

  3. 撤销分销商返现（复用现有）
     refundCashbackInTx(ctx, tx, orderID)  // logic_wallet.go:221
     // 没有 income 记录 → log warning, 不 rollback

  4. 给用户钱包打款（新增）
     wallet := GetOrCreateWallet within tx (用 userID)
     change := WalletChange{
       WalletID:      wallet.ID,
       Type:          WalletChangeTypeOrderRefund,
       Amount:        order.PayAmount,               // 正数
       BalanceBefore: wallet.Balance,
       BalanceAfter:  wallet.Balance + order.PayAmount,
       FrozenUntil:   nil,                           // E1: 不冻结
       OrderID:       &orderID,
       OperatorID:    &operatorID,                   // 审计必填
       Remark:        reason,
     }
     tx.Create(&change)
     // 若 util.DbIsDuplicatedErr → 重复退款 → return ErrorConflict
     tx.Model(&wallet).
        Update("balance", gorm.Expr("balance + ?", order.PayAmount)).
        Update("total_income", gorm.Expr("total_income + ?", order.PayAmount))

  5. 更新订单状态
     order.IsRefunded   = BoolPtr(true)
     order.RefundedAt   = &now
     order.RefundAmount = order.PayAmount
     order.RefundReason = reason
     tx.Select("IsRefunded", "RefundedAt", "RefundAmount", "RefundReason").Updates(&order)

  6. 日志
     log.Infof(ctx, "order refunded: uuid=%s user=%d amount=%d reason=%s operator=%d approval_id=%d",
               order.UUID, order.UserID, order.PayAmount, reason, operatorID, ...)
```

**原子性**：任一步失败整事务 rollback——不会出现"扣了授权但没打款"或反之。

**幂等性**：三道闸门
- 预校验拦住 `is_refunded=true`
- Callback 再次校验（approval 框架要求，防审批期间状态变）
- `wallet_changes.idx_type_order` 唯一索引兜底

---

## § 3. API Surface + Approval + 审计链

### 3.1 路由注册（`api/route.go`）

挂在 `admin` 组（超管 + 任何 admin 可发起，路由本身不做 Role 限制，Approval 框架自然分流）：

```go
admin.POST("/orders/:uuid/refund", api_admin_refund_order)
```

> 注意：对齐 `opsAdmin.GET("/orders/:uuid", ...)` 的 UUID 参数风格；现有 handler 用的是 `:id` + 数字 ID，实施时一并修正。

### 3.2 Handler 改造（`api/api_admin_order_refund.go`）

```go
func api_admin_refund_order(c *gin.Context) {
    orderUUID := c.Param("uuid")

    var req RefundOrderRequest
    if err := c.ShouldBindJSON(&req); err != nil { Error(c, ErrorInvalidArgument, err.Error()); return }

    // 预校验
    var order Order
    if err := db.Get().Preload("User").Where(&Order{UUID: orderUUID}).First(&order).Error; err != nil {
        Error(c, ErrorNotFound, "订单不存在")
        return
    }
    if order.IsPaid == nil || !*order.IsPaid {
        Error(c, ErrorInvalidOperation, "订单未支付，无法退款")
        return
    }
    if order.IsRefunded != nil && *order.IsRefunded {
        Error(c, ErrorConflict, "订单已退款")
        return
    }

    operator := ReqUser(c)
    summary := fmt.Sprintf("退款订单 %s（¥%.2f，用户 %s，原因：%s）",
        order.UUID, float64(order.PayAmount)/100, order.User.Email, req.Reason)

    approvalID, executed, err := SubmitApproval(c, "order_refund", orderRefundApprovalParams{
        OrderID:    order.ID,
        Reason:     req.Reason,
        OperatorID: operator.ID,
    }, summary)
    if err != nil {
        Error(c, ErrorSystemError, err.Error())
        return
    }
    if !executed {
        PendingApproval(c, approvalID)
        return
    }
    SuccessEmpty(c)
}
```

### 3.3 Request Type 补强（`api/type.go:717`）

```go
type RefundOrderRequest struct {
    Reason string `json:"reason" binding:"required,min=2,max=500"` // 必填，长度受控
}
```

### 3.4 Approval Params + Callback（`api/logic_approval_callbacks.go`）

```go
type orderRefundApprovalParams struct {
    OrderID    uint64 `json:"orderId"`
    Reason     string `json:"reason"`
    OperatorID uint64 `json:"operatorId"`
}

func executeApprovalOrderRefund(ctx context.Context, params json.RawMessage) error {
    var p orderRefundApprovalParams
    if err := json.Unmarshal(params, &p); err != nil {
        return fmt.Errorf("unmarshal params: %w", err)
    }

    // 重新校验前置条件（approval 框架要求）
    var order Order
    if err := db.Get().First(&order, p.OrderID).Error; err != nil {
        return fmt.Errorf("order not found: %w", err)
    }
    if order.IsPaid == nil || !*order.IsPaid {
        return fmt.Errorf("订单未支付")
    }
    if order.IsRefunded != nil && *order.IsRefunded {
        return fmt.Errorf("订单已退款")
    }

    return ProcessOrderRefund(ctx, p.OrderID, p.Reason, p.OperatorID)
}
```

### 3.5 注册 callback（`api/worker_integration.go:88` 后加一行）

```go
RegisterApprovalCallback("order_refund", executeApprovalOrderRefund)
```

### 3.6 审计链（5 条关联记录）

| 表 | 关键字段 | 作用 |
|---|---|---|
| `orders` | `IsRefunded / RefundedAt / RefundAmount / RefundReason` | 订单状态 + 原因文本 |
| `admin_approvals` | `Action='order_refund' / Params(JSON含 orderID+reason+operatorID) / RequestorID / ApproverID / ApprovedAt / ExecutedAt / Status` | 谁发起谁批什么时候——approval 框架自动落盘；`WriteAuditLogFromApproval` 已写入审计日志 |
| `user_pro_histories` | `Type='refund' / ReferenceID=orderID / Days=负数 / Reason` | 授权变动账本 |
| `wallet_changes` | `Type='order_refund' / OrderID / OperatorID / BalanceBefore / BalanceAfter / Remark=reason / FrozenUntil=NULL` | 用户钱包打款记录 |
| `wallet_changes` | `Type='refund' / OrderID / Amount=负数` | 分销商返现回扣（`refundCashbackInTx` 现有） |

**追溯查询示例**：

```sql
-- 看一个订单的完整退款过程
SELECT * FROM admin_approvals WHERE action='order_refund' AND params LIKE '%"orderId":123%';
SELECT * FROM orders WHERE id=123;
SELECT * FROM user_pro_histories WHERE reference_id=123 AND type='refund';
SELECT * FROM wallet_changes WHERE order_id=123 ORDER BY created_at;

-- 某 admin 发起过的退款
SELECT * FROM admin_approvals WHERE action='order_refund' AND requestor_id=?;

-- 某 admin 批准过的退款
SELECT * FROM admin_approvals WHERE action='order_refund' AND approver_id=?;
```

**日志**：`ProcessOrderRefund` 入口 + 每一步完成 + 完成都走 `log.Infof(ctx, ...)`，带 order UUID / user ID / amount / operator ID / approval ID。

**通知**：复用 approval 框架 Slack DM（`logic_approval.go` → `qtoolkit/slack.SendDM`）；不新增通道。

---

## § 4. 统计 Callsite 审计（按会计口径修正）

**核心原则**：营收按 `paid_at` 口径、退款按 `refunded_at` 口径，两张独立账各期净额 = 当期营收 − 当期退款。**历史已报税的数字不可事后修改。**

### 4.1 财务/营收统计（按时间期口径，**不过滤** refunded）

| 位置 | 现状 | 处理 |
|---|---|---|
| `api_admin_stats.go:146,160,176,188,200,219` 全局 dashboard | `WHERE is_paid = true ...` | **保持不变**（按 `paid_at` 口径）；另起一组查询按 `refunded_at` 统计退款金额，前端展示"毛营收 / 退款 / 净营收" |
| `logic_campaign.go:229-231,298` 活动表现 | `CASE WHEN is_paid = true THEN ...` | **保持不变**；新增 `refunded_orders` + `refund_amount` 字段到响应 DTO（按 `refunded_at` 落在活动区间/统计区间） |

**新增 admin stats 端点或扩展现有**（`api/api_admin_stats.go`）：
```go
// 按 refunded_at 计算
Refunds_24h       { Count, SumAmount }
Refunds_7d        { Count, SumAmount }
Refunds_30d       { Count, SumAmount }
TopRefundReasons  []{ Reason, Count }           // GROUP BY refund_reason LIMIT 10
RefundRate_30d    = Refunds_30d.Count / PaidOrders_30d.Count
```

**前端 dashboard 结构**：
```
营收（按 paid_at）
  近 30 天: ¥XXX,XXX    ← 历史口径，不可事后改变
退款（按 refunded_at）
  近 30 天: -¥Y,YYY     ← 当期冲销
  其中:
    - N 笔 1m 内付款的
    - N 笔 1-6m 内付款的
    - N 笔 6m+ 付款的
净营收
  近 30 天: ¥ZZZ,ZZZ
```

### 4.2 业务资格/当前有效状态判断（**需过滤** refunded）

| 位置 | 必要性 |
|---|---|
| `logic_campaign.go:74,97` `paid_before / paid_before_active` matcher | 判断用户是否真的付过—— 退款的应失去首付资格，加 `AND (is_refunded IS NULL OR is_refunded = false)` |
| `logic_retailer.go:364` 首单判断 | 退款后分销商返现已回扣，若不过滤 → 下一单会被错判"非首单"漏发奖励；加过滤 |
| `logic_license_key_batch.go:136,310` 批次转化统计 | "兑换授权码 → 真实付费转化" 只计留存；加过滤 |

### 4.3 保持不动（语义无关 refund）

| 位置 | 原因 |
|---|---|
| `api_webhook.go:138,148,194` | 幂等检查，已付就跳过第二次 webhook |
| `logic_order.go:28` | `MarkOrderAsPaid` 入口幂等校验 |
| `logic_order.go:106` | `ProcessOrderRefund` 入口校验（只有已付订单可退）—— 对 |
| `worker_abandoned_order.go:121,125` | 弃单统计按 "是否曾付"，退款不影响 |
| `logic_member.go:81 / logic_retailer.go:392` | 付款事务路径校验，refund 不经此路径 |
| `api_admin_order.go:74-76` | 管理员列表 `isPaid` 过滤—— UX 上应包含退款过的订单 |

### 4.4 API DTO 扩展

- `type.go:400,423,489`（`OrderInfo` / `OrderListRequest` / `AdminOrderInfo`）新增：
  ```go
  IsRefunded   bool       `json:"isRefunded"`
  RefundedAt   *time.Time `json:"refundedAt,omitempty"`
  RefundAmount uint64     `json:"refundAmount,omitempty"`
  RefundReason string     `json:"refundReason,omitempty"`
  ```
- `api_admin_order.go` list filter 新增 `IsRefunded *bool form:"isRefunded"` 过滤开关
- `api_order.go` 用户侧订单列表也加 `isRefunded` 字段（用户能在自己订单页看到退款状态）
- 无新增 error code，不需同步 `webapp/src/utils/errorCode.ts`

---

## § 5. 错误处理 / 不变量 / 幂等

### 5.1 错误码映射

| 情况 | Error Code |
|---|---|
| Order UUID 不存在 | `ErrorNotFound` |
| `IsPaid != true` | `ErrorInvalidOperation`（"订单未支付，无法退款"） |
| `IsRefunded == true` | `ErrorConflict`（"订单已退款"） |
| `Reason` 为空 / 超长 | `ErrorInvalidArgument`（gin binding 自动） |
| 事务异常 / DB 错误 | `ErrorSystemError` |

### 5.2 并发 / 幂等三道门

1. **Handler 预校验**：提前拦掉大部分重复请求，避免无效 approval 记录
2. **Callback 再次校验**：approval 审批期间订单状态可能被改（极罕见），必须重查
3. **DB 唯一索引**：`wallet_changes.idx_type_order` 兜底，任何情况下同一订单的 `order_refund` 最多一条

### 5.3 事务锁

对 `orders` 加 `FOR UPDATE` 行锁（`clause.Locking{Strength: "UPDATE"}`，对齐 `api_webhook.go:140` 的写法），防两个并发 approve 同时进入。

### 5.4 部分失败

- 撤销授权失败 → 整事务回滚
- 钱包打款失败 → 整事务回滚
- 分销商返现无 income 记录（脏数据 / 没过活动） → 走 warning，**不** 回滚（保留现有 `refundCashbackInTx` 行为）

---

## § 6. 测试策略

遵循 `api/CLAUDE.md` 三层测试规范。

### 6.1 Unit（Mock DB，`SetupMockDB(t)`）

| 测试 | 覆盖 |
|---|---|
| `TestProcessOrderRefund_HappyPath` | 订单 Paid + 用户有 Pro + 有 income 记录 → 验证 5 条记录的 SQL 都正确 |
| `TestProcessOrderRefund_OrderNotPaid` | `IsPaid=false` → 返错，无任何 DB 修改 |
| `TestProcessOrderRefund_AlreadyRefunded` | `IsRefunded=true` → 返 conflict |
| `TestProcessOrderRefund_NoProHistory` | 付费但 UserProHistory 查不到（脏数据） → 订单仍退款、授权不变、记 warning |
| `TestProcessOrderRefund_NoCashbackRecord` | 无分销商 income → 用户仍退款成功 |
| `TestProcessOrderRefund_DuplicateWalletChange` | 唯一索引冲突 → 返错，wallet 不被多扣 |
| `TestProcessOrderRefund_FirstOrderDoneRevoke` | 唯一付费订单退款 → `IsFirstOrderDone` 翻回 false |
| `TestProcessOrderRefund_FirstOrderDoneKeep` | 还有其它有效付费订单 → `IsFirstOrderDone` 保持 true |
| `TestProcessOrderRefund_ExpiredAtUnderflow` | 扣天数后 `ExpiredAt < now` → 保持扣完的值，不人工置 0 |
| `TestProcessOrderRefund_InviteRewardPreserved` | 订单关联有 `VipPurchase` + `VipInvitedReward`（邀请奖励），退款只扣 `VipPurchase` 的天数，邀请奖励保留 |

### 6.2 Handler（Mock DB）

| 测试 | 覆盖 |
|---|---|
| `TestAdmin_RefundOrder_Success_SuperAdmin` | 超管发起 → `executed=true`，返回 approval id |
| `TestAdmin_RefundOrder_Success_RegularAdmin` | 普通 admin 发起 → `executed=false`，approval pending，DB 无变动 |
| `TestAdmin_RefundOrder_InvalidReason` | reason 空/超长 → 400 |
| `TestAdmin_RefundOrder_OrderNotFound` | 404 |
| `TestAdmin_RefundOrder_OrderNotPaid` / `_AlreadyRefunded` | 正确返错 |
| `TestAdmin_RefundOrder_ApprovalReValidation` | 预校验通过但 callback 时订单状态已变 → callback 返错 |

### 6.3 Integration（`skipIfNoConfig(t)`，真实 MySQL）

- **端到端**：创建 user + order → MarkOrderAsPaid → 验证授权/钱包 → ProcessOrderRefund → 验证 5 张表的记录串得起来（SELECT 全部 5 张，assert 一致性）
- **并发 approve**：起两个 goroutine 同时 approve 同一 approval → 只一个成功；验证 wallet 只被打款一次

---

## § 7. 遗留风险 / Migration / Rollout

### 7.1 已知风险

1. **历史"假退款"数据**：生产库现有 `is_paid=false` 的"伪退款"订单（含三笔 test order）。Migration 后这些订单状态是"未付款"而非"已退款"；钱包未打款、授权未撤销、分销商返现未回扣。
   - 处理策略：**B2（统一标今日 refunded_at + 报表备注）**
   - 实施动作：上 prod 后单独出 ticket 跑一次性脚本 `cmd/migrate-legacy-refund.go`，对每个 `is_paid=false` 且对应历史 webhook 曾成功的订单：
     1. 翻 `is_paid=true`（历史事实）
     2. 设 `is_refunded=true / refunded_at=迁移当日 / refund_amount=PayAmount / refund_reason="历史数据迁移"`
     3. 不走 ProcessOrderRefund（不追加钱包/授权撤销，因为业务影响不可考），只做报表层面对齐
   - 前端 dashboard 在迁移日加一个 note："YYYY-MM-DD 历史数据对齐"
2. **WalletChange 唯一索引已生效**：生产环境需确认 `AutoMigrate` 已建 `idx_type_order`（现有代码已有，但部署时验证）
3. **`api_admin_refund_order` 前端调用**：核实 webapp 侧没有既有调用（因为路由未注册，应该没有）
4. **`ProcessOrderRefund` 签名变更**：新增 `operatorID` 参数，扫所有调用点（仅 callback 一处）

### 7.2 Rollout 顺序

1. **DB migration**：`AutoMigrate` 加 Order 4 个字段（不破坏现有数据）
2. **代码 PR**：model + logic + handler + callback + route + stats 修正 + 单元测试，一个 PR 提
3. **部署 staging**：`make deploy-api` 到 staging，跑端到端 smoke（一个完整退款流程，5 张表齐全）
4. **部署 prod**：同上
5. **历史清洗 ticket**（可选，单独）：跑 `migrate-legacy-refund.go`
6. **前端 UI**：另起 PR（不在本 spec 范围）

### 7.3 Open Points

- **前端 UI** 另外设计：admin 退款按钮入口、退款列表页、用户订单页退款状态展示
- **无 undo 功能**：一旦退款成功无法程序化撤销，误操作靠人工 + 客服介入
- **`ProcessOrderRefund` signature**：`operatorID` 用 `uint64` 而非 `*uint64`—— 所有正式调用都必有 operator（system-initiated refund 不在本 spec 范围）

---

## Related Files

- `api/model.go` — Order + VipChangeType
- `api/model_wallet.go` — WalletChangeType
- `api/logic_order.go` — MarkOrderAsPaid + ProcessOrderRefund
- `api/logic_wallet.go` — refundCashbackInTx, GetOrCreateWallet
- `api/logic_member.go` — addProExpiredDays (反向调用参考)
- `api/logic_approval.go` — SubmitApproval
- `api/logic_approval_callbacks.go` — callback 模板（参考 `executeApprovalWithdrawApprove`）
- `api/api_admin_order_refund.go` — 改造现有 handler
- `api/api_admin_stats.go` — 新增退款报表 queries
- `api/worker_integration.go` — 注册 callback
- `api/route.go` — 路由挂载
- `api/type.go` — 请求/响应 DTO
