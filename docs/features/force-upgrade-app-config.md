# Feature: Force Upgrade & App Config

## Meta

| Field | Value |
|-------|-------|
| Feature | force-upgrade-app-config |
| Version | v1 |
| Status | implemented |
| Created | 2026-02-18 |
| Updated | 2026-02-18 |

## Overview

App Config 是 Kaitu 客户端的全局远程配置系统，通过 Center API 的 `/api/app/config` 端点下发。基于此配置实现了三个核心能力：

1. **强制升级 (Force Upgrade)** — 当客户端版本低于 `minClientVersion` 时弹出不可关闭的对话框，用户必须下载新版本
2. **服务公告 (Announcement Banner)** — 服务端推送的跑马灯横幅，支持过期、关闭持久化、可选链接
3. **自动更新 (Auto Update)** — 桌面端 Tauri updater 在后台检测新版本，下载完成后非阻塞式通知用户安装

三者分别对应不同的紧急程度：强制升级 > 自动更新 > 公告。

## Product Requirements

### PR-1: App Config 远程配置

**需求**：客户端启动后从 Center API 拉取全局配置，控制前端行为。

**配置结构**（`GET /api/app/config`，无需认证）：

```typescript
interface AppConfig {
  appLinks: AppLinks;           // 应用链接（下载页、隐私协议、更新日志等）
  inviteReward: InviteConfig;   // 邀请奖励配置
  minClientVersion?: string;    // 最低客户端版本，低于此版本强制升级
  announcement?: Announcement;  // 公告信息，nil 表示无公告
}
```

**缓存策略**：Stale-While-Revalidate (SWR)
- 首次加载：等待网络请求
- 后续访问：立即返回缓存，后台刷新
- TTL: 1 小时（配置变更频率低）
- 缓存层：内存 + localStorage 双层（`cacheStore` 键 `api:app_config`）

### PR-2: 强制升级

**需求**：当客户端版本低于服务端要求的最低版本时，阻止用户继续使用，引导下载新版。

**流程**：
1. `useAppConfig` hook 从 `/api/app/config` 获取 `minClientVersion`
2. `ForceUpgradeDialog` 对比 `window._platform.version`（当前版本）和 `minClientVersion`
3. 使用 `cleanVersion()` 清理 `v` 前缀，`isValidVersion()` 校验格式
4. 使用 `isOlderVersion()` 比较：若当前版本 < 最低版本，弹出对话框
5. 对话框不可关闭（`disableEscapeKeyDown`，空 `onClose`）
6. "立即更新"按钮通过 `window._platform.openExternal()` 打开下载页
7. 下载 URL 从 `appConfig.appLinks.baseURL + appLinks.installPath` 拼接，fallback `https://kaitu.io/install`
8. 点击后对话框保持打开 — 用户必须手动安装新版

**版本比较规则**（`versionCompare.ts`）：
- 支持完整 semver: `major.minor.patch[-prerelease][+build]`
- 比较顺序：major → minor → patch → prerelease
- 有 prerelease 的版本 < 无 prerelease 的同版本号（如 `1.0.0-beta.1 < 1.0.0`）
- prerelease 段内按 `.` 分割，数字段按数值比较，字符段按字典序
- 无效版本格式跳过比较（静默失败，不弹框）

### PR-3: 服务公告

**需求**：服务端推送可关闭的横幅公告，跑马灯滚动显示。

**公告结构**：
```typescript
interface Announcement {
  id: string;        // 唯一 ID，用于跟踪关闭状态
  message: string;   // 公告文字
  linkUrl?: string;   // 可选跳转链接
  linkText?: string;  // 可选链接文字（默认"查看详情"）
  expiresAt?: number; // 可选过期时间戳（Unix 秒），0 = 不过期
}
```

**行为**：
- 显示位置：Layout 顶部，在 sidebar 右侧（桌面端有 `marginLeft`）
- 跑马灯动画速度：`Math.max(8, Math.ceil(message.length / 15))` 秒，鼠标悬停暂停
- 关闭持久化：`localStorage` 键 `announcement_dismissed_{id}`
- 过期检查：`Date.now() / 1000 > expiresAt`（expiresAt 为 0 时不过期）
- 链接点击通过 `window._platform.openExternal()` 在外部浏览器打开
- 无公告或已关闭/已过期时组件返回 `null`

