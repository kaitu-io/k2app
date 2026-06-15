# 专属线路 (Private Node) 精简 MVP — V1 生产部署运维手册

> 状态：**待部署**。代码在 `main`（领先 `origin/main` 10 commit，未推）。本手册是 ops 在
> 用户 `git push` 后执行的一站式清单。承接 task #20 (伞) / #41 (V0 数据门) / #42 (V2 keystone)。

## 0. 产品形态（部署前必读）

精简 MVP = **3 个独立右尺寸档**，互不升级、不做多节点订阅：

| 档 | pid | 售卖配额 (quota) | 定价 | 单节点 transfer 上限 (成本不变式) |
|----|-----|------------------|------|-----------------------------------|
| 1T | `pn-1t` | 1 TB | **$199 / 年** | 2 TB |
| 2T | `pn-2t` | 2 TB | **$398 / 年** | 3 TB |
| 4T | `pn-4t` | 4 TB | **$796 / 年** | 5 TB |

**工程对部署拓扑零感知**：Center 只存「配额 + 价格」。某档实际用 1 台还是 2 台 VPS 交付，是
ops 在 NodeOperation 队列里的人工决定，对代码/数据模型不可见。利润优化（如 4T 用 2×便宜 2T 节点 =
多一个 IP，用户更满意）在 ops 层透明发生。

**成本不变式 G1**：`traffic_total_bytes < bundle_transfer_bytes`（售卖配额 < 节点 transfer 上限）。
固定成本 provider（Lightsail bundle 含固定 transfer，超出不额外计费/会限速），所以**永不因流量超用亏钱**。
下方 SQL 的值已满足该不变式（1<2、2<3、4<5 TB）。

---

## 1. 部署前数据门 (V0 — task #41)

### 1.1 旧表孤儿检查（必须为 0 才能继续）

`migrate` 只 `AutoMigrate` 新建 `node_operations`，**不改名**旧表 `node_provision_jobs`。
产品未上线，旧表应不存在或在途行=0：

```sql
-- 若表不存在 = OK（产品从未上线）。若存在，open 行必须为 0：
SELECT COUNT(*) AS open_jobs FROM node_provision_jobs
WHERE status IN ('pending','running','open','provisioning');  -- 按实际枚举调整
```

> 期望：表不存在 **或** `open_jobs = 0`。非 0 = 停止，先人工处理在途任务。

### 1.2 EDM 模板存在性

欢迎邮件模板 `private-node-welcome` 必须存在（缺失则欢迎邮件 graceful skip，不报错但用户收不到）。
**dev/prod 均已通过 MCP `create_edm_template` 创建（prod id=32, zh-CN）。** 部署前复核：

```
MCP: list_edm_templates  → 确认 slug = "private-node-welcome" 存在
```

---

## 2. 部署步骤 (V1)

按顺序执行，每步绿灯再下一步：

1. **推送**：`git push origin main`（用户操作；main 领先 origin/main 10 commit）。
2. **Center 部署**：`make deploy-api`（与其它未推 commit 同车）。
3. **迁移**：手动 `kaitu-center migrate`
   - 纯 AutoMigrate 新建 `node_operations` 表 + 索引 `idx_op_sub_action_status(sub_id,action,status)`。
   - **无 schema 破坏性变更，无数据迁移。**
4. **MCP 构建**：`tools/kaitu-center` → `npm run build`（NodeOperation 4 工具）。
5. **Admin 站**：`git push origin main:website` → Amplify 自动部署（`/manager/node-operations` 页）。
6. **webapp bundle**：各平台发版含 webapp（专属线路购买/管理面板）。桌面端 bundle 进
   `k2/webui/dist`；移动端随发版。
7. **生产目录 SQL**：执行下方 §3（建 3 档 Plan + Spec）。

---

## 3. 生产目录幂等 SQL (keystone)

**幂等**：可重复执行。`plans.pid` 与 `private_node_plan_specs.plan_id` 均为 UNIQUE，
用 `ON DUPLICATE KEY UPDATE` 收敛。Spec 用子查询按 `pid` 反查 `plan_id`，
**不硬编码 auto-increment id**（prod 的 id 与 dev 不同）。

