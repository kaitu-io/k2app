# Feature: User Management UI

## Meta

| Field   | Value               |
|---------|---------------------|
| Feature | user-management-ui  |
| Version | v1                  |
| Status  | implemented         |
| Created | 2026-02-18          |
| Updated | 2026-02-18          |

## Version History

| Version | Date       | Summary                                                          |
|---------|------------|------------------------------------------------------------------|
| v1      | 2026-02-18 | Cold-start spec：文档化已实现的用户管理 UI 全貌 |

## Overview

用户管理 UI 是 k2app 的 Account 体系核心，涵盖设备管理、成员/团队管理、邀请系统、分销商体系、邮箱变更和密码设置六大模块。所有功能页面从 Account 页面入口导航，通过 `cloudApi` 与 Center API 交互，遵循 split globals 架构（`_k2` 负责 VPN 控制，`_platform` 负责平台能力）。

主要用户场景：
1. **设备管理** — 查看/删除/标注已登录设备，新设备安装引导
2. **成员管理** — 为家庭/团队添加子账户，购买时可选择代付对象
3. **邀请系统** — 生成邀请码、分享短链接（可配置有效期）、追踪注册和购买转化
4. **分销商体系** — 等级体系（L1-L4）、佣金比例、钱包余额、提现
5. **账户安全** — 邮箱变更（验证码两步流程）、密码设置/修改

## Product Requirements

### 1. 设备管理 (Devices)

**路由**: `/devices`
**入口**: Account 页面 → "我的设备"
**守卫**: `LoginRequiredGuard`

功能：
- 展示当前账户已登录的所有设备列表
- 每个设备显示：备注名（可编辑）、最后登录时间、当前设备标记 (Chip)
- 当前设备禁止删除（不显示删除按钮）
- 编辑备注：点击设备名进入编辑模式（inline TextField），支持 Enter 保存、Escape 取消、onBlur 保存
- 删除设备：二次确认 Dialog，确认后调 DELETE API 并刷新列表
- 空状态和加载状态有专用组件

API：
| 操作 | 方法 | 路径 |
|------|------|------|
| 列表 | GET | `/api/user/devices` |
| 删除 | DELETE | `/api/user/devices/:udid` |
| 改备注 | PUT | `/api/user/devices/:udid/remark` |

### 2. 新设备安装引导 (DeviceInstall)

**路由**: `/device-install`
**入口**: Account 页面 → "安装到其他设备"
**守卫**: 无（公开页面）
**Feature Flag**: `appConfig.features.deviceInstall`

功能：
- 从 `/api/app/config` 读取 `appLinks.baseURL + installPath` 拼接安装链接
- 展示 4 个平台图标（iOS / Android / Windows / macOS）
- 生成安装链接的 QR Code（`qrcode` 库，蓝色主题色）
- 一键复制安装链接（通过 `_platform.writeClipboard`）
- 底部展示多设备同步提示和客服联系方式
- 降级使用默认链接 `https://kaitu.io/install`

### 3. 成员管理 (MemberManagement)

**路由**: `/member-management`
**入口**: Account 页面 → "成员管理"
**守卫**: `LoginRequiredGuard`
**Feature Flag**: `appConfig.features.memberManagement`

功能：
- 展示当前用户的子账户成员列表
- 每个成员显示：邮箱、状态 Chip（有效/即将过期/已过期/未激活）、到期时间、UUID 前缀
- 成员状态计算逻辑：
  - `expiredAt` 为空 → "未激活" (default)
  - `expiredAt` 已过 → "已过期" (error)
  - 剩余 ≤7 天 → "N天后过期" (warning)
  - 其余 → "有效" (success)
- 添加成员：Dialog 输入邮箱，前端正则校验 + 后端 422 错误处理（邮箱已被使用时保持 Dialog 打开）
- 删除成员：点击删除图标直接调 API（无二次确认），乐观更新本地列表
- 顶部有说明 Alert 和刷新按钮
- 空状态引导"添加第一个成员"
- 加载失败显示重试按钮

