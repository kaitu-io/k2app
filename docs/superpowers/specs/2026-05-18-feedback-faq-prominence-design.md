# Feedback 页 FAQ 凸显改造

**日期**：2026-05-18
**作者**：David
**状态**：设计阶段，待 review

## 背景

`/feedback` 页（`webapp/src/pages/Feedback.tsx`）目前是「我的工单列表 + 顶部一对小按钮（FAQ outlined + 新建工单 contained）」的布局。FAQ 入口在右上角的次要位置，多数用户直接点「提交新工单」，工单量里大量是 FAQ 已能解答的高频问题（速度慢、节点选择、特定 App 失效、老品牌登录等）。

## 目标

把 FAQ 推到 `/feedback` 页的视觉首要位置，让用户在打开工单前先自助筛一遍。**不** 强制 funnel —— 已知道要建工单的重度用户仍有顶部快速通道。

## 范围

仅改造 `/feedback` 的展示与文案。不动 `/faq` 路由（`SideNavigation` 和 `ServiceError` 还在引用）、不动 `/submit-ticket-form`、不动后端、不动错误码。

## 设计决策

### 1. 布局（自上而下）

```
┌──────────────────────────────────────────────┐
│ 反馈中心                       [+ 提交工单]   │  header
├──────────────────────────────────────────────┤
│ 常见问题                                       │  FAQ section (始终置顶)
│ 遇到问题时先看这里，大部分情况都能快速解决       │
│ ▸ 连接不稳定 / 速度慢怎么办？                  │
│ ▸ 装 Kaitu 后某个软件 / 游戏用不了怎么办？     │  ← 新增
│ ▸ 节点列表那么多，我应该选哪个？                │  ← 新增
│ ▸ 切换 WiFi 或网络后连接断了？                 │
│ ▸ 为什么我的设备被移除 / 踢下线？               │
│ ▸ 更新后无法使用？                            │
│ ▸ 登录失败 / 收不到验证码？                    │
│ ▸ 我之前用的是 AllNationConnect，能在 Kaitu 登录吗？│  ← 新增
│ ▸ 为什么 Kaitu 能在中国区 App Store 上架？     │
├──────────────────────────────────────────────┤
│ 我的工单                                       │  ticket section
│ [处理中] #123 客服回复于 2小时前               │   (无工单时：标题 + 一行小字「暂无工单」)
│ [已解决] #120 ...                            │
├──────────────────────────────────────────────┤
│ 没找到答案？                                   │  底部 CTA
│ [提交工单]                                    │
└──────────────────────────────────────────────┘
```

### 2. 顺序固定

无论用户是否有工单，FAQ 永远在最上、工单列表在 FAQ 之下、底部 CTA 永远在最后。这优先让用户先自助，对常回访看工单状态的用户略不友好，但顶部 header 的「+ 提交工单」按钮可作为快速 escape hatch；多数有工单用户通常从邮件/推送进入工单详情页，不一定走 `/feedback` 列表入口。

### 3. FAQ 显示形态

- 全部 9 条 inline accordion，默认全部折叠
- 用户在 Feedback 页内直接展开 / 收起，不跳转 `/faq`
- 视觉与 FAQ.tsx 一致（同样的 `bgcolor: background.paper`、`borderRadius: 1`）

### 4. 两个「提交工单」按钮区分语境

- **顶部 header 按钮**：图标 + 文字 `+ 提交工单`，`size="small"` `variant="outlined"`，给已经知道要建工单的用户的快捷通道
- **底部 CTA**：上方一行小字「没找到答案？」+ `variant="contained"` 大按钮「提交工单」，作为「FAQ 浏览完未解决」的自然终点

### 5. 工单详情视图保持现状

`selectedTicketId !== null` 时整页替换为 `TicketDetailView`，**不** 渲染 FAQ section —— 用户在跟客服对话时 FAQ 是干扰。当前已是这个行为，重构后保持。

### 6. 「我的工单」无工单空状态

不再渲染当前那张「暂无工单 / 遇到问题？提交一个工单…」的大卡片（FAQ 已经撑起视觉重心）。改为：

