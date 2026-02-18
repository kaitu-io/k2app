# Feature: Support & Issues System

## Meta

| Field     | Value              |
|-----------|--------------------|
| Feature   | support-issues     |
| Version   | v1                 |
| Status    | implemented        |
| Created   | 2026-02-18         |
| Updated   | 2026-02-18         |

## Overview

k2app 的用户支持与问题反馈系统，覆盖三个核心场景：社区问题浏览（GitHub Issues 代理）、工单提交（邮件通知）、浮动反馈入口。整体架构：

1. **GitHub Issues 社区面板** — 用户可浏览其他人提交的问题和官方回复，支持 open/closed 筛选、分页、评论回复。后端通过 `qtoolkit/github/issue` 库代理 GitHub API，Center API 在中间层注入用户身份（`X-App-User-ID`），实现匿名化的社区交互。
2. **工单提交** — 登录会员通过表单提交工单（subject + content），Center API 将工单以邮件形式发送到 support@kaitu.me，抄送用户注册邮箱，同时发送 Slack 通知到 #customer 频道。不自动创建 GitHub Issue（由工程师人工审核后脱敏创建）。
3. **Feedback FAB** — 全局浮动反馈按钮，固定在左上角，带 pulse 橙色动画吸引注意力。点击导航到 `/submit-ticket?feedback=true`，进入反馈模式时自动静默上传服务日志（`_platform.uploadServiceLogs`），feedbackId 关联日志与工单。

导航路径：Account -> FAQ（帮助中心入口页）-> Issues / SubmitTicket。FAQ 页面作为三个支持功能的 hub：安全软件白名单帮助（外链）、社区反馈（Issues）、联系客服（SubmitTicket）。

## Product Requirements

- PR1: 用户可在 Issues 列表页浏览所有社区问题，每条显示 open/closed 状态徽章、标题、评论数、相对时间、是否有官方回复徽章
- PR2: Issue 列表支持分页加载（每页 20 条，"加载更多"按钮），空状态有友好提示
- PR3: 用户点击 Issue 可进入详情页，查看完整问题描述和评论线程
- PR4: 评论区域区分官方回复（左侧蓝色 border + "官方回复"徽章）和普通用户回复
- PR5: 登录用户可在 Issue 详情页添加评论回复
- PR6: Issues 列表和详情页均需要登录（LoginRequiredGuard），submit-ticket 页需要有效会员（MembershipGuard）
- PR7: 登录会员可通过 SubmitTicket 提交工单，包含标题（最长 200 字符）和描述（最长 5000 字符）
- PR8: 工单提交成功后显示成功页面（绿色卡片 + 邮箱提示），引导用户查收邮件
- PR9: 工单提交时自动静默上传客户端日志（feedbackId 关联），不阻塞用户操作，上传失败也不影响工单提交
- PR10: 全局 FeedbackButton 浮动按钮提供快捷反馈入口，pulse 动画吸引用户注意力
- PR11: FAQ 页面作为支持功能 hub，聚合三个入口：安全软件白名单帮助（外链）、社区反馈（Issues）、联系客服（SubmitTicket）
- PR12: 整个支持模块受 `features.feedback` feature flag 控制，可全局关闭

## Technical Decisions

### TD1: GitHub Issues 作为社区反馈后端

**决策**: 使用 GitHub Issues 作为社区问题的存储和管理后端，通过 Center API 代理访问。

**原因**:
- 无需自建工单数据库和管理后台
- GitHub Issues 天然支持 open/closed 状态、标签、评论线程
- 工程师可在 GitHub 直接回复，自动同步到用户端
- `qtoolkit/github/issue` 库封装了 GitHub API 的复杂性，Center 只需注册路由即可

**实现**:
- Center API 在 `/api/issues` group 上挂载 `issue.RegisterRoutes()`
- 中间件 `setAppUserIDHeader()` 将已认证的 `userID` 注入 `X-App-User-ID` header
- qtoolkit 库负责：列表查询（分页）、详情查询（含评论）、创建评论、官方标记（`is_official` / `has_official`）
- GitHub repo 配置通过 `viper.GetString("github.owner")` / `github.repo` 读取

### TD2: 工单走邮件而非 GitHub Issue

**决策**: 用户提交的工单通过邮件发送到支持邮箱，不自动创建 GitHub Issue。

**原因**:
- 工单可能包含用户隐私信息（邮箱、账号状态、使用场景等）
- GitHub Issue 是公开可见的，直接创建会泄露隐私
- 由工程师人工审核工单内容后，脱敏创建 GitHub Issue 公开讨论