API：
| 操作 | 方法 | 路径 |
|------|------|------|
| 列表 | GET | `/api/user/members` |
| 添加 | POST | `/api/user/members` |
| 删除 | DELETE | `/api/user/members/:uuid` |

### 4. 成员选择器 (MemberSelection)

**组件**: `MemberSelection`（嵌入 Purchase 页面）

功能：
- 购买套餐时选择"为谁充值"：自己 + 子账户成员多选
- Checkbox 多选，默认全选所有成员 + 自己
- 显示已选数量 badge
- 可在选择器内直接添加新成员（复用添加 Dialog 逻辑）
- SWR 模式：优先返回缓存数据（`cacheStore`，TTL 180s），后台静默刷新
- 未选择任何对象时显示 warning 提示

### 5. 邀请中心 (InviteHub)

**路由**: `/invite`（Tab 页面，keep-alive）
**Feature Flag**: `appConfig.features.invite`

功能：
- 加载"最新邀请码" GET `/api/invite/my-codes/latest`
- 显示邀请码统计：已注册人数 / 已购买人数（双色 Paper 卡片）
- 桌面端显示 QR Code（依赖短链接生成），移动端隐藏 QR Code
- 邀请码大字展示，点击可复制
- 备注编辑（inline Collapse 切换读/编辑模式）
- 三个操作按钮：
  - **分享完整内容**：弹出有效期选择 Popover → 生成包含奖励规则 + 下载链接 + 邀请码的文本 → 移动端优先 `navigator.share()`，桌面端复制到剪贴板
  - **复制分享链接**：弹出有效期选择 Popover → 复制短链接
  - **生成新邀请码**：POST API 创建新码
- 分销商用户显示 `RetailerStatsOverview`
- 普通用户显示 `InviteRule`（邀请规则说明）+ "成为分销商" CTA

### 6. 邀请码列表 (MyInviteCodeList)

**路由**: `/invite-codes`
**入口**: InviteHub → "查看全部"
**守卫**: `LoginRequiredGuard`

功能：
- 分页加载所有邀请码 GET `/api/invite/my-codes?page=0&pageSize=100`
- 卡片布局（移动端优先），每张卡片显示：
  - 邀请码（monospace 大写）
  - 统计：购买人数 / 注册人数
  - 备注（斜体显示）
  - 创建时间
  - 操作按钮组：分享（含有效期选择）、复制链接、编辑备注
- 编辑备注：Dialog 输入，调用 `useInviteCodeActions.updateRemark()`
- 空状态 / 加载状态有专用组件

### 7. 邀请码操作 Hook (useInviteCodeActions)

封装邀请码操作逻辑，供 InviteHub 和 MyInviteCodeList 复用：

| 方法 | 功能 |
|------|------|
| `shareInviteCode(code)` | 分享完整邀请内容（规则+链接+邀请码） |
| `shareInviteCodeWithExpiration(code, days)` | 带有效期的完整分享 |
| `copyShareLink(code)` | 复制短链接 |
| `copyShareLinkWithExpiration(code, days)` | 带有效期的短链接复制 |
| `copyInviteCode(code)` | 复制邀请码 |
| `updateRemark(code, remark)` | 更新邀请码备注 |

分享策略：
- 移动端 + `navigator.share` 可用 → 系统分享 Sheet
- 其余情况 → `_platform.writeClipboard` 复制到剪贴板
- 用户取消系统分享（AbortError）静默忽略

### 8. 分享链接 Hook (useShareLink)

管理邀请码短链接的获取和缓存：

- API: GET `/api/invite/my-codes/:code/share-link?expiresInDays=N`
- 缓存策略：`useRef` 保存 `{code}:{days}` → `{link, expiresAt}` 映射，缓存 1 小时
- 有效期选项：1 天 / 7 天（默认）/ 30 天 / 365 天
- 提供 `clearCache(code)` 和 `clearAllCache()` 方法

### 9. 有效期选择器 (ExpirationSelectorPopover)

通用 Popover 组件，用于分享链接有效期选择：