- 标题「我的工单」照常显示
- 标题下一行小字「暂无工单」（`variant="caption"` `color="text.secondary"`）
- 不重复显示「提交工单」按钮（底部 CTA 已经有）

加载中 / 错误状态仍按现有逻辑显示在「我的工单」section 内。

### 7. 错误状态保留

`fetchTickets` 失败时仍在「我的工单」section 内显示当前的 Alert + Retry 按钮。FAQ 不受影响，仍立即可见 —— FAQ 是本地静态内容、无 loading。

## 新增 FAQ 内容（zh-CN 初稿）

放在 `ticket:faq.items.<key>` 下。其他 6 个 locale 在实现时翻译。

### `appNotWorking` — 装 Kaitu 后某个软件 / 游戏用不了怎么办？

> 部分软件（特别是 Adobe Creative Cloud、一些游戏、第三方 IM 工具）会缓存自己的服务器地址。开启 Kaitu 后这些缓存地址被路由到代理，可能被对端拒绝或超时。按以下顺序尝试：
>
> 1. **重启那个软件**：多数情况退出再打开即可恢复，让软件刷新自己的服务器缓存。
> 2. **切换分流模式**：在 Kaitu 里把"智能分流 ↔ 全局代理"对调一次，再试该软件。
> 3. **临时断开 Kaitu**：极少数案例（例如 Adobe Creative Cloud 缓存了已停服的旧 IP），用该软件时先断开 Kaitu，用完再连。
> 4. 仍不行请提交工单附上日志，我们的工程师会跟进分析。

### `nodeChoice` — 节点列表那么多，我应该选哪个？

> **最省心的做法：用「自动选择」。** 在节点列表顶部点「自动选择」，Kaitu 会根据实时延迟和负载帮你挑最优节点；开启「自动切换」后，网络状况变化时还会自动换到更好的节点。这是大多数用户的最佳选择。
>
> 如果你坚持手动选：
>
> - 不要总点列表第一个 —— 列表前几个节点（特别是 AU 澳大利亚）经常被打爆，因为大部分用户都会下意识点第一个。
> - 优先尝试 **JP / SG / KR / US**，通常更稳更快。
> - 连不上或慢时**换一个再试**就行 —— 不同地区访问哪个节点最优会随时间浮动。
> - 看视频、刷网页对节点选择不敏感；玩游戏或视频会议对延迟敏感，多换几个找最稳的那个。

### `allNationConnect` — 我之前用的是 AllNationConnect，能在 Kaitu 登录吗？

