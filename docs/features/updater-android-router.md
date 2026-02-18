# Feature: Mobile Updater Wiring & Dual-CDN Manifest

## Meta

| Field     | Value                                               |
|-----------|-----------------------------------------------------|
| Feature   | updater-android-router                              |
| Version   | v1                                                  |
| Status    | implemented                                         |
| Created   | 2026-02-18                                          |
| Updated   | 2026-02-18                                          |
| Depends on| mobile-updater, force-upgrade-app-config             |

## Version History

| Version | Date       | Summary                                                                     |
|---------|------------|-----------------------------------------------------------------------------|
| v1      | 2026-02-18 | Initial: 双 CDN 端点、相对路径 manifest、原生自动检查、webapp updater 集成、CI pipeline |

## Overview

当前移动端 K2Plugin 已实现完整的更新方法（checkWebUpdate、checkNativeUpdate、applyWebUpdate、downloadNativeUpdate、installNativeUpdate），但存在三个问题：

1. **无人调用** — K2Plugin 更新方法从未被触发，capacitor-k2.ts 未注入 `_platform.updater`
2. **单一 CDN** — manifest URL 硬编码 S3 直连（`d0.all7.cc`），无 CloudFront 容灾
3. **绝对路径 manifest** — 如果要支持双 CDN，当前方案需要维护两份 latest.json（桌面端的痛点）

本 feature 解决以上三个问题：
- K2Plugin 改为双端点数组 + 相对路径 URL 解析
- 原生层 app 启动时自动检查更新（复刻桌面端 Tauri 模式）
- capacitor-k2.ts 注入 `_platform.updater`，webapp `UpdateNotification` 跨平台复用
- CI pipeline 生成单一 `latest.json`（相对路径），一份适配所有 CDN

**不包含 OpenWrt 路由器自更新**（已拆分为独立 TODO: `docs/todos/updater-openwrt-ota.md`）。

## Context

### 现有 K2Plugin 更新方法（已实现，未触发）

| 方法 | iOS 行为 | Android 行为 |
|------|----------|-------------|
| `checkWebUpdate` | 获取 web manifest → 比较版本 → 返回 `{available, version, size}` | 同左 |
| `checkNativeUpdate` | 获取 ios manifest → 比较版本 → 返回 `{available, version, url}` | 获取 android manifest → 比较版本（含 min_android 检查）→ 返回 `{available, version, size, url}` |
| `applyWebUpdate` | 下载 zip → sha256 验证 → 解压到 web-update/ → 备份旧版本 | 同左 |
| `downloadNativeUpdate` | 返回 `"appstore"`（iOS 无法自更新） | 下载 APK 到 cache（带进度事件 `updateDownloadProgress`）→ 返回 `{path}` |
| `installNativeUpdate` | 打开 App Store URL | FileProvider + ACTION_INSTALL_PACKAGE → 系统安装器 |

### 现有 Manifest URL（单一 S3）

```swift
// iOS
private let webManifestURL = "https://d0.all7.cc/kaitu/web/latest.json"
private let iosManifestURL = "https://d0.all7.cc/kaitu/ios/latest.json"
```

```kotlin
// Android
private const val WEB_MANIFEST_URL = "https://d0.all7.cc/kaitu/web/latest.json"
private const val ANDROID_MANIFEST_URL = "https://d0.all7.cc/kaitu/android/latest.json"
```

### 桌面端对比（已实现）

桌面端 Tauri updater 使用双端点：
```json
"endpoints": [
  "https://d13jc1jqzlg4yt.cloudfront.net/kaitu/desktop/cloudfront.latest.json",
  "https://d0.all7.cc/kaitu/desktop/d0.latest.json"
]
```
但桌面端维护了**两份** latest.json（URL 绝对路径不同）—— 本 feature 用相对路径避免这个问题。

## Product Requirements

### PR1: 双 CDN 端点容灾

K2Plugin 的 manifest URL 从单一字符串改为端点数组，按顺序尝试，第一个成功即返回。

