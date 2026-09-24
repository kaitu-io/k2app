# 路由器版预售 + 两品牌定价页 设计

> 承接 `2026-09-16-router-edition-web-onboarding-design.md`（路由器版网站与下单）与 `feat/site-nav`（顶栏结构）。
> 决策来源：2026-09-24 与 David 的对话。

## 0. 决策

| 项 | 决定 |
|---|---|
| 预售形态 | 现在可付款预订；**2026-11-11 起按下单顺序发货**；**服务期自发货日起算** |
| 预售价 | 首年含路由器 **$359**（划线原价 $399）；续费一年 $299 不变 |
| 发售后价 | 首年含路由器回到 $399（运营在发售日改套餐价） |
| 定价页 | 开途与 Overleap 各建 `/pricing`；两品牌顶栏「定价」与页脚同指该页 |

## 1. 后端（`api/`）

### 1.1 服务期自发货日起算

`api_admin_update_router_stage` 在 ready→shipped 的条件更新成功后，同一事务把台账所指线路的
`expires_at` 重设为 `shipped_at + plan.Month 个月`（取 `max(原 expires_at, 新值)`，只延不缩）。

- 只有含硬件的新购台账会走运营的 ready→shipped：续费单沿 `extendPrivateLine` 延期且台账继承旧
  `ShippedAt` 自动跳到 shipped，自备单没有发货阶段。因此不需要预售标记，规则对预售单与发售后正常单一致。
- 不改 `createPrivateNodeSubscription`：付款时 `expires_at = 付款 + 12 月` 仍是暂定值，发货时被重设。
- `PrivateNodeSubscription` 无 `plan` 关联字段时按 `PlanID` 查 `plans.month`。

### 1.2 预售期间的线路

不加「预售」字段、不改下单门。付款即建 pending 线路并派发 `provision` 运维任务，由运维决定何时认领
（预售期可留队列，发售前集中开机 → 烧录 → 寄出）。

### 1.3 套餐数据（运营动作，不在代码里）

```
router-std-1y: price 35900, origin_price 39900   ← 本次先改（套餐仍未激活，无副作用）
router-std-1y / router-svc-1y: is_active=1        ← 预售开卖动作：API 与网站都部署后再做
2026-11-11: router-std-1y price 39900             ← 发售日恢复原价
```

## 2. 网站（`web/`）

### 2.1 预售常量与日期切换

`lib/router-edition.ts`：

```ts
export const EDITION_PRICE_CENTS = { firstYear: 39900, renewal: 29900 };
export const ROUTER_PRESALE = { firstYear: 35900, shipsFrom: '2026-11-11' };
export function routerOffer(now = new Date()): { presale: boolean; firstYear: number; originFirstYear?: number; shipsFrom: string }
```

`presale = now < shipsFrom（UTC+8 零点）`。`/routers` 与开途 `/pricing` 从 `force-static` 改为
`revalidate = 3600`，发售日一小时内自动切换到非预售文案与 $399。数据库慢一步的失败方向是用户少付。

### 2.2 `/routers`

- 首屏：预售徽标「预售中 · 11 月 11 日起发货」；主 CTA「预售价 $359 立即预订」。
- 价格卡：$359 划线 $399；一句说明「预售价。11 月 11 日起按下单顺序发货，服务期自发货日起算」。
- FAQ 增两条：预售何时发货、服务期何时开始算（非预售期也保留后者）。
- meta description 用当前价。

### 2.3 `/purchase/router`

- 套餐行显示划线原价（`plan.originPrice > plan.price`）。
- 预售期显示发货与起算说明，按钮「预订并支付」；非预售期维持「去支付」。

### 2.4 `/account/router`

含硬件且 `shippedAt == 0` 的台账：订阅卡不显示「到期日 xxxx」，改显示「服务期自发货日起算，发货后显示到期日」。

### 2.5 `/pricing`

新增共享 namespace `pricing`（7 个 locale），`BRAND_NAMESPACES` 两品牌都加。

- **开途** `pricing/page.kaitu.tsx`：
  - App 版四档卡片：静态价表 `KAITU_SITE.pricing.app`（`[{pid, months, price, originPrice, highlight}]`），
    SSR 出价格保证 SEO；`PricingAppPlans` 客户端组件加载后拉 `/api/plans`，按 pid 用线上价覆盖静态价。
  - 路由器版一张卡：`routerOffer()` 出价，CTA → `/routers`。
  - 定价 FAQ（怎么收费、退款、设备数、路由器版区别）+ FAQPage JSON-LD。
- **Overleap** `pricing/page.overleap.tsx`：复用 `OverleapPricing` + `OverleapFAQ`（pricing / payment / cancel / devices 四条），
  `landing` 文案；`force-static`。
- 两品牌 `nav.nav.pricing` 顶栏与页脚 → `/pricing`；`staticRoutes` 加 `/pricing`。

## 3. 测试

- api：`TestAdminRouterFulfillments_ShipResetsExpiry`（发货后 `expires_at == shipped_at + month`）；
  续费继承台账不经此路径由既有 `TestRouterRenewal*` 覆盖。
- web：`routers-edition-ssr` 加预售 / 非预售两种日期；`router-edition.test` 覆盖 `routerOffer`；
  `pricing-page-ssr.test.tsx` 两品牌各渲染一次（零另一品牌词、零原始 key、含价格）；
  `brand-page-tree` 不需改（`pricing/` 是分裂页）；`brand-leak-ssr` metadata 清单加 `/pricing`；
  `site-config-keys` / `footer-brand-gates` 随配置变更调整期望。

## 4. 部署顺序

1. `make deploy-api`（发货重置到期日）。
2. 网站 `git push origin main:website`。
3. 后台改 `router-std-1y` 价格（已做）并激活两条套餐 → 预售开卖。
4. 2026-11-11：`router-std-1y` 价格改回 39900；网站按日期自动切换。

清单同步进 `docs/router-edition-prod-deploy.md`。