**实现**:
- `api_create_ticket` handler 获取用户邮箱 -> 构建邮件 -> `mail.Send()`
- 邮件发送到 `support@kaitu.me`（可配置），ReplyTo 和 CC 设为用户邮箱
- 邮件主题加 `[Ticket]` 前缀标识
- 异步发送 Slack 通知到 #customer 频道（`SafeGoWithContext` + `slack.Send`），不阻塞响应
- 如有 feedbackId 则在邮件和 Slack 消息中附带，方便查找对应日志

### TD3: 静默日志上传 + feedbackId 关联

**决策**: 进入 SubmitTicket 页面时自动静默上传客户端日志，通过 UUID feedbackId 将日志与工单关联。

**原因**:
- 用户报告问题时往往不知道如何收集日志
- 静默上传减少用户操作步骤，提升工单质量
- feedbackId 让支持人员能快速找到对应的日志文件

**实现**:
- 进入页面立即检查 `window._platform?.uploadServiceLogs` 是否可用
- 生成 UUID v4 作为 feedbackId（`generateFeedbackId()`）
- 调用 `_platform.uploadServiceLogs({ email, reason: 'user_feedback_report', platform, version, feedbackId })`
- 上传状态（idle/uploading/success/error）仅内部跟踪，不显示 UI
- 提交工单时，若日志上传成功则附带 feedbackId 到请求体
- `uploadAttemptedRef` 保证每次进入页面只上传一次

### TD4: FeedbackButton pulse 动画

**决策**: 使用 MUI `Fab` + CSS keyframes 实现永久 pulse 动画的浮动反馈按钮。

**原因**:
- 反馈入口需要醒目但不遮挡主要操作区域
- 固定在左上角（left:8, top:84）避开底部导航和右侧操作区
- 橙色 glow pulse 动画在深色主题下视觉突出

**实现**:
- `@keyframes pulse`: 0% → 70% → 100% 的 box-shadow 渐变（`rgba(255, 152, 0, 0.7)` → transparent）
- hover 时停止动画（`animation: 'none'`）+ scale(1.1) 放大效果
- 点击导航到 `/submit-ticket?feedback=true`，feedback query param 触发反馈模式
- 反馈模式下标题和描述文案不同（`feedbackTitle` / `feedbackDescription`），提示日志正在上传

### TD5: 路由守卫分层

**决策**: Issues 页面用 `LoginRequiredGuard`（要求登录），SubmitTicket 用 `MembershipGuard`（要求有效会员）。

**原因**:
- 社区问题浏览属于公共内容，但需要登录才能追踪用户身份（评论需要 userID）
- 提交工单需要获取用户邮箱发送邮件，且涉及日志上传等敏感操作，限制为有效会员
- MembershipGuard 会将过期用户重定向到 `/purchase`

**路由配置**:
```tsx
<Route path="faq" element={<FAQ />} />
<Route path="issues" element={<LoginRequiredGuard><Issues /></LoginRequiredGuard>} />
<Route path="issues/:number" element={<LoginRequiredGuard><IssueDetail /></LoginRequiredGuard>} />
<Route path="submit-ticket" element={<MembershipGuard><SubmitTicket /></MembershipGuard>} />
```

### TD6: Feature Flag 控制

**决策**: 整个支持模块受 `appConfig.features.feedback` flag 控制。

**实现**:
- App.tsx 中 FAQ/Issues/IssueDetail/SubmitTicket 四条路由包裹在 `{appConfig.features.feedback && (<>...</>)}` 中
- FeedbackButton 在 Layout.tsx 中全局渲染（始终显示）
- `webapp/src/config/apps.ts` 中 `feedback: true` 默认开启

## Key Files

### Frontend

| File | Purpose |
|------|---------|
| `webapp/src/pages/FAQ.tsx` | 帮助中心 hub 页面，聚合三个支持入口（安全软件帮助、社区反馈、联系客服） |
| `webapp/src/pages/Issues.tsx` | Issue 列表页，分页加载，显示 open/closed 状态徽章、评论数、官方回复标记 |
| `webapp/src/pages/IssueDetail.tsx` | Issue 详情页，展示问题描述 + 评论线程 + 评论表单，官方回复有左侧蓝色 border |
| `webapp/src/pages/SubmitTicket.tsx` | 工单提交页，subject + content 表单，静默日志上传，成功后显示邮箱提示 |
| `webapp/src/components/FeedbackButton.tsx` | 全局浮动反馈按钮，pulse 动画，导航到 submit-ticket?feedback=true |
| `webapp/src/components/StarRating.tsx` | 星级评分组件（用于路线诊断展示，非本功能核心） |
| `webapp/src/components/MembershipGuard.tsx` | 会员守卫，过期用户重定向到 /purchase |
| `webapp/src/services/api-types.ts` | TypeScript 类型定义：GitHubIssue、GitHubComment、GitHubIssueDetail、GitHubIssuesListResponse、CreateTicketRequest |
| `webapp/src/i18n/locales/zh-CN/ticket.json` | ticket 和 issues 相关 i18n 文案（中文） |
| `webapp/src/i18n/locales/zh-CN/feedback.json` | FeedbackButton 相关 i18n 文案（中文） |