### PR-4: 桌面端自动更新

> 详见 [desktop-auto-updater.md](desktop-auto-updater.md) — Tauri updater 插件、CDN 分发、签名验证、前端通知组件。

### PR-5: 版本显示

**`VersionItem`**：Account 页面中显示当前版本号（`appVersion` prop），点击跳转 `/changelog` 页面。

**`VersionComparison`**：Free vs Pro 功能对比表（非版本号比较），展示设备数、流量、广告拦截等差异。

## Technical Decisions

### TD-1: 版本来源传播链

```
package.json (0.4.0)          ← 唯一版本源
  ├→ tauri.conf.json           "version": "../../package.json" (引用)
  │    └→ CARGO_PKG_VERSION    Tauri build 注入
  │         └→ get_platform_info IPC → window._platform.version
  ├→ Makefile ldflags          k2 binary -X main.version=...
  └→ webapp build              import.meta.env 或运行时从 _platform 读取
```

桌面端版本比较路径：`window._platform.version` (from Rust `get_platform_info`) vs `appConfig.minClientVersion` (from Center API)。

### TD-2: 三层更新机制分离

| 层级 | 组件 | 阻塞 | 触发源 | 关闭方式 |
|------|------|------|--------|----------|
| 强制升级 | `ForceUpgradeDialog` | 阻塞（不可关闭） | Server `minClientVersion` | 无法关闭 |
| 自动更新 | `UpdateNotification` | 非阻塞（顶部横幅） | Tauri updater CDN | "稍后"按钮 |
| 公告 | `AnnouncementBanner` | 非阻塞（顶部横幅） | Server `announcement` | 关闭按钮 + localStorage 持久化 |

渲染层级：
- `ForceUpgradeDialog` 和 `UpdateNotification` 在 `App.tsx`（路由外层，全局可见）
- `AnnouncementBanner` 在 `Layout.tsx`（路由内层，仅主界面可见）

### TD-3: useAppConfig 缓存策略

选择 SWR 而非纯网络请求的原因：
- App Config 变更频率极低（通常数天才改一次）
- 强制升级检查不应被网络延迟阻塞（尤其在弱网环境）
- 缓存未命中时才等待网络，命中后立即返回 + 后台刷新

缓存实现使用 `CacheStore` 单例：内存 Map 优先，fallback 到 localStorage。TTL 通过 `expireAt` 时间戳控制。

### TD-4: 版本比较的防御性设计

- `cleanVersion()` 去除 `v` 前缀（如 `v0.4.0` → `0.4.0`）
- `isValidVersion()` 在比较前校验格式，无效版本静默跳过
- `compareVersions()` 解析失败时 fallback 到 `localeCompare`（字符串比较）
- `ForceUpgradeDialog` 捕获所有异常，版本检查失败不阻塞用户

### TD-5: 服务端配置架构

Center API 使用 Viper 读取 YAML 配置文件：

```yaml
frontend_config:
  min_client_version: "0.4.0"
  app_links:
    base_url: "https://www.kaitu.io"
    install_path: "/install"
    # ... 其他链接
  announcement:
    id: "announcement-2024-01"
    message: "系统维护公告"
    link_url: "https://kaitu.io/news"
    link_text: "查看详情"
    expires_at: 1704067200
```

所有字段都有默认值。`announcement` 为空时不返回（`omitempty`）。`minClientVersion` 为空时客户端跳过强制升级检查。

### TD-6–7: 桌面端自动更新技术细节

> 详见 [desktop-auto-updater.md](desktop-auto-updater.md) — 平台差异、CDN 配置、签名机制。

## Key Files

### 前端组件

| File | Purpose |
|------|---------|
| `webapp/src/components/ForceUpgradeDialog.tsx` | 强制升级阻塞对话框 |
| `webapp/src/components/UpdateNotification.tsx` | 桌面端更新通知横幅 |
| `webapp/src/components/AnnouncementBanner.tsx` | 服务公告跑马灯横幅 |
| `webapp/src/components/VersionItem.tsx` | Account 页版本显示条目 |
| `webapp/src/components/VersionComparison.tsx` | Free vs Pro 功能对比表 |

### Hooks & 工具