> ⚠️ **ops 上线前必改两处占位符**（dev 用的是占位值）：
> - `bundle_id`：改成真实 AWS Lightsail bundle id（dev 占位 `lightsail_2tb/3tb/5tb`；
>   真值形如 `medium_3_0` 等，须对应「含 ≥ 不变式上限 transfer」的真实 bundle）。
> - `image_id`：改成真实 k2s 基础镜像 id（dev 占位 `k2s_base`）。
>
> `traffic_total_bytes`（售卖配额）与 `price` 已是定稿值，**不要改**。
> `bundle_transfer_bytes` 须 > `traffic_total_bytes`（成本不变式），按真实 bundle 的 transfer 上限填。

```sql
-- 注：plans 用 product 列做产品判别（值 app|private_node）。kind 从未上线（仅存在于
-- 未推送的本地提交），prod 无该列；Center 的 migrate 用 AutoMigrate 新建 product 列
-- （not null default 'app'，自动回填老 app 套餐），无需改名/值迁移。
-- ============ 3 档 Plan（product=private_node, 年付 month=12）============
INSERT INTO plans (created_at, updated_at, pid, label, price, origin_price, month, highlight, is_active, tier, product)
VALUES
  (NOW(3), NOW(3), 'pn-1t', '专属线路 1T', 19900, 19900, 12, 0, 1, 'basic', 'private_node'),
  (NOW(3), NOW(3), 'pn-2t', '专属线路 2T', 39800, 39800, 12, 0, 1, 'basic', 'private_node'),
  (NOW(3), NOW(3), 'pn-4t', '专属线路 4T', 79600, 79600, 12, 0, 1, 'basic', 'private_node')
ON DUPLICATE KEY UPDATE
  label=VALUES(label), price=VALUES(price), origin_price=VALUES(origin_price),
  month=VALUES(month), is_active=VALUES(is_active), tier=VALUES(tier),
  product=VALUES(product), updated_at=NOW(3);

-- ============ 3 档 Spec（按 pid 反查 plan_id，幂等 on plan_id）============
-- pn-1t：售卖 1 TB，节点上限 2 TB
INSERT INTO private_node_plan_specs
  (plan_id, provider, ip_type, allowed_regions, image_id, bundle_id, traffic_total_bytes, bundle_transfer_bytes)
SELECT id, 'aws_lightsail', 'non_residential',
  '["us-virginia","us-ohio","us-oregon","us-siliconvalley","ca-central","eu-ireland","eu-london","eu-paris","eu-frankfurt","eu-stockholm","me-dubai","me-riyadh","ap-tokyo","ap-seoul","ap-singapore","ap-sydney","ap-jakarta","ap-mumbai","ap-bangkok","ap-kualalumpur","ap-manila","cn-hongkong","cn-taiwan","sa-saopaulo"]',
  'k2s_base', 'lightsail_2tb', 1000000000000, 2000000000000
FROM plans WHERE pid='pn-1t'
ON DUPLICATE KEY UPDATE
  provider=VALUES(provider), ip_type=VALUES(ip_type), allowed_regions=VALUES(allowed_regions),
  image_id=VALUES(image_id), bundle_id=VALUES(bundle_id),
  traffic_total_bytes=VALUES(traffic_total_bytes), bundle_transfer_bytes=VALUES(bundle_transfer_bytes);

-- pn-2t：售卖 2 TB，节点上限 3 TB
INSERT INTO private_node_plan_specs
  (plan_id, provider, ip_type, allowed_regions, image_id, bundle_id, traffic_total_bytes, bundle_transfer_bytes)
SELECT id, 'aws_lightsail', 'non_residential',
  '["us-virginia","us-ohio","us-oregon","us-siliconvalley","ca-central","eu-ireland","eu-london","eu-paris","eu-frankfurt","eu-stockholm","me-dubai","me-riyadh","ap-tokyo","ap-seoul","ap-singapore","ap-sydney","ap-jakarta","ap-mumbai","ap-bangkok","ap-kualalumpur","ap-manila","cn-hongkong","cn-taiwan","sa-saopaulo"]',
  'k2s_base', 'lightsail_3tb', 2000000000000, 3000000000000
FROM plans WHERE pid='pn-2t'
ON DUPLICATE KEY UPDATE
  provider=VALUES(provider), ip_type=VALUES(ip_type), allowed_regions=VALUES(allowed_regions),
  image_id=VALUES(image_id), bundle_id=VALUES(bundle_id),
  traffic_total_bytes=VALUES(traffic_total_bytes), bundle_transfer_bytes=VALUES(bundle_transfer_bytes);

-- pn-4t：售卖 4 TB，节点上限 5 TB（ops 实际可用 2×2T 交付，对本数据不可见）
INSERT INTO private_node_plan_specs
  (plan_id, provider, ip_type, allowed_regions, image_id, bundle_id, traffic_total_bytes, bundle_transfer_bytes)
SELECT id, 'aws_lightsail', 'non_residential',
  '["us-virginia","us-ohio","us-oregon","us-siliconvalley","ca-central","eu-ireland","eu-london","eu-paris","eu-frankfurt","eu-stockholm","me-dubai","me-riyadh","ap-tokyo","ap-seoul","ap-singapore","ap-sydney","ap-jakarta","ap-mumbai","ap-bangkok","ap-kualalumpur","ap-manila","cn-hongkong","cn-taiwan","sa-saopaulo"]',
  'k2s_base', 'lightsail_5tb', 4000000000000, 5000000000000
FROM plans WHERE pid='pn-4t'
ON DUPLICATE KEY UPDATE
  provider=VALUES(provider), ip_type=VALUES(ip_type), allowed_regions=VALUES(allowed_regions),
  image_id=VALUES(image_id), bundle_id=VALUES(bundle_id),
  traffic_total_bytes=VALUES(traffic_total_bytes), bundle_transfer_bytes=VALUES(bundle_transfer_bytes);
```