### Backend (Center API)

| File | Purpose |
|------|---------|
| `api/route.go` | 路由注册：`/api/issues` group（qtoolkit 代理）、`/api/user/ticket`（工单创建） |
| `api/api_issue.go` | `setAppUserIDHeader()` 中间件，将 userID 注入 X-App-User-ID header |
| `api/api_ticket.go` | `api_create_ticket` handler：验证 -> 获取邮箱 -> 发邮件 -> Slack 通知 |
| `api/type.go` | `CreateTicketRequest` 结构体定义（subject/content/feedbackId） |

### 外部依赖

| Package | Purpose |
|---------|---------|
| `qtoolkit/github/issue` | GitHub Issues API 代理库，`RegisterRoutes()` 注册 CRUD 路由 |
| `qtoolkit/mail` | 邮件发送 |
| `qtoolkit/slack` | Slack 通知 |

## API Endpoints

### GitHub Issues（qtoolkit 代理）

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/issues?page=1&per_page=20` | AuthRequired | 获取 Issue 列表，返回 `GitHubIssuesListResponse` |
| GET | `/api/issues/:number` | AuthRequired | 获取 Issue 详情（含评论），返回 `GitHubIssueDetail` |
| POST | `/api/issues/:number/comments` | AuthRequired | 添加评论，body: `{ body: string }` |

### 工单

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| POST | `/api/user/ticket` | AuthRequired | 提交工单，body: `{ subject, content, feedbackId? }` |

## Data Types

### GitHubIssue（列表项）

```typescript
interface GitHubIssue {
  number: number;        // Issue 编号
  title: string;         // 标题
  body: string;          // 正文
  state: string;         // "open" | "closed"
  labels: string[];      // 标签列表
  has_official: boolean; // 是否有官方回复
  comment_count: number; // 评论数
  created_at: string;    // ISO 日期
  updated_at: string;
}
```

### GitHubComment（评论）

```typescript
interface GitHubComment {
  id: number;
  body: string;
  is_official: boolean;  // 是否官方回复
  created_at: string;
}
```

### CreateTicketRequest（后端）

```go
type CreateTicketRequest struct {
  Subject    string `json:"subject" binding:"required,min=1,max=200"`
  Content    string `json:"content" binding:"required,min=1,max=5000"`
  FeedbackID string `json:"feedbackId,omitempty"`
}
```

## Acceptance Criteria

- AC1: 用户从 Account -> FAQ 进入帮助中心，看到三个入口卡片
- AC2: 点击"社区反馈"进入 Issues 列表，未登录时弹出 LoginDialog
- AC3: Issues 列表正确显示 open（黄色警告色）/ closed（绿色成功色）状态徽章
- AC4: 有官方回复的 Issue 显示蓝色"官方回复"徽章
- AC5: 列表滚动到底部可点击"加载更多"分页加载
- AC6: 点击 Issue 进入详情页，显示完整描述和评论线程
- AC7: 官方评论有左侧蓝色 border line 和"官方回复"小徽章
- AC8: 登录用户可在详情页输入评论并提交，新评论实时追加到列表
- AC9: 点击"联系客服"进入 SubmitTicket，过期会员被重定向到 /purchase
- AC10: 工单表单验证：标题必填（<=200 字符），描述必填（<=5000 字符）
- AC11: 工单提交成功后显示绿色成功卡片，提示用户查收邮件
- AC12: 支持邮箱收到工单邮件，包含 [Ticket] 前缀、用户邮箱、工单内容
- AC13: 工单邮件 CC 到用户注册邮箱，ReplyTo 设为用户邮箱
- AC14: Slack #customer 频道收到工单通知
- AC15: 反馈模式（feedback=true）下，页面标题和描述切换为反馈专用文案
- AC16: 进入 SubmitTicket 页面时自动静默上传日志，feedbackId 关联到工单
- AC17: FeedbackButton 在全局 Layout 中显示，带 pulse 橙色动画
- AC18: FeedbackButton 点击导航到 /submit-ticket?feedback=true
- AC19: 所有错误状态有 retry 按钮，网络异常有友好提示
- AC20: `features.feedback` 设为 false 时，FAQ/Issues/SubmitTicket 路由不渲染
