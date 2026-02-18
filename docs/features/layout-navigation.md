# Layout & Navigation System

| Field | Value |
|-------|-------|
| Feature | Layout & Navigation System |
| Version | 0.4.0 |
| Status | implemented |
| Created | 2026-02-18 |
| Updated | 2026-02-18 |

---

## Overview

k2app 的 Layout & Navigation 系统是整个 webapp 的外壳框架，负责：

1. **App Shell**: `Layout` 组件包裹所有路由页面，提供统一的容器结构
2. **双导航模式**: 移动端 BottomNavigation（底部 tab bar）、桌面端/OpenWrt SideNavigation（左侧 sidebar）
3. **Keep-alive tab 缓存**: 主 tab 页面挂载后不销毁，通过 `visibility:hidden` 隐藏非活跃页
4. **响应式布局**: 基于 768px 断点自动切换 mobile/desktop 模式，路由器模式强制 desktop
5. **Feature flag 路由控制**: `AppConfig.features` 控制 tab 和路由的可见性
6. **独立平台桥接**: Standalone/OpenWrt 模式通过相对路径 HTTP 调用 daemon API

---

## Product Requirements

### PR-1: App Shell 架构

- 所有页面统一包裹在 `<Layout>` 组件内
- Layout 提供：AnnouncementBanner（公告栏）、ServiceAlert（服务状态告警）、主内容区、导航组件、FeedbackButton（悬浮反馈按钮）
- 启动链路：`main.tsx` → Sentry → i18n → 平台检测(Tauri/Capacitor/Standalone) → `initializeAllStores()` → render `<App>`
- `<App>` 结构：`ThemeProvider > BrowserRouter > AuthGate > AppRoutes`
- AuthGate 仅在 `isAuthChecking` 时显示 Loading，之后开放访问（无论是否登录）
- 全局弹窗（LoginDialog、ForceUpgradeDialog、UpdateNotification、AlertContainer）挂载在路由层外

### PR-2: 双导航模式

**移动端 — BottomNavigation**:
- 固定在屏幕底部，高度 56px
- 适配 safe-area-inset-bottom（iPhone 底部横条、Android 手势导航）
- 5 个 tab：Dashboard `/`、Purchase `/purchase`、Invite `/invite`、Discover `/discover`、Account `/account`
- Invite tab 根据 user.isRetailer 显示"经销商"或"邀请"文案
- Purchase tab 根据登录状态显示"购买"或"激活"文案
- Feature flag 控制 Invite、Discover 的可见性

**桌面端 — SideNavigation**:
- 永久固定左侧 Drawer，宽度 220px
- 顶部显示 appName（来自 AppConfig）
- Primary nav items 与 BottomNavigation 一致（5 个主 tab）
- Secondary nav items（仅桌面端）：Devices `/devices`、ProHistory `/pro-histories`、Help `/faq`
- 使用 `location.pathname.startsWith(path)` 做路径匹配高亮（`/` 精确匹配）
- Feature flag 控制 secondary items 的可见性

### PR-3: Keep-alive Tab 缓存

- 4 个 keep-alive tab：Dashboard `/`、Invite `/invite`、Discover `/discover`、Account `/account`
- Purchase 已移出 keep-alive，作为普通路由每次重新渲染（避免与 LoginRequiredGuard 冲突）
- Tab 页使用 `React.lazy()` 做 code splitting，首次访问时 lazy load
- `mountedTabs` state 记录已挂载的 tab，首次访问后永不卸载
- 活跃 tab：`visibility: visible` + `pointerEvents: auto` + flex 布局占满空间
- 非活跃 tab：`visibility: hidden` + `pointerEvents: none` + `position: absolute` + `height: 0`（塌缩不占空间）
- Feature flag 检查：未启用的 feature 对应的 tab 不会挂载
- 非 tab 路由通过 `<Outlet>` 正常渲染，tab 页活跃时 Outlet 不显示

### PR-4: 响应式布局 & 路由器模式