| File | Purpose |
|------|---------|
| `webapp/src/hooks/useAppConfig.ts` | App Config SWR 缓存 hook |
| `webapp/src/utils/versionCompare.ts` | Semver 解析、比较、清理、校验 |

### 类型定义

| File | Purpose |
|------|---------|
| `webapp/src/services/api-types.ts` | `AppConfig`, `AppLinks`, `Announcement`, `InviteConfig` 类型 |
| `webapp/src/types/kaitu-core.ts` | `IUpdater`, `UpdateInfo`, `IPlatform.updater?` 接口 |

### 服务端

| File | Purpose |
|------|---------|
| `api/api_app_config.go` | `GET /api/app/config` 处理函数 + 数据结构定义 |
| `api/route.go` | 路由注册（`api.GET("/app/config", api_get_app_config)`） |

### 挂载点

| File | Purpose |
|------|---------|
| `webapp/src/App.tsx` | 挂载 `ForceUpgradeDialog` + `UpdateNotification`（路由外层） |
| `webapp/src/components/Layout.tsx` | 挂载 `AnnouncementBanner`（路由内层） |

### 国际化

| File | Purpose |
|------|---------|
| `webapp/src/i18n/locales/zh-CN/startup.json` | `forceUpgrade.*` + `app.*` 键 |
| `webapp/src/i18n/locales/en-US/startup.json` | 英文翻译 |
| 其他 locale: `ja`, `zh-TW`, `zh-HK`, `en-GB`, `en-AU` | 对应翻译 |

### 缓存基础设施

| File | Purpose |
|------|---------|
| `webapp/src/services/cache-store.ts` | `CacheStore` 类 — 内存 + localStorage 双层缓存 |
| `webapp/src/services/cloud-api.ts` | `cloudApi.get()` — 带认证的 HTTP 请求 |

## Acceptance Criteria

### AC-1: 强制升级对话框

- [x] 当 `minClientVersion` 存在且当前版本低于要求时，对话框弹出
- [x] 对话框不可通过 ESC、点击外部、或其他方式关闭
- [x] 对话框显示当前版本号和最低要求版本号
- [x] "立即更新"按钮通过外部浏览器打开下载页面
- [x] 点击更新按钮后对话框保持打开
- [x] 版本格式无效时静默跳过（不弹框，不崩溃）
- [x] `minClientVersion` 为空时不做强制升级检查
- [x] 下载 URL 正确拼接，有 fallback 默认值
- [x] i18n 文本在所有 7 个 locale 中完整

### AC-2: App Config 远程配置

- [x] `GET /api/app/config` 不需要认证即可访问
- [x] 返回 `appLinks`、`inviteReward`、`minClientVersion`、`announcement` 字段
- [x] 所有 `appLinks` 字段有默认值
- [x] `useAppConfig` hook 实现 SWR 缓存策略（TTL 1 小时）
- [x] 缓存命中时立即返回 + 后台刷新
- [x] 缓存未命中时等待网络请求
- [x] 网络失败时 `error` 状态正确设置

### AC-3: 服务公告横幅

- [x] 公告有内容时显示跑马灯横幅
- [x] 关闭按钮可关闭横幅
- [x] 关闭状态通过 `localStorage` 按公告 ID 持久化
- [x] 已过期公告不显示
- [x] 已关闭公告不再显示
- [x] 可选链接通过外部浏览器打开
- [x] 链接文字有默认值（`common:common.viewDetails`）
- [x] 动画速度根据文字长度自适应
- [x] 鼠标悬停时动画暂停
- [x] 公告为 null 时组件不渲染

### AC-4: 桌面端自动更新

> AC 详见 [desktop-auto-updater.md](desktop-auto-updater.md)。

### AC-5: 版本比较工具

- [x] 支持 `major.minor.patch` 标准格式
- [x] 支持 prerelease 后缀（如 `1.0.0-beta.1`）
- [x] 支持 build 元数据后缀（如 `1.0.0+build.123`）
- [x] `cleanVersion()` 去除 `v` 前缀
- [x] `isValidVersion()` 校验格式有效性
- [x] `compareVersions()` 解析失败时 fallback 到字符串比较
- [x] `checkServiceVersionCompatibility()` 用于 daemon 版本匹配检查
