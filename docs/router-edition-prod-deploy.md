# 开途路由器版 · 生产部署清单

> 承接 `docs/superpowers/specs/2026-09-16-router-edition-web-onboarding-design.md`。Center 部署走
> `make deploy-api`（`scripts/deploy-center.sh` 会在装完 systemd 服务后暂停，等管理员手动执行
> `./kaitu-center migrate` 再回车继续重启）。

## 1. 迁移

`kaitu-center migrate`（`AutoMigrate`）自动处理：`plans.hardware_sku`、`orders.router_shipping` 两个新列
（additive，存量行零迁移）+ 新表 `router_fulfillments`。无需手工 DDL。

## 2. 目录（幂等 SQL，`migrate` 之后执行）

已在开发库（`root:dev@127.0.0.1:3306/kaitu`）用同一份 SQL 验证过幂等性（事务内跑两遍，第二遍不新增行，
详见本次任务报告）：

```sql
-- 两条路由器版套餐（价格单位：美分）。规格复用 pn-dc-2t 的 spec（同规格、不同售卖包装）。
INSERT INTO plans (pid, label, price, origin_price, month, highlight, is_active, tier, product, brand, hardware_sku, created_at, updated_at)
SELECT 'router-std-1y', '开途路由器版·首年（含路由器）', 39900, 39900, 12, 1, 0, 'basic', 'router', 'kaitu', 'redmi-ax6s', NOW(), NOW()
WHERE NOT EXISTS (SELECT 1 FROM plans WHERE pid = 'router-std-1y');
INSERT INTO plans (pid, label, price, origin_price, month, highlight, is_active, tier, product, brand, hardware_sku, created_at, updated_at)
SELECT 'router-svc-1y', '开途路由器版·续费一年', 29900, 29900, 12, 0, 0, 'basic', 'router', 'kaitu', '', NOW(), NOW()
WHERE NOT EXISTS (SELECT 1 FROM plans WHERE pid = 'router-svc-1y');

INSERT INTO private_node_plan_specs (plan_id, ip_type, allowed_regions, traffic_total_bytes)
SELECT p.id, s.ip_type, s.allowed_regions, s.traffic_total_bytes
FROM plans p JOIN plans src ON src.pid = 'pn-dc-2t' JOIN private_node_plan_specs s ON s.plan_id = src.id
WHERE p.pid IN ('router-std-1y', 'router-svc-1y')
  AND NOT EXISTS (SELECT 1 FROM private_node_plan_specs x WHERE x.plan_id = p.id);
```

`is_active` 先留 `0`（两条套餐入目录但 `/api/products/router/plans` 不返回，因为该端点过滤
`is_active=true`）；网站上线（Plan B，本次未随此清单交付）当天再由 admin 后台把两条 Plan 的 `is_active`
置 `1`。`allowed_regions` 沿用 `pn-dc-2t` 的现值；结账页若要给一个默认选中项，建议取列表首项。

**前提**：执行前确认生产库已有 `pn-dc-2t`（`private_node` 产品线的既有内部规格）——上面第二段 SQL 靠
`JOIN plans src ON src.pid = 'pn-dc-2t'` 拷贝它的 `ip_type` / `allowed_regions` / `traffic_total_bytes`；
若生产库还没有这条内部规格，需先按专属线路的既有流程建好，再跑本清单第二段。

## 3. EDM 模板

`router-welcome`（zh-CN 起底，变量 `region`）需在 EDM 模板表中存在；缺失时欢迎邮件静默跳过
（`templateSlugExists` 门，`logic_private_node_onboarding.go`），**不阻断下单**，但用户收不到引导邮件。

## 4. 验证（部署后）

1. `GET /api/products/router/plans`：`is_active` 置 `1` 前返回空列表（预期，不是故障）；置 `1` 后返回
   两条，且带 `hardwareSku` / `privateNode.allowedRegions`。
2. 后台 `GET /app/router/fulfillments`：200，空列表。
3. 首单真机：泰国刷好一台 AX6S，用真实订单走 paid → provisioning → ready → 代铸凭证烧录 → shipped →
   收货插线 → online（账户页 `GET /api/user/router` 与后台 `GET /app/router/fulfillments` 同时可见）。