- 断点：`DESKTOP_BREAKPOINT = 768px`
- `layoutMode`: `'mobile'` | `'desktop'`
- 移动端 `< 768px`：显示 BottomNavigation，Main 区域无左 margin
- 桌面端 `>= 768px`：显示 SideNavigation，Main 区域 `marginLeft: 220px`
- 路由器模式（`VITE_CLIENT_IS_ROUTER=true`）：强制 `desktop` 模式，不监听 resize
- `connectionButtonCollapsed`：桌面版默认展开，路由器版默认折叠
- 使用 `window.matchMedia` 监听断点变化（兼容旧版浏览器的 `addListener`）
- Layout store 同时存储派生状态（`isMobile`/`isDesktop`）以支持精准订阅

### PR-5: 路由结构

所有路由嵌套在 `<Layout>` 下：

**Keep-alive Tab 路由**（在 Routes 中声明 `element={null}`，实际渲染在 Layout 内部）：
| 路径 | 组件 | 登录要求 | Feature Flag |
|------|------|----------|-------------|
| `/` | Dashboard | 否 | - |
| `/invite` | InviteHub | 是 | `invite` |
| `/discover` | Discover | 否 | `discover` |
| `/account` | Account | 否 | - |

**普通路由**（通过 `<Outlet>` 渲染）：
| 路径 | 组件 | 守卫 | Feature Flag |
|------|------|------|-------------|
| `/purchase` | Purchase | - | - |
| `/tunnels` | Tunnels | - | - |
| `/changelog` | Changelog | - | - |
| `/service-error` | ServiceError | - | - |
| `/devices` | Devices | LoginRequiredGuard | - |
| `/pro-histories` | ProHistory | LoginRequiredGuard | `proHistory` |
| `/invite-codes` | MyInviteCodeList | LoginRequiredGuard | `invite` |
| `/member-management` | MemberManagement | LoginRequiredGuard | `memberManagement` |
| `/device-install` | DeviceInstall | - | `deviceInstall` |
| `/faq` | FAQ | - | `feedback` |
| `/issues` | Issues | LoginRequiredGuard | `feedback` |
| `/issues/:number` | IssueDetail | LoginRequiredGuard | `feedback` |
| `/submit-ticket` | SubmitTicket | MembershipGuard | `feedback` |
| `/update-email` | UpdateLoginEmail | MembershipGuard | `updateLoginEmail` |

### PR-6: 登录守卫体系

- **AuthGate**：全局认证检查，`isAuthChecking` 时显示 LoadingPage，检查完成后开放访问
- **LoginRequiredGuard**：包裹需要登录的页面，未登录时弹出 LoginDialog（不跳转，不阻塞渲染）。通过 `pagePath` 参数解决 keep-alive 场景下多个 Guard 同时响应的问题
- **MembershipGuard**：会员权限守卫，会员过期时重定向到购买页

### PR-7: 外部内容加载

**Discover 页面**：
- 通过 iframe 加载 `discoveryUrl`（来自 `useAppLinks`，baseURL 由 AppConfig 返回）
- 注入 KaituBridge（`useKaituBridge` hook），支持 iframe 内的认证状态同步
- 监听 `window.addEventListener('message')` 处理外部链接（`external-link` 类型消息），通过 `window._platform.openExternal()` 打开
- 进度条动画模拟加载过程（随机增长至 90%，iframe onLoad 后完成至 100%）

**Changelog 页面**：
- 同样通过 iframe 加载 `changelogUrl`（带 `?embed=true` 参数 + locale 路径注入）
- 与 Discover 相同的 KaituBridge 注入和外部链接处理
- 相同的进度条动画模式

### PR-8: Standalone / OpenWrt 桥接

- 当 `window.__TAURI__` 和 `Capacitor.isNativePlatform()` 都不满足时，回退到 standalone 模式
- `standalone-k2.ts` 提供 `window._k2`（VPN 控制）和 `window._platform`（平台能力）
- VPN 控制通过 `fetch('/api/core', { method: 'POST', body: { action, params } })` 相对路径调用 daemon
- UDID 通过 `fetch('/api/device/udid')` 获取
- `window._platform.os` = `'web'`，`isDesktop` = `false`，`isMobile` = `false`
- 存储使用 `webSecureStorage`（localStorage fallback）
- 路由器模式下 `VITE_CLIENT_IS_ROUTER=true`，layout 强制 desktop 模式

