# 授权码批次解耦设计

> 将授权码的批量生成与活动码（Campaign）解耦，引入 Batch 作为独立组织单位，活动码恢复为纯折扣/优惠券逻辑。

## 背景

当前授权码（LicenseKey）与活动码（Campaign）紧密耦合：

- 批量生成只能通过 `POST /app/campaigns/:id/issue-keys` 走活动码入口
- `GenerateLicenseKeysForCampaign()` 硬编码活动码的用户匹配、份数、邮件发送
- 统计按 `campaign_id` 分组，授权码没有独立的统计维度
- Campaign 模型包含 `IsShareable`/`SharesPerUser` 等与折扣无关的字段

## 目标

1. 授权码以 Batch（批次）为核心组织单位，独立管理和统计
2. 活动码回归纯折扣/优惠券，删除所有授权码相关字段和逻辑
3. 转化追踪：兑换 → 付费的漏斗分析作为核心指标

## 设计决策

| 决策 | 结论 | 理由 |
|------|------|------|
| 授权码是否绑定用户 | 不绑定 | 裂变场景已有独立的邀请码系统；授权码核心场景是渠道分发 |
| 兑换限制 | Batch 级别 RecipientMatcher，兑换时校验 | 不在生成时绑定，保持灵活性 |
| 转化统计 | 查询时实时 JOIN | Batch 数量级小，不需要异步 Worker |
| 邮件发送 | 不在本次范围 | 运营通过接口获取 keys 后自行分发 |

## 数据模型

### 新增 `license_key_batches` 表

```sql
CREATE TABLE license_key_batches (
    id                BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    name              VARCHAR(255) NOT NULL,           -- "Apr Twitter 投放"
    source_tag        VARCHAR(100) NOT NULL DEFAULT '', -- 渠道标签："twitter"、"kol-xxx"、"winback"
    recipient_matcher VARCHAR(50)  NOT NULL DEFAULT 'all', -- 兑换限制："never_paid"、"all"
    plan_days         INT          NOT NULL,           -- 兑换后给多少天
    quantity          INT          NOT NULL,           -- 生成数量
    expires_at        BIGINT       NOT NULL,           -- 授权码过期时间 (unix)
    note              TEXT,                            -- 备注
    created_by_user_id BIGINT UNSIGNED NOT NULL,       -- 管理员 ID
    created_at        DATETIME(3)  NOT NULL,
    updated_at        DATETIME(3)  NOT NULL,
    deleted_at        DATETIME(3)  DEFAULT NULL,       -- 软删除
    INDEX idx_source_tag (source_tag),
    INDEX idx_deleted_at (deleted_at)
);
```

### 修改 `license_keys` 表

```sql
-- 新增
ALTER TABLE license_keys ADD COLUMN batch_id BIGINT UNSIGNED NOT NULL DEFAULT 0;
ALTER TABLE license_keys ADD INDEX idx_batch_id (batch_id);

-- 数据迁移后删除
ALTER TABLE license_keys DROP COLUMN campaign_id;
ALTER TABLE license_keys DROP COLUMN source;
ALTER TABLE license_keys DROP COLUMN recipient_matcher;
ALTER TABLE license_keys DROP COLUMN created_by_user_id;
```

保留字段：`id`, `code`, `plan_days`, `expires_at`, `is_used`, `used_by_user_id`, `used_at`, `batch_id`

### 修改 `campaigns` 表

```sql
ALTER TABLE campaigns DROP COLUMN is_shareable;
ALTER TABLE campaigns DROP COLUMN shares_per_user;
```

## API 设计

### 批次管理

| Method | Path | 说明 |
|--------|------|------|
| `POST` | `/app/license-key-batches` | 创建批次 + 生成 keys（需审批） |
| `GET` | `/app/license-key-batches` | 批次列表（含已用/总量摘要） |
| `GET` | `/app/license-key-batches/:id` | 批次详情 |
| `GET` | `/app/license-key-batches/:id/keys` | 该批次下的 keys 列表（分页 + 状态筛选） |
| `DELETE` | `/app/license-key-batches/:id` | 删除批次（需审批，级联删未使用的 keys） |

### 批次统计

| Method | Path | 说明 |
|--------|------|------|
| `GET` | `/app/license-key-batches/stats` | 全局统计仪表盘 |
| `GET` | `/app/license-key-batches/stats/by-source` | 按 source_tag 聚合 |
| `GET` | `/app/license-key-batches/stats/trend` | 按时间段兑换趋势 |

### 保留（简化）

| Method | Path | 说明 |
|--------|------|------|
| `GET` | `/app/license-keys` | 列表（filter 改为 batch_id） |
| `DELETE` | `/app/license-keys/:id` | 删除单个 key |

### 删除

| Method | Path | 理由 |
|--------|------|------|
| `POST` | `/app/campaigns/:id/issue-keys` | 解耦 |
| `POST` | `/app/license-keys` | 不再有脱离 batch 的手动创建 |
| `GET` | `/app/license-keys/stats` | 由 batch stats 替代 |

## 请求/响应类型

### 创建批次

```go
type CreateLicenseKeyBatchRequest struct {
    Name             string `json:"name" binding:"required"`
    SourceTag        string `json:"source_tag"`
    RecipientMatcher string `json:"recipient_matcher" binding:"required,oneof=all never_paid"`
    PlanDays         int    `json:"plan_days" binding:"required,min=1"`
    Quantity         int    `json:"quantity" binding:"required,min=1,max=10000"`
    ExpiresInDays    int    `json:"expires_in_days" binding:"required,min=1"`
    Note             string `json:"note"`
}

type CreateLicenseKeyBatchResponse struct {
    Batch LicenseKeyBatchBrief `json:"batch"`
}
```

