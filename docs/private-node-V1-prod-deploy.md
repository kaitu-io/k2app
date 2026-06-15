# 专属线路 (Private Node) 精简 MVP — V1 生产部署运维手册

> 状态：**待部署**。代码在 `main`（领先 `origin/main` 15 commit，未推）。本手册是 ops 在
> 用户 `git push` 后执行的一站式清单。承接 task #20 (伞) / #41 (V0 数据门) / #42 (V2 keystone)。

## 部署前外部依赖（V0 门收敛后，仅剩这些非代码输入）

代码与数据门已就绪（详见 §1 的 2026-06-15 prod 复核）。上线前需要**人来定/填**的，只剩：

| # | 依赖 | 谁 | 硬阻断时点 | 现状 |
|---|------|----|-----------|------|
| 1 | provider / bundle_id / image_id 已**不进 catalog/代码**——开机时由运维/agent 自定（选 bundle 时保证自带流量 > 卖出额度） | ops | **首个订单 provision 时**（纯部署执行，无数据门） | — 运维职责 |
| 2 | 确认/补建 EDM `private-node-welcome` 模板（§1.2） | ops | 首单欢迎邮件（best-effort，不阻断下单） | ⚠️ 未坐实 |

> 定价已定稿（$199/$398/$796），售卖配额/成本不变式已定稿 —— **不需要再做产品决策**。
> 一切代码改动已在 `main`、静态全绿、双审。剩余风险集中在 §4 的真支付/真机 smoke（task #42/#20），与本数据门无关。

## 0. 产品形态（部署前必读）

精简 MVP = **3 个独立右尺寸档**，互不升级、不做多节点订阅：

| 档 | pid | 售卖配额 (quota) | 定价 | 运维选 bundle 参考下限 |
|----|-----|------------------|------|------------------------|
| 1T | `pn-1t` | 1 TB | **$199 / 年** | 自带 ≥2 TB |
| 2T | `pn-2t` | 2 TB | **$398 / 年** | 自带 ≥3 TB |
| 4T | `pn-4t` | 4 TB | **$796 / 年** | 自带 ≥5 TB |

**工程对部署拓扑零感知**：Center 只存「配额 + 价格 + 住宅? + 可选地区」。用哪个 provider / bundle /
镜像、某档用 1 台还是 2 台 VPS 交付，全是 ops 在 NodeOperation 队列里的人工决定，对代码/数据模型不可见。

**成本安全护栏归属运维/主机（代码不再校验）**：固定成本 provider（Lightsail bundle 含固定 transfer，
超出不额外计费/会限速），运维选 bundle 时须保证其自带流量 > 卖出额度（卖 2T → 选 ≥3T bundle，见上表
"参考下限"），即可**永不因流量超用亏钱**。这一把关由运维在开机时做，不在 catalog/代码里强制。

---

## 1. 部署前数据门 (V0 — task #41)

> **2026-06-15 prod 只读复核结果**（MCP，无写操作）：
> - §1.1 孤儿检查 **PASS（按缺失）** — `list_provisioning_intents` 命中 `/app/provision-jobs` 返 **HTTP 404**：provisioning 功能从未部署，`node_provision_jobs` 表不存在，在途行=0。
> - §1.3 目录洁净 **PASS** — `list_admin_plans` 返 prod 仅 4 档（`1y/2y/3y/5y`，全 `tier:basic`），无 `pn-*`，响应**无 `product` 字段**（再证 kind/product 从未上线）。catalog SQL 为纯 INSERT。
> - §1.2 EDM **未能确认** — `list_edm_templates` 仅返 19 条中的前 10 条（工具无分页参数），`private-node-welcome` 不在前 10。须 ops 在 DB 侧或分页确认（详见下）。

### 1.1 旧表孤儿检查（必须为 0 才能继续）

`migrate` 只 `AutoMigrate` 新建 `node_operations`，**不改名**旧表 `node_provision_jobs`。
产品未上线，旧表应不存在或在途行=0。✅ **已复核：prod `/app/provision-jobs` 404 = 表不存在 = PASS。**
如未来重查：

```sql
-- 若表不存在 = OK（产品从未上线）。若存在，open 行必须为 0：
SELECT COUNT(*) AS open_jobs FROM node_provision_jobs
WHERE status IN ('pending','running','open','provisioning');  -- 按实际枚举调整
```

> 期望：表不存在 **或** `open_jobs = 0`。非 0 = 停止，先人工处理在途任务。

### 1.2 EDM 模板存在性（⚠️ 部署前唯一须人工确认的数据门）

欢迎邮件模板 `private-node-welcome` 必须存在（缺失则欢迎邮件 graceful skip，不报错但用户收不到）。
历史记录称已建（prod id=32, zh-CN），但 **2026-06-15 MCP 复核因 `list_edm_templates` 无分页、仅见前 10 条而未能坐实**。部署前 ops 二选一确认：

```sql
-- 方式 A（DB 直查，最可靠）：
SELECT id, slug, language, is_active FROM email_marketing_templates WHERE slug = 'private-node-welcome';
```

```
方式 B（MCP）：list_edm_templates 翻到含 id=32 的页，确认 slug='private-node-welcome'
```

> 若不存在：用 MCP `create_edm_template` 建 `slug=private-node-welcome`（zh-CN 起底，其余语言懒翻译）。
> 缺失不阻断下单（欢迎邮件 best-effort），但属可见体验缺口，应在首单前补齐。

### 1.3 目录洁净（catalog 为纯新增）

