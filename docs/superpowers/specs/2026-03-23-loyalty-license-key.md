# 老用户回馈 + 老带新活动系统 — 设计规格

**日期**: 2026-03-23
**状态**: 已审核

---

## 业务目标

1. **老用户回馈**：对"在某日期之前首次付款"的活跃老用户，发放专属折扣码供自用
2. **老带新**：同一批老用户获得可分享的一次性授权码，专供从未付款的新用户使用
3. **营销效果**：以"送礼"框架驱动分享，提升新用户落地转化率

---

## 核心设计决策

### 两套独立系统协同工作

```
Campaign（活动规则）                LicenseKey（授权码实例）
─────────────────────────          ────────────────────────────
定义折扣规则、目标人群              独立表，软关联 campaign_id
新增 paid_before matcher           单次使用，UUID 唯一码
新增 plan_active matcher           有过期时间（30天）
老用户自用折扣码（已有逻辑）        老用户分享给新用户
```

Campaign 保持"广播码"语义（公开码，用户主动输入），LicenseKey 是"定向码"（私有UUID，系统发放给个人）。

---

## 数据模型

### Campaign 扩展字段

```go
// 新增字段（在现有 Campaign struct 上）
MatcherParams string `gorm:"type:varchar(500)" json:"matcherParams"`
// JSON: {"beforeDate": 1735689600, "requireActivePlan": true}
// beforeDate: 首次付款截止 Unix 时间戳
// requireActivePlan: 是否要求当前套餐仍有效

IsShareable   bool  `gorm:"default:false" json:"isShareable"`
// true = 此活动配套发放 LicenseKey 批次

SharesPerUser int64 `gorm:"default:0" json:"sharesPerUser"`
// 每个老用户获得的可分享码数量（IsShareable=true 时有效）
```

### 新增 LicenseKey 表

```go
type LicenseKey struct {
    ID              uint64         `gorm:"primarykey"`
    CreatedAt       time.Time
    UpdatedAt       time.Time
    DeletedAt       gorm.DeletedAt `gorm:"index"`

    UUID            string         `gorm:"type:varchar(50);uniqueIndex;not null"`
    // 分享链接: /redeem/{uuid}

    // 折扣信息（独立存储，不依赖 Campaign）
    DiscountType    string         `gorm:"type:varchar(20);not null"`
    // "discount"（百分比）或 "coupon"（固定金额，分）
    DiscountValue   uint64         `gorm:"not null"`
    // discount: 80 = 打8折；coupon: 500 = 减5元

    // 目标限制
    RecipientMatcher string        `gorm:"type:varchar(50);not null"`
    // "never_paid"（从未付款）或 "all"
    ExpiresAt       int64          `gorm:"not null"`
    // Unix 时间戳，过期后不可用

    // 发放溯源（可空）
    CampaignID      *uint64        `gorm:"index"`
    // 软关联 Campaign，仅用于统计，非强依赖

    // 使用追踪
    CreatedByUserID *uint64        `gorm:"index"`
    // 老用户 ID（分享者）

    IsUsed          bool           `gorm:"default:false"`
    UsedByUserID    *uint64        `gorm:"index"`
    UsedAt          *time.Time
}
```

---

## 新增 Matcher 类型

| matcherType | matcherParams 字段 | 判断逻辑 |
|---|---|---|
| `paid_before` | `{"beforeDate": unix}` | 用户首次付款时间 < beforeDate |
| `paid_before_active` | `{"beforeDate": unix}` | 同上 + 当前套餐有效 |
| `all` / `first_order` / `vip` | 无变化 | 现有逻辑不变 |

判断"首次付款时间"：查 `orders` 表，`user_id = ? AND status = 'paid'`，取最早一条的 `created_at`。

---

## API 设计

### 管理端新增接口