> 可以。AllNationConnect 是我们 Kaitu 的早期客户端，已经停止维护（不再更新协议和节点）。请：
>
> 1. 在 App Store / Google Play 搜索 **"Kaitu"** 或 **"开途"**，或访问 [kaitu.io](https://kaitu.io) 下载新版客户端。
> 2. 用原来的邮箱登录，订阅会自动同步保留，无需重新购买。
> 3. 如果原客户端遇到稳定性问题，直接升级到新版，老版本不再修复。

## 删除 `linuxSupport` FAQ item

低频问题，使用率不足以占据列表位置。删除 `ticket:faq.items.linuxSupport.question` 和 `ticket:faq.items.linuxSupport.answer` 在全部 7 个 locale 的 key（不留兼容桥）。`FAQ.tsx` 的 `FAQ_KEYS` 数组同步移除。

## 最终 FAQ 顺序（9 条）

`Feedback.tsx` 和 `FAQ.tsx` 共享同一份顺序常量。建议提取到一个共享模块（见下文）。

```ts
const FAQ_KEYS = [
  "connection",          // 1. 速度慢/不稳定 (已有)
  "appNotWorking",       // 2. 某软件用不了 (新增)
  "nodeChoice",          // 3. 该选哪个节点 (新增)
  "wifiSwitch",          // 4. WiFi 切换 (已有)
  "deviceRemoved",       // 5. 设备移除 (已有)
  "updateIssue",         // 6. 更新后 (已有)
  "loginFailed",         // 7. 验证码 (已有)
  "allNationConnect",    // 8. 老用户登录 (新增)
  "chinaAppStore",       // 9. 中国区上架 (已有)
] as const;
```

## 涉及改动

### Feedback.tsx（主要改造）

1. 新增 `FaqSection` 子组件 —— 渲染 accordion 列表，无自己的 BackButton / CTA（这些由 Feedback 页统一管理）
2. `TicketList` 简化：
   - 顶部 header 改为「反馈中心」标题 + 右上角小按钮「+ 提交工单」（`variant="outlined"`, `size="small"`），删除原 FAQ 跳转按钮（FAQ 现在内嵌）
   - 删除 `tickets.length === 0` 那张大卡片，替换为「标题 + 一行小字」
3. `Feedback` 主组件重组：列表视图按 FaqSection → TicketList → 底部「没找到？提交工单」CTA 顺序渲染；详情视图保持现状

### FAQ.tsx

- 提取共享的 `FAQ_KEYS` 数组到新文件 `webapp/src/pages/faq-items.ts`，从 `FAQ.tsx` 和 `Feedback.tsx` 两处 import
- 删除 `linuxSupport` 从数组
- 其他保持不变

### i18n（全 7 个 locale: zh-CN / en-US / ja / zh-TW / zh-HK / en-AU / en-GB）

新增 keys：
- `ticket:faq.items.appNotWorking.question` / `.answer`
- `ticket:faq.items.nodeChoice.question` / `.answer`
- `ticket:faq.items.allNationConnect.question` / `.answer`
- `ticket:feedback.myTickets` = "我的工单"
- `ticket:feedback.notFoundQuestion` = "没找到答案？"
- `ticket:feedback.submitTicket` = "提交工单"

删除 keys：
- `ticket:faq.items.linuxSupport.question` / `.answer`（全 7 locale）

zh-CN 是 source of truth；其他 locale 手工翻译。删除 key 一次完成、不留兼容桥（参照 `feedback_no_defensive_migration_bridges` 反模式）。

## 风险与权衡

- **滚动**：FAQ 占首屏后有工单用户需要滚一屏才能看到自己工单 → header「+ 提交工单」+ 多数有工单用户从邮件/推送进详情页直接缓解，可接受
- **不破坏路由**：`/faq` 保留给 SideNav 和 ServiceError，FAQ.tsx 内容仅同步 `FAQ_KEYS` 数组变更
- **i18n 工作量**：3 条新 FAQ × 7 locale = 21 条翻译，加 3 条 UI 文案 × 7 = 21 条。zh-CN 已草拟，其他 locale 在实现时翻译
- **删除 linuxSupport**：不留兼容桥意味着旧 locale 文件中相关 key 一次性删除，无回滚成本（如未来需要恢复，git history 即可找回）

## 测试

- Vitest：现有 Feedback 页测试（如有）需要更新以反映新结构；为 FaqSection 添加渲染测试（accordion 数量、key 顺序）
- 手测：
  - 列表视图（无工单 / 有工单两种状态下 FAQ 都在顶部）
  - accordion 展开 / 折叠交互
  - 顶部「+ 提交工单」与底部「提交工单」均能 navigate 到 `/submit-ticket-form`
  - 进入工单详情后 FAQ 不显示
  - 7 个 locale 切换后文案正常渲染、无 missing key 警告
  - `/faq` 路由仍可从 SideNavigation 和 ServiceError 正常进入，FAQ.tsx 仍渲染同样 9 条
- iOS / Capacitor WebView 验证 accordion 展开不触发滚动卡顿（参考 `feedback_no_window_confirm_in_webapp` —— MUI Accordion 非 native dialog，安全）

## 不做的事（YAGNI）

- 不做 FAQ 智能预展开（按 VPN 状态预展开相关条目）—— 复杂度高、收益小
- 不做 funnel 强制（建工单前必须点 FAQ）—— 拒绝用户的话术让人反感
- 不做 FAQ 搜索框 —— 9 条列表手动扫够快
- 不改 connection FAQ 的内容（运营商降速话术 + 节点已经单独成条）
- 不动后端 / API / 错误码 / submit-ticket flow
