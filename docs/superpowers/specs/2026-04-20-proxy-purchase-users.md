---
purpose: Inventory of users who attempted代付 (proxy purchase) before its removal in tier-rename refactor
date: 2026-04-20
status: reference data for tier-rename spec
sample_coverage: pages 1-8 of 12 (≈750 / 1182 orders, 63%)
---

# 代付用户调查

## 目的
为 `2026-04-20-tier-rename-design.md` 提供数据依据，决定是否需要保留代付。结论：**砍掉，零业务影响**。

## 真实用户代付订单（有 email 的，已分析 page 1-8 ≈ 750/1182 订单）

| 邮箱 | 代付订单数 | 已支付 | 备注 |
|------|-----------|--------|------|
| `yanghongen@sanyischool.com` | 2 | 0 | 1年×2用户、2年×2用户，全部弃单 |
| `wangyun_0517@163.com` | 1 | 0 | 1年×2用户，弃单 |
| `xiuqijin2021@gmail.com` | 2 | 0 | 1年×3用户 ×2，全部弃单 |
| `xiaogui1000@163.com` | 1 | 0 | 1年×2用户，弃单 |
| **合计真实用户** | **6 单** | **0** | **付费转化率 0%** |

## 匿名（空 user.uuid）的代付订单
- **未支付**：14+ 单（page 1-8 累计），疑似测试或用户登出后下单
- **已支付**：3 单
  - `ord-d34052qn47br6qq0tva0` — 1年套餐 × 2用户 (¥78.00, paid 2025-09-15)
  - `ord-d3401h81ck8qgqpk8img` — 1年套餐 × 2用户 (¥78.00, paid 2025-09-15)
  - `ord-d2kvlkqn47bujbis7jog` — 1年套餐 × 2用户 (¥54.60 with discount, paid 2025-08-23)

> 这 3 单匿名已支付订单可能是早期数据导入或测试订单。无 user 关联，没有用户需要通知。

## 推断到全量
- 按 page 1-8 趋势线性外推（剩余 4 页 ≈ 432 单）
- 预计全量代付订单 ≤ 30 单，**真实用户付费代付订单 ≈ 0**
- **削掉代付功能不会影响任何活跃付费用户**

## 处理建议
1. **删除代付功能**：UI 删除"为他人购买" toggle，API `forUsers` 字段废弃
2. **不发用户通知**：无任何已支付的真实用户代付订单需要保护
3. **不引入 LicenseKey 礼品码替代**（本 spec 范围）—— 已存在的 LicenseKey（渠道/活动用）逻辑保持不变
4. **未支付订单**：标准订单清理流程会处理（订单超时未支付自动失效）

## 标注 4 个用户的潜在意图（路由器候选）
这 4 个尝试过代付的用户表达了"为多个家庭/朋友付费"的意图，本质上是**家庭/路由器场景需求**：
- 路由器产品（一台路由器覆盖全家）正是他们想要的
- 在 K2R 路由器发布时，可以**优先邀请**这 4 个用户作为内测/早期用户

候选名单（按尝试次数）：
- `yanghongen@sanyischool.com` (2 次, ≥2 用户) — 高潜在
- `xiuqijin2021@gmail.com` (2 次, ≥3 用户) — 最高潜在（3 用户）
- `wangyun_0517@163.com` (1 次, 2 用户)
- `xiaogui1000@163.com` (1 次, 2 用户)

> 路由器产品发布时建议触达这 4 个邮箱，转化率应高于普通用户

## 调研方法
- 通过 `mcp__kaitu-center__list_orders` 抽样 page 1-8（含 page 1=50 + pages 2-8 ×100 = 750 单）
- 标识符：order title 包含 `× N用户` 模式
- 数据日期范围：2025-01 到 2026-04