✅ **已复核：prod 仅 4 档存量 plan，无 `pn-1t/pn-2t/pn-4t`，§3 的 catalog SQL 为干净 INSERT。**

---

## 2. 部署步骤 (V1)

按顺序执行，每步绿灯再下一步：

1. **推送**：`git push origin main`（用户操作；main 领先 origin/main，批量未推）。
2. **Center 部署**：`make deploy-api`（与其它未推 commit 同车）。
3. **迁移**：手动 `kaitu-center migrate`
   - AutoMigrate 新建 `node_operations` 表 + 索引 `idx_op_sub_action_status(sub_id,action,status)`。
   - ⚠️ **必做 DDL（否则 §3 catalog INSERT 必失败）**：`private_node_plan_specs` 已存在且带旧列
     `provider`(NOT NULL 无默认)/`image_id`/`bundle_id`/`bundle_transfer_bytes`。新模型不再写这些列，
     AutoMigrate **不会自动删**，而 `provider` NOT NULL 会拒绝省略它的 INSERT（dev 已实测复现并修复）。
     表为空（零订阅），直接删：
     ```sql
     ALTER TABLE private_node_plan_specs
       DROP COLUMN provider, DROP COLUMN image_id,
       DROP COLUMN bundle_id, DROP COLUMN bundle_transfer_bytes;
     ```
   - 这是本次唯一需手动跑的 DDL；其余无破坏性变更、无数据迁移。
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

> ⚠️ **catalog 不再含任何部署细节**。`provider` / `bundle_id` / `image_id` / `bundle_transfer_bytes`
> 已从 spec 表移除——这些"怎么部署"的选择属于运维/agent 在**开机时**决定，不进数据。
> spec 只表达业务意图：`ip_type`（住宅?）+ `allowed_regions`（可选地区）+ `traffic_total_bytes`（卖出配额）。
>
> **成本安全护栏归属运维/主机**：运维选 bundle 时须保证其自带流量 > 卖出额度
> （卖 2T → 选 ≥3T 的 bundle），代码不再校验。`traffic_total_bytes` 与 `price` 是定稿值，**不要改**。

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
-- pn-1t：售卖 1 TB
INSERT INTO private_node_plan_specs
  (plan_id, ip_type, allowed_regions, traffic_total_bytes)
SELECT id, 'non_residential',
  '["us-virginia","us-ohio","us-oregon","us-siliconvalley","ca-central","eu-ireland","eu-london","eu-paris","eu-frankfurt","eu-stockholm","me-dubai","me-riyadh","ap-tokyo","ap-seoul","ap-singapore","ap-sydney","ap-jakarta","ap-mumbai","ap-bangkok","ap-kualalumpur","ap-manila","cn-hongkong","cn-taiwan","sa-saopaulo"]',
  1000000000000
FROM plans WHERE pid='pn-1t'
ON DUPLICATE KEY UPDATE
  ip_type=VALUES(ip_type), allowed_regions=VALUES(allowed_regions),
  traffic_total_bytes=VALUES(traffic_total_bytes);

-- pn-2t：售卖 2 TB
INSERT INTO private_node_plan_specs
  (plan_id, ip_type, allowed_regions, traffic_total_bytes)
SELECT id, 'non_residential',
  '["us-virginia","us-ohio","us-oregon","us-siliconvalley","ca-central","eu-ireland","eu-london","eu-paris","eu-frankfurt","eu-stockholm","me-dubai","me-riyadh","ap-tokyo","ap-seoul","ap-singapore","ap-sydney","ap-jakarta","ap-mumbai","ap-bangkok","ap-kualalumpur","ap-manila","cn-hongkong","cn-taiwan","sa-saopaulo"]',
  2000000000000
FROM plans WHERE pid='pn-2t'
ON DUPLICATE KEY UPDATE
  ip_type=VALUES(ip_type), allowed_regions=VALUES(allowed_regions),
  traffic_total_bytes=VALUES(traffic_total_bytes);

-- pn-4t：售卖 4 TB（ops 实际可用 2×2T 交付，对本数据不可见）
INSERT INTO private_node_plan_specs
  (plan_id, ip_type, allowed_regions, traffic_total_bytes)
SELECT id, 'non_residential',
  '["us-virginia","us-ohio","us-oregon","us-siliconvalley","ca-central","eu-ireland","eu-london","eu-paris","eu-frankfurt","eu-stockholm","me-dubai","me-riyadh","ap-tokyo","ap-seoul","ap-singapore","ap-sydney","ap-jakarta","ap-mumbai","ap-bangkok","ap-kualalumpur","ap-manila","cn-hongkong","cn-taiwan","sa-saopaulo"]',
  4000000000000
FROM plans WHERE pid='pn-4t'
ON DUPLICATE KEY UPDATE
  ip_type=VALUES(ip_type), allowed_regions=VALUES(allowed_regions),
  traffic_total_bytes=VALUES(traffic_total_bytes);
```

### 3.1 SQL 后置校验

```sql
SELECT p.pid, p.label, p.price, p.product, p.is_active,
       s.ip_type, s.allowed_regions, s.traffic_total_bytes
FROM plans p JOIN private_node_plan_specs s ON s.plan_id = p.id
WHERE p.pid IN ('pn-1t','pn-2t','pn-4t') ORDER BY p.pid;
```

期望 3 行，`traffic_total_bytes` 分别为 1/2/4 TB，`ip_type=non_residential`。
（成本安全护栏=运维选够大 bundle，不在数据里校验。）

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