---

## Technical Decisions

### TD-1: Keep-alive 使用 visibility 而非 React portal

选择 `visibility: hidden` + `position: absolute` + `height: 0` 方案而非 React portal 或 `display: none`：
- `visibility: hidden` 保留组件在 DOM 中，所有 React state、useEffect、ref 保持不变
- 与 `display: none` 不同，`visibility: hidden` 的元素仍然参与布局计算（但通过 height:0 + absolute 规避了实际占位）
- 不使用 React portal 是因为 portal 会改变事件冒泡路径，且实现复杂度更高
- `pointerEvents: none` 防止隐藏页面拦截用户交互

### TD-2: Tab 路由在 Routes 中声明 null 元素

Tab 页在 React Router 的 `<Routes>` 中声明 `element={null}`，实际渲染在 Layout 内部的 keep-alive 逻辑中：
- 这保证了 React Router 的路径匹配正常工作
- 同时 Layout 可以完全控制 tab 的挂载/显示/隐藏生命周期
- 非 tab 页通过 `<Outlet>` 正常渲染

### TD-3: Layout store 存储派生状态

`isMobile` 和 `isDesktop` 作为独立字段存储在 store 中（而非通过 getter 计算）：
- 支持 Zustand 的 `subscribeWithSelector` 精准订阅
- 组件可以只订阅 `isMobile` 而不触发 `layoutMode` 变更时的无关重渲染

### TD-4: Feature flag 双重检查

Feature flag 在两处生效：
1. **路由层**（`App.tsx`）：条件渲染 `<Route>` 元素，未启用的 feature 路由不存在
2. **Layout 层**（`Layout.tsx` + 导航组件）：tab 和 nav items 根据 feature flag 过滤

### TD-5: Purchase 移出 keep-alive

Purchase 页面从 keep-alive tabs 中移出，改为普通路由：
- 原因：Purchase 使用 LoginRequiredGuard，keep-alive 下 Guard 的 useEffect 在页面隐藏时仍会触发，导致冲突
- 移出后每次访问 Purchase 都会重新渲染，但 Guard 行为正确

### TD-6: safe-area 适配

- 移动端 Layout 容器顶部使用 `env(safe-area-inset-top)` padding
- BottomNavigation 使用 `env(safe-area-inset-bottom)` padding
- 确保 iOS 刘海屏、Android 打孔屏、底部手势导航区域不遮挡内容

### TD-7: 公告栏和服务告警在导航之上

- AnnouncementBanner：marquee 滚动文字，可关闭（localStorage 持久化），支持过期时间
- ServiceAlert：固定顶部 fixed 定位，三种告警类型（initialization / serviceFailure / networkError），带一键修复按钮

---

## Key Files

| 文件路径 | 职责 |
|---------|------|
| `webapp/src/components/Layout.tsx` | App shell 根组件：keep-alive tab 管理、AnnouncementBanner、ServiceAlert、SideNavigation、BottomNavigation、FeedbackButton |
| `webapp/src/components/BottomNavigation.tsx` | 移动端底部 tab bar（5 tabs，feature flag 过滤，memo 优化） |
| `webapp/src/components/SideNavigation.tsx` | 桌面端左侧 sidebar（primary + secondary nav items，permanent Drawer） |
| `webapp/src/stores/layout.store.ts` | 布局状态管理：layoutMode、isRouterMode、sidebarWidth、connectionButtonCollapsed、matchMedia 监听 |
| `webapp/src/App.tsx` | 路由定义：Layout 包裹所有路由，tab 路由 element=null，普通路由正常渲染 |
| `webapp/src/main.tsx` | 启动引导：Sentry → i18n → 平台检测(Tauri/Capacitor/Standalone) → store 初始化 → 渲染 |
| `webapp/src/config/apps.ts` | AppConfig 定义：feature flags、branding、apiEndpoint |
| `webapp/src/services/standalone-k2.ts` | Standalone/OpenWrt 桥接：相对路径 fetch 调用 daemon API |
| `webapp/src/pages/Discover.tsx` | Discover tab：iframe 加载外部 URL + KaituBridge 注入 + 进度条 |
| `webapp/src/pages/Changelog.tsx` | Changelog 页面：iframe 加载带 embed+locale 参数的 URL |
| `webapp/src/pages/FAQ.tsx` | 帮助中心：安全软件白名单、社区反馈、提交工单入口 |
| `webapp/src/components/AuthGate.tsx` | 全局认证门控：仅 checking 时阻塞，完成后开放 |
| `webapp/src/components/LoginRequiredGuard.tsx` | 页面级登录守卫：未登录弹 LoginDialog，不阻塞渲染 |
| `webapp/src/components/MembershipGuard.tsx` | 会员守卫：过期时重定向 |
| `webapp/src/components/ServiceAlert.tsx` | 服务状态告警 banner：初始化/服务失败/网络错误 |
| `webapp/src/components/AnnouncementBanner.tsx` | 公告栏：marquee 滚动、可关闭、过期控制 |
| `webapp/src/components/FeedbackButton.tsx` | 悬浮反馈按钮：固定位置、脉冲动画 |
| `webapp/src/hooks/useAppLinks.ts` | 构建应用链接（discoveryUrl、changelogUrl 等），支持多语言 |
| `webapp/src/hooks/useKaituBridge.ts` | iframe bridge 注入 hook：认证状态广播、消息监听 |
| `webapp/src/stores/index.ts` | Store 统一导出 + `initializeAllStores()`（layout → auth → vpn 顺序） |