| Channel | Primary (CloudFront) | Fallback (S3 直连) |
|---------|---------------------|-------------------|
| Web OTA | `d13jc1jqzlg4yt.cloudfront.net/kaitu/web/latest.json` | `d0.all7.cc/kaitu/web/latest.json` |
| Android APK | `d13jc1jqzlg4yt.cloudfront.net/kaitu/android/latest.json` | `d0.all7.cc/kaitu/android/latest.json` |
| iOS | `d13jc1jqzlg4yt.cloudfront.net/kaitu/ios/latest.json` | `d0.all7.cc/kaitu/ios/latest.json` |

### PR2: 相对路径 Manifest

S3 上只需维护**一份** `latest.json`，下载 URL 使用相对路径。客户端根据成功获取 manifest 的端点 base path 拼接完整下载 URL。

**Manifest 格式（web/latest.json）：**
```json
{
  "version": "0.5.0",
  "url": "0.5.0/webapp.zip",
  "hash": "sha256:abc123...",
  "size": 1523456,
  "released_at": "2026-02-18T10:00:00Z"
}
```

**Manifest 格式（android/latest.json）：**
```json
{
  "version": "0.5.0",
  "url": "0.5.0/Kaitu-0.5.0.apk",
  "hash": "sha256:def456...",
  "size": 45678901,
  "released_at": "2026-02-18T10:00:00Z",
  "min_android": 26
}
```

**Manifest 格式（ios/latest.json）：**
```json
{
  "version": "0.5.0",
  "appstore_url": "https://apps.apple.com/app/id6759199298",
  "released_at": "2026-02-18T10:00:00Z"
}
```

**URL 解析规则：**
```
manifest 来自 https://d13jc1jqzlg4yt.cloudfront.net/kaitu/web/latest.json
  → baseURL = https://d13jc1jqzlg4yt.cloudfront.net/kaitu/web/
  → download = baseURL + "0.5.0/webapp.zip"
  → = https://d13jc1jqzlg4yt.cloudfront.net/kaitu/web/0.5.0/webapp.zip

manifest 来自 https://d0.all7.cc/kaitu/web/latest.json
  → baseURL = https://d0.all7.cc/kaitu/web/
  → download = baseURL + "0.5.0/webapp.zip"
  → = https://d0.all7.cc/kaitu/web/0.5.0/webapp.zip
```

如果 `url` 字段以 `http://` 或 `https://` 开头，视为绝对路径，不做拼接（向后兼容）。

### PR3: 原生层自动检查更新

App 启动（冷启动）时，原生层自动检查更新。复刻桌面端 Tauri 模式。

**检查流程：**
```
App 启动
  → 3 秒延迟（不阻塞启动 UI）
  → checkNativeUpdate()（双端点容灾）
     → 有新版本（Android）→ 静默后台下载 APK
        → 下载完成 → emit "native-update-ready" {version, size, path}
     → 有新版本（iOS）→ emit "native-update-available" {version, appStoreUrl}
     → 无新版本 → 继续 ↓
  → checkWebUpdate()（双端点容灾）
     → 有新版本 → 静默下载 zip + 验证 + 解压（全程无 UI）
        → 成功 → 下次冷启动生效
        → 失败 → 回退到 backup 版本，log warning
     → 无新版本 → 结束
```

**"下次启动生效" = 仅冷启动**。从后台恢复不 reload WebView（避免中断用户操作、VPN 状态丢失）。

**原生更新优先级 > Web OTA**：新原生版本可能包含不兼容的 Web 变更。

### PR4: Webapp 更新通知（跨平台复用）

`capacitor-k2.ts` 注入 `_platform.updater: IUpdater`，webapp 通过统一的 `useUpdater` hook + `UpdateNotification` 组件显示更新通知。

| 平台 | "立即更新" 行为 |
|------|----------------|
| 桌面 Tauri | 下载 + 安装 + 重启 app |
| Android | 调起系统安装器（APK 已后台下载完成） |
| iOS | 跳转 App Store 页面 |

Web OTA 全程静默，不走 UpdateNotification。

### PR5: 两阶段安全发布

**阶段 1（CI 自动）**：`build-mobile.yml` 在 `v*` tag push 时自动构建 + 上传产物到 S3 版本目录。此时 `latest.json` 不变，用户不会收到更新。

**阶段 2（手动确认）**：测试验证后，运行 `make publish-mobile VERSION=x.y.z` 更新 `latest.json`，正式发布版本。

此模式与桌面端 `publish-release.sh` 一致：CI 构建，人工发布。

## Technical Decisions

