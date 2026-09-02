# 2026-08-10 — Apple IAP sandbox transaction created a real Order

Moved verbatim from `api/CLAUDE.md` (Apple IAP section) on 2026-09-02. The invariants this incident produced still live there; this file keeps the narrative.

Code anchors: `creditAppleTransaction`, `alertIfAlreadyWalletRefunded`, `revokeIAPOrderCashbackInTx` (`api/logic_apple_iap.go`); `ProcessOrderRefund`, `orderEntitlementSecondsInTx` (`api/logic_order.go`); `iapOrderFixture` (`api/logic_apple_iap_order_test.go`).

**Sandbox 交易发权益，但绝不建订单**（`creditAppleTransaction`，2026-08-10 事故后加）。`info.Environment == appstore.Environment_Sandbox` 时在建单前早退：权益照发（iOS 沙盒账号做内购端到端测试必须能看到 Pro 生效），但不建 `Order`、不调 `processOrderCashbackInTx`。理由是**订单是财务实体**——`createAppleIAPOrderInTx` 写的 `PayAmount` 取 plan 标价，而这个数直接充当分销返现基数、营收统计口径、以及后台退款往用户钱包打款的金额；沙盒交易用户实付为 0，建单即等于凭空造出一笔可退可分佣的钱。

- **事故经过**：`environment` 从一开始就被写进 `subscriptions.environment`，但**全代码库没有任何一处读它做判断**（存档字段，无门控）。沙盒交易于是建出 `ord-d9sjien7k7qc2u9r30ig`（4900 分），客服在后台点退款，被当时那道"IAP 订单一律拒绝退款"的门挡下才没打款。事发时全库唯一的 IAP 订单就是这一笔，唯一的 subscription 也是 Sandbox——即真实 IAP 付费尚未发生，全部 IAP 生产数据都是沙盒污染。
- **测试 fixture 曾是同谋**：`iapOrderFixture` 一度硬编码 `Environment: "Sandbox"`，整套建单/返现测试跑的都是这条路径，所以没有任何测试能发现沙盒会建单。现已参数化为 `f.env`，**默认 `Environment_Production`**——建单是只在生产交易上发生的行为，用 Sandbox 建 fixture 等于测一条被门提前截断的路径。改 fixture 默认值前先想清楚这条。

**IAP 订单支持后台钱包退款**（`ProcessOrderRefund`，2026-08-10 起）。此前整个拒绝，两条理由中只有一条成立：

- 资金侧那条（"Apple 已原路退款，再打钱包=退两次"）是把 Apple 退款当成了前提。后台退款的实际用法是 **Apple 未退款时由客服主动补偿到钱包**，不冲突。真正会双退的是"后台退款后用户又向 Apple 申请并成功"，由 `alertIfAlreadyWalletRefunded`（`logic_apple_iap.go`）哨兵覆盖：`revokeIAPOrderCashbackInTx` 的 `IsRefunded` 短路点查 `wallet_changes{type=order_refund, order_id}`，命中即 Slack `[DOUBLE-REFUND]` 告警。**只告警不阻断**——Apple 侧已成事实，返错只会招来通知重试风暴。
- 授权侧那条是真 bug，已修：**两条渠道的权益记账形态不同，反算必须分口径**（`orderEntitlementSecondsInTx`，`logic_order.go`）。网页订单是 `UserProHistory{type=purchase, reference_id=order.ID}`；IAP 是 `UserProHistory{type=apple_sub, reference_id=SubscriptionCredit.ID}`——**reference_id 是 credit 行 id，不是订单 id**，按订单 id 查恒得 0，会出现"钱退了权益一秒没扣"。IAP 走 `orders.apple_transaction_id → SubscriptionCredit.CreditedSeconds`（精确秒；`UserProHistory.Days` 是 floor 后的展示值）。`apple_transaction_id` 为空的 IAP 订单**拒绝退款**，不许降级成"查到 0 秒→只打钱不扣权益"。
- **退款 ≠ 退订**：Apple 侧订阅仍然活着，下个计费周期照常扣款并由 `DID_RENEW` 建出**新订单**。停止续费只能由用户在 Apple 设置里取消，后台无接口代劳。`ProcessOrderRefund` 成功后会为此打一条 warn。