---

## Acceptance Criteria

### AC-1: App Shell 正常渲染
- [ ] Layout 包裹所有页面，AnnouncementBanner、ServiceAlert、FeedbackButton 正确显示
- [ ] AuthGate 在 checking 时显示 LoadingPage，完成后立即显示内容

### AC-2: 双导航正确切换
- [ ] 窗口宽度 < 768px 时显示 BottomNavigation，隐藏 SideNavigation
- [ ] 窗口宽度 >= 768px 时显示 SideNavigation，隐藏 BottomNavigation
- [ ] 路由器模式始终显示 SideNavigation
- [ ] BottomNavigation 5 个 tab 根据 feature flag 正确显示/隐藏
- [ ] SideNavigation primary + secondary items 根据 feature flag 正确显示/隐藏

### AC-3: Keep-alive 缓存生效
- [ ] 从 Dashboard 切换到 Account 再切回，Dashboard 状态保留（不重新挂载）
- [ ] 未访问过的 tab 不会被预加载（lazy loading 生效）
- [ ] 隐藏的 tab 不可交互（pointerEvents: none）
- [ ] 隐藏的 tab 不占据可视空间（height: 0 + absolute）
- [ ] Purchase 每次导航都重新渲染（非 keep-alive）

### AC-4: 路由守卫正常工作
- [ ] 未登录访问 `/invite` 弹出 LoginDialog，页面内容仍可见
- [ ] 未登录访问 `/devices` 弹出 LoginDialog
- [ ] 会员过期访问 `/submit-ticket` 重定向到购买页
- [ ] Keep-alive 场景下只有当前活跃页面的 Guard 触发副作用

### AC-5: 外部内容正确加载
- [ ] Discover 页面 iframe 加载 discoveryUrl，进度条正常动画
- [ ] Changelog 页面 iframe 加载带 embed+locale 参数的 URL
- [ ] iframe 内外部链接通过 postMessage → `_platform.openExternal()` 在默认浏览器打开
- [ ] 认证状态变化时广播到 iframe

### AC-6: Standalone / OpenWrt 模式
- [ ] 无 Tauri/Capacitor 时自动注入 standalone K2 和 platform
- [ ] VPN 控制通过相对路径 `/api/core` POST 调用 daemon
- [ ] UDID 通过 `/api/device/udid` GET 获取
- [ ] 路由器模式下 layout 锁定为 desktop，不响应 resize

### AC-7: 响应式与 safe-area
- [ ] iOS 刘海屏顶部不遮挡内容（safe-area-inset-top）
- [ ] iPhone/Android 底部导航区域不与系统手势冲突（safe-area-inset-bottom）
- [ ] 桌面端 Main 区域 marginLeft 为 sidebar 宽度（220px），带过渡动画