### TD1: 相对路径解析（不维护两份 manifest）

**选择**：manifest `url` 字段使用相对路径，客户端根据 manifest 来源端点拼接。

**替代方案**：桌面端当前方式（两份 latest.json，各含绝对路径）。

**选择理由**：
- 一份 manifest = 一个 source of truth，CI 更简单
- 不会出现两份 manifest 版本不一致的问题
- CloudFront 从 S3 origin 同步，无需额外操作
- 向后兼容：绝对路径 URL 仍然正常工作

### TD2: 端点容灾策略

**策略**：有序数组，按顺序尝试，第一个成功即返回。连接超时 10 秒。

**实现**：K2Plugin 抽取公共方法 `fetchManifest(endpoints: [String]) -> (data, baseURL)`，返回 manifest 数据和成功端点的 base URL。所有 check/apply 方法复用此方法。

### TD3: 自动检查时机——仅冷启动

**选择**：K2Plugin `load()` 方法中，延迟 3 秒后自动触发检查流程。

**不在后台恢复时检查的原因**：
- 后台恢复频率高，每次检查浪费流量
- Web OTA 结果无法立即生效（需要冷启动）
- 原生更新通知在 resume 时弹出会干扰用户

**不做定时轮询**：移动 app 生命周期短（系统定期回收），每次冷启动检查足够。

### TD4: Capacitor 事件与 IUpdater 注入

**事件设计：**

| 事件名 | 载荷 | 触发时机 |
|--------|------|---------|
| `nativeUpdateReady` | `{version, size, path}` | Android APK 下载完成 |
| `nativeUpdateAvailable` | `{version, appStoreUrl}` | iOS 有新版本 |
| `updateDownloadProgress` | `{percent}` | Android APK 下载进度（已有） |

**capacitor-k2.ts IUpdater 实现：**

```typescript
// 在 injectCapacitorGlobals() 中注入
const updaterState: IUpdater = {
  isUpdateReady: false,
  updateInfo: null,
  isChecking: false,
  error: null,
  applyUpdateNow: async () => { /* 见下文 */ },
  onUpdateReady: (callback) => { /* 监听 nativeUpdateReady/nativeUpdateAvailable 事件 */ },
};
capacitorPlatform.updater = updaterState;
```

**applyUpdateNow() 平台差异：**
- Android：调用 `K2Plugin.installNativeUpdate({path})` → 系统安装器
- iOS：调用 `window._platform.openExternal(appStoreUrl)` → App Store

### TD5: S3 文件布局

```
d0.all7.cc/kaitu/
├── web/
│   ├── latest.json              ← 一份，相对路径（手动发布时更新）
│   └── 0.5.0/
│       └── webapp.zip
├── android/
│   ├── latest.json              ← 一份，相对路径（手动发布时更新）
│   └── 0.5.0/
│       └── Kaitu-0.5.0.apk
├── ios/
│   └── latest.json              ← 一份（手动发布时更新）
├── desktop/                     ← 现有，暂不改动
│   ├── cloudfront.latest.json
│   ├── d0.latest.json
│   └── 0.4.0/...
```

CloudFront origin 指向同一 S3 bucket，自动镜像。

### TD6: 两阶段发布（安全发布模式）

**原则**：CI 自动上传产物，但不自动更新 `latest.json`。版本发布需要手动确认。

**阶段 1：CI 自动上传产物（build-mobile.yml）**
```
v* tag push
  → 构建 APK / webapp.zip / IPA
  → 上传产物到 S3 版本目录：
     s3://kaitu-releases/android/{version}/Kaitu-{version}.apk
     s3://kaitu-releases/web/{version}/webapp.zip
  → 此时 latest.json 未变更，用户不会收到更新通知
```

**阶段 2：手动发布（make publish-mobile）**
```bash
make publish-mobile VERSION=0.5.0
```

脚本执行：
1. 验证 S3 上 `{version}/` 目录中产物存在
2. 计算产物的 sha256 hash 和 size（从 S3 下载）
3. 生成 `latest.json`（相对路径 URL）
4. 上传 `latest.json` 到 S3 各 channel 目录
5. 输出确认信息

**Makefile target：**
```makefile
publish-mobile:
	@test -n "$(VERSION)" || (echo "Usage: make publish-mobile VERSION=x.y.z" && exit 1)
	@echo "Publishing mobile v$(VERSION)..."
	bash scripts/publish-mobile.sh $(VERSION)
```