### 3.1 SQL 后置校验

```sql
SELECT p.pid, p.label, p.price, p.product, p.is_active,
       s.provider, s.ip_type, s.bundle_id,
       s.traffic_total_bytes, s.bundle_transfer_bytes,
       (s.traffic_total_bytes < s.bundle_transfer_bytes) AS invariant_ok
FROM plans p JOIN private_node_plan_specs s ON s.plan_id = p.id
WHERE p.pid IN ('pn-1t','pn-2t','pn-4t') ORDER BY p.pid;
```

期望 3 行，每行 `invariant_ok = 1`，`bundle_id`/`image_id` 已是真实值（非 `lightsail_*tb`/`k2s_base` 占位）。

---

## 4. 部署后冒烟（进 V2 keystone — task #42）

部署完成后，最小可信冒烟（不需要真扣钱可在 dev 做完整链路，prod 做只读核对）：

1. **目录可见**：webapp 购买页能看到 3 档（价格 $199/$398/$796，区域选择器出 24 个 AWS region）。
2. **新购全链路**（V2 keystone，可 dev 真支付 / prod 首单观察）：
   下单 → WordGate 支付 → webhook `applyOrderToBuyer` 建 `PrivateNodeSubscription` →
   自动 emit provision NodeOperation（走 FOR UPDATE 幂等）→ 装机工单 `pn-install-<id>` 生成 +
   Slack 告警 + 异步欢迎邮件（`private-node-welcome`）。
3. **生命周期**（V3 — task #43）：到期 cron 派 stop/destroy；续费取消 open stop/destroy。

> 真机三连（撞顶 → 节点心跳 → 路由器重连恢复）归 task #20，封顶信心 6-7/10 直到真机 smoke 跑过。

---

## 5. 回滚

目录 SQL 可逆（停售即可，不删数据，避免悬挂已购订阅的 FK）：

```sql
UPDATE plans SET is_active = 0, updated_at = NOW(3) WHERE pid IN ('pn-1t','pn-2t','pn-4t');
```

代码层：`node_operations` 表为纯新增，无破坏性；如需回退 Center 版本按常规 `make deploy-api` 回滚镜像即可。
