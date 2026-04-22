# Tier Rename 部署 Checklist

Commit reference: `feature/tier-rename` branch (24 commits ahead of main).

## Pre-deploy

- [ ] 全量 DB backup（RDS snapshot）
- [ ] Staging 环境完整跑通迁移：
  - [ ] `./kaitu-center migrate -c config.yml` 第一次跑成功
  - [ ] 同样命令第二次跑成功（验证幂等：ALTER + UPDATE + DROP 全部应 no-op）
  - [ ] 验证 `plans.tier` 列默认值为 `'basic'`
  - [ ] 验证 `users.tier` 列默认值为 `'basic'`
  - [ ] 验证 `plans.max_device` / `plans.max_router_device` / `plans.max_lan_client` 列已删
  - [ ] 验证 `users.max_device` / `users.max_router_device` / `users.max_lan_client` 列已删
- [ ] Staging 验证 API 行为：
  - [ ] `GET /api/plans` 的 JSON 输出包含 `maxDevice` / `maxRouterDevice` / `maxLanClient`（来自 MarshalJSON 注入，非 DB 列）
  - [ ] `GET /api/tiers` 返回 4 个 tier，每个嵌套 plans
  - [ ] `GET /app/tiers` (admin) 返回 4 个 tier，包含 inactive plans
  - [ ] `POST /api/orders` 带 `forUserUUIDs` 或 `forUsers` → 422002
  - [ ] `POST /api/orders` 首次购买任意 tier → 成功
  - [ ] `POST /api/orders` 二次购买不同 tier → 422001
  - [ ] `PUT /app/users/:uuid/tier` 写入审计 `admin_audit_logs`
- [ ] Staging 验证前端：
  - [ ] webapp Purchase 页面无代付 UI
  - [ ] webapp Purchase 页面首次购买看到所有 plans
  - [ ] webapp Purchase 页面二次购买只看到同 tier plans
  - [ ] web Purchase 页面同上
  - [ ] manager plans 编辑器 tier dropdown 4 选项 + 配额预览卡
  - [ ] manager users 详情页“修改档位”按钮 + Dialog
- [ ] 监控告警阈值：
  - [ ] 422002 错误码率 > 0.1% 告警（代付下线的老客户端追踪）
  - [ ] 422001 错误码率 > 1% 告警（tier 不匹配）
- [ ] 通知客服/运营：代付功能下线，准备话术；tier 修改需要提供 reason，已记录审计日志

## Deploy

- [ ] 避开订单高峰部署（参考 `/app/order/statistics` 日/周节律）
- [ ] 部署后 5 分钟内手动跑一次完整购买流程
- [ ] `migrate` 命令的启动日志中确认 `[migrate] tier rename migrations completed`

## Post-deploy (24h)

- [ ] 监控错误码分布（Grafana / Datadog dashboard）
- [ ] 监控老客户端占比（422002 出现率随时间下降）
- [ ] 通知运营团队：`docs/superpowers/specs/2026-04-20-proxy-purchase-users.md` 列出的 4 个潜在路由器用户可在 K2R 发布时优先触达

## Rollback 预案

- **Phase 1（部署后 < 10min 内发现严重问题）：**
  - 代码回滚：`git revert <merge-commit>`
  - DB 不回滚（ALTER/UPDATE 在旧代码下仍兼容）
- **Phase 2（DROP COLUMN 已执行，发现严重问题）：**
  - DROP COLUMN 不可回滚（除非从 snapshot 还原）
  - 评估影响面 → 发布 hotfix 而非回滚
  - 最后手段：从 pre-deploy snapshot 还原（会丢失迁移后的业务数据）

## 验收标准

- [ ] CI 全绿
- [ ] Staging 迁移两次幂等通过
- [ ] 生产部署后 24h 无新增 P1/P0 相关告警
- [ ] 运营报告客服反馈正面/中性（无“用户大量投诉无法购买”）