**与桌面端 `publish-release.sh` 同一发布模式**：CI 负责构建，人工确认后再发布版本指针。

### TD7: CI Pipeline 变更（仅上传产物）

`build-mobile.yml` 新增 S3 产物上传步骤（**不更新 latest.json**）：

```yaml
# 在 build-android job 末尾
- name: Upload APK to S3
  run: |
    VERSION="${GITHUB_REF_NAME#v}"
    aws s3 cp "build/Kaitu-${VERSION}.apk" \
      "s3://kaitu-releases/android/${VERSION}/Kaitu-${VERSION}.apk"

# Web OTA（独立 job 或 build-android job 中）
- name: Build and upload Web OTA
  run: |
    VERSION="${GITHUB_REF_NAME#v}"
    cd webapp && yarn build
    cd dist && zip -r "../../webapp-${VERSION}.zip" .
    cd ../..
    aws s3 cp "webapp-${VERSION}.zip" \
      "s3://kaitu-releases/web/${VERSION}/webapp.zip"
```

CI 只负责上传产物到版本目录。`latest.json` 由 `make publish-mobile VERSION=x.y.z` 手动更新（见 TD6）。

## Key Files

### 已修改

| File | Change |
|------|--------|
| `mobile/plugins/k2-plugin/ios/Plugin/K2Plugin.swift` | 单一 URL → 端点数组，相对路径解析，load() 自动检查，emit 事件 |
| `mobile/plugins/k2-plugin/android/src/main/java/io/kaitu/k2plugin/K2Plugin.kt` | 同上 |
| `webapp/src/services/capacitor-k2.ts` | 注入 `_platform.updater: IUpdater`，监听原生更新事件 |
| `.github/workflows/build-mobile.yml` | 新增 S3 产物上传步骤（不更新 latest.json） |

### 已新增

| File | Purpose |
|------|---------|
| `scripts/publish-mobile.sh` | 手动发布脚本：从 S3 验证产物 → 生成 latest.json → 上传 |
| `scripts/test-publish-mobile.sh` | publish-mobile.sh 的 10 个 shell 测试（使用本地 mock S3） |
| `Makefile` (publish-mobile target) | `make publish-mobile VERSION=x.y.z` 入口 |

### 未修改（已就绪）

| File | Why |
|------|-----|
| `webapp/src/components/UpdateNotification.tsx` | 已通过 `_platform.updater` 驱动，注入后自动工作 |
| `webapp/src/hooks/useUpdater.ts` | 已支持 `onUpdateReady` 回调模式 |
| `webapp/src/types/kaitu-core.ts` | `IUpdater` 接口已完整定义 |
| `webapp/src/App.tsx` | 已挂载 `<UpdateNotification />` |

## Acceptance Criteria

### AC1: 双 CDN 端点容灾

- K2Plugin iOS/Android 从端点数组按顺序尝试获取 manifest
- CloudFront 端点可用时优先使用（主端点）
- CloudFront 不可用时自动 fallback 到 S3 直连
- 连接超时 10 秒，不阻塞 app 启动

### AC2: 相对路径 Manifest

- S3 上每个 channel 只有一份 `latest.json`
- manifest `url` 字段为相对路径（如 `0.5.0/webapp.zip`）
- 客户端根据成功获取 manifest 的端点 base path 拼接下载 URL
- 绝对路径 URL（`https://...`）仍能正常工作（向后兼容）
- 从 CloudFront 获取 manifest → 从 CloudFront 下载产物
- 从 S3 获取 manifest → 从 S3 下载产物

### AC3: 原生自动检查

- App 冷启动后 3 秒自动开始检查
- 先检查 native 更新，再检查 web OTA（native 优先）
- Android 有新 APK → 静默后台下载 → emit `nativeUpdateReady` 事件
- iOS 有新版本 → emit `nativeUpdateAvailable` 事件
- Web OTA 有新版本 → 静默下载 + 解压 → 下次冷启动生效
- 检查失败仅 log warning，不影响 app 正常使用
- 从后台恢复不触发检查

### AC4: Webapp Updater 集成