### 批次列表

```go
type LicenseKeyBatchBrief struct {
    ID               uint64 `json:"id"`
    Name             string `json:"name"`
    SourceTag        string `json:"source_tag"`
    RecipientMatcher string `json:"recipient_matcher"`
    PlanDays         int    `json:"plan_days"`
    Quantity         int    `json:"quantity"`
    ExpiresAt        int64  `json:"expires_at"`
    Note             string `json:"note"`
    RedeemedCount    int64  `json:"redeemed_count"`   // 已兑换
    ExpiredCount     int64  `json:"expired_count"`     // 已过期
    CreatedAt        int64  `json:"created_at"`
}
```

### 批次下的 Keys 列表

```go
// GET /app/license-key-batches/:id/keys?status=unused&page=1&page_size=50
// status 可选值: all, used, unused, expired

type LicenseKeyItem struct {
    ID           uint64 `json:"id"`
    Code         string `json:"code"`
    PlanDays     int    `json:"plan_days"`
    ExpiresAt    int64  `json:"expires_at"`
    IsUsed       bool   `json:"is_used"`
    UsedByUserID *uint64 `json:"used_by_user_id,omitempty"`
    UsedAt       *int64  `json:"used_at,omitempty"`
}
```

### 批次统计

```go
type BatchStats struct {
    BatchID        uint64  `json:"batch_id"`
    Name           string  `json:"name"`
    SourceTag      string  `json:"source_tag"`
    TotalKeys      int64   `json:"total_keys"`
    Redeemed       int64   `json:"redeemed"`
    Expired        int64   `json:"expired"`
    RedeemRate     float64 `json:"redeem_rate"`
    ConvertedUsers int64   `json:"converted_users"`   // 兑换后付费人数
    ConversionRate float64 `json:"conversion_rate"`    // 兑换→付费转化率
    Revenue        uint64  `json:"revenue"`            // 转化用户带来的收入 (cents)
}
```

## 转化追踪（核心）

兑换→付费转化率在查询时实时计算：

```sql
SELECT
  b.id,
  b.name,
  b.source_tag,
  b.quantity                                                    AS total_keys,
  SUM(CASE WHEN k.is_used = 1 THEN 1 ELSE 0 END)              AS redeemed,
  SUM(CASE WHEN k.is_used = 0 AND k.expires_at < UNIX_TIMESTAMP() THEN 1 ELSE 0 END) AS expired,
  COUNT(DISTINCT CASE WHEN o.id IS NOT NULL THEN o.user_id END) AS converted_users,
  COALESCE(SUM(CASE WHEN o.id IS NOT NULL THEN o.amount END), 0) AS revenue
FROM license_key_batches b
JOIN license_keys k ON k.batch_id = b.id
LEFT JOIN orders o ON o.user_id = k.used_by_user_id
  AND o.status = 'paid'
  AND o.created_at > k.used_at
WHERE b.deleted_at IS NULL
GROUP BY b.id;
```

## 兑换逻辑变更

`RedeemLicenseKey()` 改动：

1. 查询 key 时 Preload Batch：`tx.Preload("Batch").Where("code = ?", code).First(&k)`
2. `MatchLicenseKey()` 从 `key.Batch.RecipientMatcher` 读取限制条件（替代 `key.RecipientMatcher`）
3. 其余逻辑不变

## 审批回调

新增回调 `"license_key_batch_create"`：
1. 创建 `LicenseKeyBatch` 记录
2. 调用 `GenerateLicenseKeysForBatch(batch)` 生成指定数量的 keys
3. 返回 batch 信息

## 活动码清理

删除的代码：
- `GenerateLicenseKeysForCampaign()` — `logic_license_key.go`
- `api_admin_issue_license_keys` handler — `api_admin_campaigns.go`
- `executeApprovalCampaignIssueKeys` 回调 — `logic_approval_callbacks.go`
- `SendLicenseKeyEmails()` — `worker_license_key.go`
- Campaign model 的 `IsShareable`、`SharesPerUser` 字段 — `model.go`
- Web admin 活动码页面的"发放授权码"按钮

## MCP 工具更新

### 删除

- `issue_campaign_keys` — `admin-campaigns.ts`

### 替换

- `create_license_keys` → `create_license_key_batch`
- `license_key_stats` → `license_key_batch_stats`

### 新增

| 工具 | 参数 |
|------|------|
| `list_license_key_batches` | page, page_size |
| `get_license_key_batch` | batch_id |
| `create_license_key_batch` | name, source_tag, recipient_matcher, plan_days, quantity, expires_in_days, note |
| `license_key_batch_stats` | batch_id? (空=全局) |

### 简化

- `list_license_keys` — filter 从 campaignId 改为 batchId

## Admin UI

- **新页面**：`/manager/license-key-batches` — 批次 CRUD + 统计仪表盘
- **现有页面**：`/manager/license-keys` — 简化为纯列表浏览（filter 改为 batch_id）
- **活动码页面**：移除"发放授权码"相关 UI

## 数据迁移

1. 为现有带 `campaign_id` 的 keys 创建对应 batch 记录（name 取 campaign.name，source_tag = "campaign-legacy"）
2. 现有 `source="manual"` 的 keys 归入 "legacy-manual" batch
3. 回填 `license_keys.batch_id`
4. 验证无 `batch_id = 0` 的残留
5. 删除旧列（`campaign_id`, `source`, `recipient_matcher`, `created_by_user_id`）
6. Campaign 删除 `is_shareable`、`shares_per_user` 列

## 信心评估

**9/10** — 逻辑层面完整，唯一不确定性是迁移脚本需要实际跑一遍验证。