- 4 个 Radio 选项：1 天、7 天、30 天、365 天
- 默认选中 7 天
- 选择后立即触发回调并关闭
- 顶部安全提示 Alert

### 10. 分销商配置 (RetailerConfig)

展示分销商奖励配置卡片：
- 返现比例（百分比）
- 已获得奖励（天数，仅有奖励时显示）
- "详细规则" 按钮导航到 `/retailer-rule`
- 渐变背景（success + primary）

### 11. 分销商统计概览 (RetailerStatsOverview)

根据用户是否为分销商显示不同内容：

**非分销商**：CTA 卡片
- "成为分销商，赚取推广收益" 文案
- "了解分销商计划" 按钮 → 打开外部链接

**分销商**：战果概览卡片
- 等级信息：L1-L4 对应不同颜色（灰/蓝/紫/金），显示等级 Chip + 累计付费用户数
- 佣金比例：首单分成 % + 续费分成 %
- 升级进度：LinearProgress 进度条 + 距下一等级所需用户数/内容证明提示
- 最高等级达成提示
- 财务概览（3 列 Grid）：
  - 钱包余额（点击跳转钱包页面）
  - 累计返现
  - 待提现
- 金额显示：美分 → 美元转换 `(cents / 100).toFixed(2)`
- "查看与操作" 按钮 → 打开外部钱包页面

API：
| 操作 | 方法 | 路径 |
|------|------|------|
| 钱包 | GET | `/api/wallet` |
| 统计 | GET | `/api/retailer/stats` |

### 12. 提现对话框 (WithdrawDialog)

**组件**: `WithdrawDialog`（由外部钱包页面或分销商面板触发）

功能：
- 显示可用余额（Alert 提示）
- 选择提现账户（Select，支持多账户，默认选中 default 账户）
  - 每个选项显示：渠道类型 Chip + 币种 Chip + 地址（长地址截断）+ 默认标记
- 输入提现金额（带 `$` 前缀，支持"最大金额"快捷按钮）
- 最低提现 $10 验证
- 余额不足校验（前端 cents 比较）
- 用户备注（可选，多行输入）
- 提交：POST `/api/wallet/withdraws`

### 13. 密码设置 (PasswordDialog)

**组件**: `PasswordDialog`（Account 页面 Dialog）

功能：
- 设置/修改密码
- 密码规则验证（前端）：
  - 最少 8 字符
  - 必须包含字母
  - 必须包含数字
  - 两次输入一致
- 错误响应使用 `handleResponseError()` 统一处理
- API: POST `/api/user/password`

### 14. 邮箱变更 (UpdateLoginEmail)

**路由**: `/update-email`
**守卫**: `MembershipGuard`（需要付费会员）
**Feature Flag**: `appConfig.features.updateLoginEmail`

两步流程：
1. **发送验证码**：输入新邮箱 → POST `/api/user/email/send-bind-verification` → 60 秒倒计时
2. **确认绑定**：输入验证码 → POST `/api/user/email/update-email` → 成功后 `navigate("/account", { replace: true })`

UI 细节：
- 验证码输入框配置 `inputMode: "numeric"`, `autoComplete: "one-time-code"`
- 发送成功后显示"检查垃圾邮件箱"提示
- 倒计时期间发送按钮禁用

## Technical Decisions

### TD-1: 乐观更新 vs 刷新列表

| 页面 | 策略 | 原因 |
|------|------|------|
| MemberManagement 添加/删除 | 乐观更新本地 state | 成员列表变更频率低，避免额外网络请求 |
| Devices 删除/改备注 | 刷新整个列表 (`loadDevices()`) | 设备列表与后端同步更重要（涉及登录态） |
| MemberSelection | SWR 缓存 + 后台刷新 | 购买流程中减少等待，TTL 180s |
| MyInviteCodeList 改备注 | 乐观更新 + `updateRemark` hook | 备注变更简单，不需要刷新统计数据 |

### TD-2: Feature Flag 控制路由