- `capacitor-k2.ts` 注入 `_platform.updater: IUpdater`
- `useUpdater` hook 在移动端返回 `isAvailable: true`
- Android 收到 `nativeUpdateReady` → `UpdateNotification` 显示新版本号
- iOS 收到 `nativeUpdateAvailable` → `UpdateNotification` 显示新版本号
- Android 点击"立即更新" → 调起系统安装器
- iOS 点击"立即更新" → 跳转 App Store
- 点击"稍后" → 通知条消失，本次会话不再提示
- Web OTA 更新全程无 UI 提示

### AC5: 两阶段安全发布

- `v*` tag push 触发 `build-mobile.yml`，上传产物到 S3 版本目录
- CI 不自动更新 `latest.json`（安全发布门控）
- `make publish-mobile VERSION=x.y.z` 从 S3 下载产物、计算 hash/size、生成 latest.json、上传
- publish 脚本验证 S3 上产物存在后再生成 latest.json（防止指向不存在的文件）
- 发布后 CloudFront 自动镜像更新的 latest.json

## Testing Strategy

### 自动化

- **Webapp 单元测试**：`webapp/src/services/__tests__/capacitor-k2.test.ts` — 8 updater 测试，mock K2Plugin events，验证 IUpdater 状态转换（`test_injectCapacitorGlobals_sets_updater`、`test_updater_handles_nativeUpdateReady_event`、`test_applyUpdateNow_android_calls_installNativeUpdate` 等）
- **Publish 脚本测试**：`scripts/test-publish-mobile.sh` — 10 个 shell 测试，使用本地 mock S3（`--s3-base` flag），验证 JSON schema、相对路径格式、sha256 前缀、版本一致性
- **向后兼容**：`test_resolveDownloadURL_absolute_passthrough` — 绝对路径 URL 原样使用

### 手动集成

- **双 CDN 容灾**：屏蔽 CloudFront DNS → 验证 fallback 到 S3 直连
- **Android APK 流程**：push v* tag → 验证 APK 出现在 S3 → app 检查到更新 → 后台下载 → UpdateNotification 显示 → 点击安装
- **iOS App Store 流程**：app 检查到新版本 → UpdateNotification 显示 → 点击跳转 App Store
- **Web OTA 流程**：上传新 webapp.zip → app 冷启动检查 → 静默下载解压 → 再次冷启动 → 新版本生效
- **Web OTA 回退**：上传损坏的 zip → 验证 hash 校验失败 → app 回退到 backup 版本
- **无更新路径**：已是最新版本时 → 无通知、无弹框
- **离线容忍**：manifest 获取失败 → app 正常启动使用（无 crash、无阻塞 UI）

## Deployment & CI/CD

两阶段发布流程：

```
阶段 1：CI 自动（v* tag push）
  → build-mobile.yml
  → Android: gomobile bind → AAR → cap sync → assembleRelease → APK → S3 版本目录
  → iOS: gomobile bind → xcframework → cap sync → xcodebuild archive → IPA → App Store Connect
  → Web OTA: yarn build → zip dist/ → S3 版本目录
  → 此时 latest.json 未变更，用户不受影响

阶段 2：手动发布
  → 测试验证 APK / TestFlight / Web OTA 产物
  → make publish-mobile VERSION=0.5.0
  → 脚本从 S3 下载产物 → 计算 hash/size → 生成 latest.json → 上传 S3
  → CloudFront 自动同步 → 用户收到更新通知
```

## Impact Analysis

### 受影响模块

| 模块 | 影响范围 | 风险 |
|------|---------|------|
| K2Plugin Swift | URL 数组 + 相对路径解析 + load() 自动检查 + 事件 | 中 — 需本地 plugin sync |
| K2Plugin Kotlin | 同上 | 中 |
| capacitor-k2.ts | 新增 updater 注入 + 事件监听 | 低 — 新增代码，不改现有逻辑 |
| build-mobile.yml | 新增 manifest 生成 + S3 上传步骤 | 低 — 附加步骤 |
| UpdateNotification.tsx | 无修改 | 无 |
| useUpdater.ts | 无修改 | 无 |

### 向后兼容

- manifest `url` 如果是绝对路径（`https://...`），客户端直接使用，不做拼接
- 现有的 Web OTA 目录结构（`web-update/`、`web-backup/`、`version.txt`）不变
- 桌面端不受影响（Tauri updater 独立机制）