| Method | Endpoint | 说明 |
|---|---|---|
| POST | `/app/campaigns/:id/issue-keys` | 触发批量生成 LicenseKey 并发送 |
| GET | `/app/license-keys` | 列出所有 LicenseKey（分页、过滤） |
| GET | `/app/license-keys/stats` | 统计（总量/已用/过期/活动ID分组） |
| DELETE | `/app/license-keys/:id` | 作废单个码 |

### 用户端新增接口

| Method | Endpoint | 说明 |
|---|---|---|
| GET | `/api/license-keys/:uuid` | 获取码详情（落地页用，不需登录） |
| POST | `/api/license-keys/:uuid/preview` | 预览折扣（登录后，结账前确认） |

### 下单接口扩展

`POST /api/orders` 的 `campaignCode` 字段兼容 LicenseKey UUID：
- 以 `-` 判断是否为 UUID 格式（xid 格式无连字符，可用长度区分）
- 走独立的 `matchLicenseKey` 验证路径

---

## 分发流程

```
管理员点击"发放授权码"
    ↓
后台 worker（异步）
    ↓
查询符合条件老用户：
  SELECT users WHERE first_paid_at < beforeDate [AND pro_expires_at > now]
    ↓
批量生成 LicenseKey（每人 sharesPerUser 个）
  UUID: xid.New().String()
  ExpiresAt: now + 30天
  CampaignID: 活动 ID
  CreatedByUserID: 老用户 ID
    ↓
发送邮件（每人一封，含所有码的分享链接）
  主题: "你有 N 个专属礼物名额可以送给朋友"
  内容: 每个链接 + 折扣说明 + 过期时间
```

---

## 落地页 `/redeem/{uuid}`

### 状态机

| Key 状态 | 页面展示 |
|---|---|
| 有效 + 未使用 | 礼物卡片：发送人 / 折扣 / 倒计时 / [立即领取] |
| 已过期 | "此礼物已过期" + 普通新用户优惠兜底入口 |
| 已使用 | "此礼物已被领走" + 普通新用户优惠兜底入口 |
| 不存在 | 404 → 首页 |

### 转化路径

```
落地页（展示折扣 + 发送人）
    ↓ [立即领取]
注册/登录（邮箱，30秒）
    ↓ 自动带入 uuid 参数
购买页（折扣预应用，倒计时显示）
    ↓ 下单
POST /api/orders { campaignCode: uuid }
    ↓ 验证通过
LicenseKey.IsUsed = true，记录 UsedByUserID
```

---

## 防滥用规则

1. **手机号验证**：使用 LicenseKey 下单前，`never_paid` 类型要求绑定手机号
2. **同 IP 限制**：同一 IP 24 小时内最多使用 3 个不同 LicenseKey
3. **同账号限制**：同一用户只能使用 1 个 LicenseKey（UsedByUserID 唯一性检查）
4. **原子消费**：`UPDATE license_keys SET is_used=true WHERE uuid=? AND is_used=false`，防并发竞争

---

## 文案框架

- 邮件标题：**"你有 3 个专属礼物名额可以送给朋友"**（不说"折扣码"）
- 落地页标题：**"[发送人昵称] 送给你一个礼物"**
- CTA：**"立即领取"**（不说"使用折扣"）
- 紧迫感：**"还剩 X 天到期"**

---

## 国际化

新增 i18n key（zh-CN 优先）：
- `licenseKeys.gift.title` — "{name} 送给你一个礼物"
- `licenseKeys.gift.expires` — "还剩 {days} 天到期"
- `licenseKeys.gift.used` — "此礼物已被领走"
- `licenseKeys.gift.expired` — "此礼物已过期"
- `licenseKeys.gift.fallback` — "查看新用户专属优惠"
- `campaigns.matchers.paid_before` — "X日期前首次付款的用户"
- `campaigns.matchers.paid_before_active` — "X日期前首次付款且套餐有效的用户"

---

## 不在本期范围内

- 老用户"已发放 / 已使用"的个人码管理页（App 内查看）
- Push 通知渠道（本期只做邮件）
- LicenseKey 用于免费计划兑换（非折扣场景）
- 分享率/转化率实时 Dashboard