所有非核心路由通过 `appConfig.features.*` 控制。`App.tsx` 中条件渲染 `<Route>`，未启用的 feature 对应路由完全不注册。当前 Kaitu 配置全部启用。

### TD-3: 分享策略分平台

移动端优先 `navigator.share()`（系统分享 Sheet，支持跨应用分享），桌面端/不支持时回退 `_platform.writeClipboard`。平台检测使用 `_platform.isMobile` + UA fallback。

### TD-4: 短链接有效期

分享链接通过后端生成短链接（`/api/invite/my-codes/:code/share-link`），支持 1/7/30/365 天有效期。前端缓存 1 小时（`useRef`），按 `{code}:{days}` 组合键缓存。

### TD-5: 分销商等级颜色体系

L1-L4 等级使用固定颜色映射，不依赖 MUI theme tokens：
- L1 (#9E9E9E 灰) — 推荐者
- L2 (#2196F3 蓝) — 分销商
- L3 (#9C27B0 紫) — 优质分销商
- L4 (#FF9800 金) — 合伙人

### TD-6: 金额处理

所有金额在 API 层使用美分（cents）传输。前端显示时 `(cents / 100).toFixed(2)` 转换为美元。提现金额输入为美元，提交前 `Math.round(amount * 100)` 转为美分。

### TD-7: 路由守卫分层

| 守卫 | 用于 | 行为 |
|------|------|------|
| `LoginRequiredGuard` | Devices, MyInviteCodeList, MemberManagement | 未登录时弹出 LoginDialog |
| `MembershipGuard` | UpdateLoginEmail | 需要付费会员身份 |
| 无守卫 | DeviceInstall | 公开页面 |

### TD-8: 延迟聚焦 (delayedFocus)

所有 Dialog/编辑模式的 TextField 使用 `delayedFocus()` 工具函数（100-150ms 延迟），避免在旧版 WebView 中因 DOM 动画/渲染时序导致的聚焦失败。

## Key Files

### Pages

| File | Description |
|------|-------------|
| `webapp/src/pages/Devices.tsx` | 设备列表、删除、编辑备注 |
| `webapp/src/pages/DeviceInstall.tsx` | 新设备安装引导（QR Code + 链接复制） |
| `webapp/src/pages/MemberManagement.tsx` | 子账户成员增删管理 |
| `webapp/src/pages/InviteHub.tsx` | 邀请中心（Tab 页，邀请码 + 统计 + 分享 + 分销商） |
| `webapp/src/pages/MyInviteCodeList.tsx` | 全部邀请码列表（CRUD + 分享） |
| `webapp/src/pages/UpdateLoginEmail.tsx` | 邮箱变更（验证码两步流程） |
| `webapp/src/pages/Account.tsx` | 账户主页（所有管理功能入口） |

### Components

| File | Description |
|------|-------------|
| `webapp/src/components/WithdrawDialog.tsx` | 钱包提现对话框 |
| `webapp/src/components/PasswordDialog.tsx` | 密码设置/修改对话框 |
| `webapp/src/components/RetailerConfig.tsx` | 分销商奖励配置卡片 |
| `webapp/src/components/RetailerStatsOverview.tsx` | 分销商统计概览 / 非分销商 CTA |
| `webapp/src/components/MemberSelection.tsx` | 购买流程成员选择器（多选） |
| `webapp/src/components/ExpirationSelectorPopover.tsx` | 链接有效期选择 Popover |

### Hooks

| File | Description |
|------|-------------|
| `webapp/src/hooks/useInviteCodeActions.ts` | 邀请码操作封装（分享/复制/备注更新） |
| `webapp/src/hooks/useShareLink.ts` | 分享短链接获取与缓存 |

### Types & Config

| File | Description |
|------|-------------|
| `webapp/src/services/api-types.ts` | 所有类型定义：Device, DataUser, MyInviteCode, Wallet, WithdrawAccount, Withdraw, RetailerStats 等 |
| `webapp/src/config/apps.ts` | Feature flags 控制路由可见性 |
| `webapp/src/App.tsx` | 路由注册与守卫配置 |

### API Endpoints Summary

| Endpoint | Method | 模块 |
|----------|--------|------|
| `/api/user/devices` | GET | 设备管理 |
| `/api/user/devices/:udid` | DELETE | 设备管理 |
| `/api/user/devices/:udid/remark` | PUT | 设备管理 |
| `/api/app/config` | GET | 安装引导 |
| `/api/user/members` | GET | 成员管理 |
| `/api/user/members` | POST | 成员管理 |
| `/api/user/members/:uuid` | DELETE | 成员管理 |
| `/api/invite/my-codes/latest` | GET | 邀请系统 |
| `/api/invite/my-codes` | GET/POST | 邀请系统 |
| `/api/invite/my-codes/:code/remark` | PUT | 邀请系统 |
| `/api/invite/my-codes/:code/share-link` | GET | 邀请系统 |
| `/api/user/email/send-bind-verification` | POST | 邮箱变更 |
| `/api/user/email/update-email` | POST | 邮箱变更 |
| `/api/user/password` | POST | 密码管理 |
| `/api/wallet` | GET | 分销商/钱包 |
| `/api/wallet/withdraws` | POST | 提现 |
| `/api/retailer/stats` | GET | 分销商 |

## Acceptance Criteria

### 设备管理
- [x] 设备列表正确显示所有已登录设备
- [x] 当前设备标记 Chip 显示，且禁止删除
- [x] 编辑备注支持 inline 编辑（Enter 保存、Escape 取消、blur 保存）
- [x] 删除设备有二次确认 Dialog
- [x] 空状态和加载状态正确展示

### 安装引导
- [x] QR Code 使用后端配置的安装链接生成
- [x] 降级到默认链接 `https://kaitu.io/install`
- [x] 复制链接通过 `_platform.writeClipboard` 跨平台工作
- [x] 展示 4 个平台图标

### 成员管理
- [x] 成员列表显示邮箱、状态、到期时间
- [x] 成员状态 Chip 颜色正确（有效=绿、即将过期=黄、已过期=红、未激活=灰）
- [x] 添加成员邮箱前端正则校验
- [x] 422 错误（邮箱已使用）保持 Dialog 打开
- [x] 添加/删除后乐观更新列表

### 邀请系统
- [x] 最新邀请码正确加载并展示
- [x] QR Code 依赖短链接生成（非直接 URL）
- [x] 桌面端显示 QR Code，移动端隐藏
- [x] 分享内容包含奖励规则 + 下载链接 + 邀请码
- [x] 移动端优先系统分享，桌面端复制到剪贴板
- [x] 有效期 Popover 提供 1/7/30/365 天选项
- [x] 短链接缓存 1 小时，按 code+days 组合键
- [x] 邀请码列表分页加载，卡片布局
- [x] 邀请码备注可编辑

### 分销商体系
- [x] 非分销商显示 CTA 卡片引导加入
- [x] 分销商显示等级（L1-L4）+ 颜色映射
- [x] 显示首单/续费佣金比例
- [x] 升级进度条 + 所需条件提示
- [x] 钱包余额/累计返现/待提现金额显示（cents→dollars）
- [x] 点击财务指标跳转外部钱包页面

### 提现
- [x] 自动选中默认提现账户
- [x] 最低 $10 提现验证
- [x] 余额不足前端校验
- [x] 金额 dollars→cents 转换正确（`Math.round`）
- [x] 长地址截断显示

### 邮箱变更
- [x] 两步流程：发送验证码 → 确认绑定
- [x] 60 秒倒计时防止重复发送
- [x] 验证码输入配置 numeric 键盘和 one-time-code autocomplete
- [x] 成功后导航回 Account 页面（replace）
- [x] MembershipGuard 保护（非付费用户不可访问）

### 密码管理
- [x] 密码规则：≥8 字符 + 包含字母 + 包含数字
- [x] 两次密码一致校验
- [x] 错误通过 `handleResponseError()` 统一处理
- [x] 提交成功后关闭 Dialog 并回调 onSuccess
