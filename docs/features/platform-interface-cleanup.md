# Feature: Platform Interface Cleanup

## Meta

| Field     | Value                          |
|-----------|--------------------------------|
| Version   | v1                             |
| Status    | implemented                    |
| Updated   | 2026-02-18                     |

## Problem

`IPlatform` 接口严重膨胀：19 个方法/属性中仅 6 个有生产消费者，10 个零调用，3 个仅测试引用。同时存在以下问题：

1. **死代码**：`showToast`, `getLocale`, `exit`, `debug`, `warn`, `getPid` 从未被消费
2. **安全隐患**：`nativeExec` 是通用 IPC 口子，唯一用途是 `admin_reinstall_service`
3. **实现不可靠**：clipboard 和 openExternal 使用 `navigator.clipboard` / `window.open`，在 WebView 中各平台表现不一致（Android clipboard 完全不工作，Windows WebView2 有 focus bug）
4. **缺失能力**：`syncLocale` 未实现（Tauri tray 多语言需要），`uploadLogs` Tauri 侧未实现
5. **冗余标识**：`isDesktop`/`isMobile` 可从 `os` 推导，前端有自己的判断方式

## Solution

精简接口至实际需要的 12 个方法/属性，删除 7 个未使用项，改造 3 个不可靠实现。

### 最终接口定义

```typescript
export interface IPlatform {
  // ====== 平台标识 ======
  os: 'windows' | 'macos' | 'linux' | 'ios' | 'android' | 'web';
  version: string;

  // ====== 核心能力 ======
  storage: ISecureStorage;
  getUdid(): Promise<string>;

  // ====== 跨平台能力 ======
  openExternal(url: string): Promise<void>;
  writeClipboard(text: string): Promise<void>;
  readClipboard(): Promise<string>;
  syncLocale(locale: string): Promise<void>;

  // ====== 桌面专属（可选）======
  updater?: IUpdater;
  reinstallService?(): Promise<void>;
  getPid?(): Promise<number>;

  // ====== 诊断（可选）======
  uploadLogs?(params: {
    email?: string | null;
    reason: string;
    failureDurationMs?: number;
    platform?: string;
    version?: string;
    feedbackId?: string;
  }): Promise<{ success: boolean; error?: string }>;
}
```

### 删除项

| 删除项 | 理由 |
|--------|------|
| `isDesktop` | 可从 `os` 推导，前端有自己的判断方式 |
| `isMobile` | 同上 |
| `nativeExec` | 通用 IPC 口子过于宽泛，替换为具体的 `reinstallService()` |
| `showToast` | 零消费者，MUI Alert store 已覆盖 |
| `getLocale` | 零消费者，i18next 已管理语言状态 |
| `exit` | 零消费者 |
| `debug` / `warn` | 零消费者，纯 console wrapper 无接口层意义 |

### 各平台实现方案

| 方法 | Tauri | Capacitor | Web |
|------|-------|-----------|-----|
| `os` | ✅ `invoke('get_platform_info')` | ✅ `Capacitor.getPlatform()` | ✅ 硬编码 `'web'` |
| `version` | ✅ `invoke('get_platform_info')` | ✅ `K2Plugin.checkReady()` | ✅ 硬编码 `'0.0.0'` |
| `storage` | ✅ webSecureStorage (AES-256-GCM) | ✅ 同左 | ✅ 同左 |
| `getUdid()` | ✅ `invoke('get_udid')` → daemon | ✅ `K2Plugin.getUDID()` | ❌ 不适用 |
| `openExternal()` | **改造** → `@tauri-apps/plugin-opener` `openUrl()` | **新增** → `@capacitor/browser` `Browser.open()` | ✅ `window.open(_blank)` |
| `writeClipboard()` | **改造** → `@tauri-apps/plugin-clipboard-manager` `writeText()` | **改造** → `@capacitor/clipboard` `Clipboard.write()` | ✅ `navigator.clipboard`（浏览器环境 OK） |
| `readClipboard()` | **改造** → `@tauri-apps/plugin-clipboard-manager` `readText()` | **改造** → `@capacitor/clipboard` `Clipboard.read()` | ✅ `navigator.clipboard` |
| `syncLocale()` | **新增** → `invoke('sync_locale')` → 更新 tray 菜单文本 | **新增** → no-op（移动端无 tray） | no-op |
| `updater?` | ✅ `updater.rs` 已实现 | TODO — 待设计（Android 侧载/路由器 OTA） | ❌ 不适用 |
| `reinstallService?()` | **改造** → `invoke('admin_reinstall_service')` | ❌ 不适用 | ❌ 不适用 |
| `getPid?()` | **新增** → `invoke('get_pid')` → Rust `std::process::id()` | ❌ 不适用 | ❌ 不适用 |
| `uploadLogs?()` | **待实现** → 收集 daemon 日志 + 打包上传 Cloud API | **待实现** → 收集 NE/K2Plugin 日志 + 上传 | ❌ 不适用 |

### 新增依赖

| 平台 | 依赖 | 用途 |
|------|------|------|
| Tauri | `@tauri-apps/plugin-opener` | `openExternal` |
| Tauri | `@tauri-apps/plugin-clipboard-manager` | `writeClipboard` / `readClipboard` |
| Capacitor | `@capacitor/browser` | `openExternal` |
| Capacitor | `@capacitor/clipboard` | `writeClipboard` / `readClipboard` |

### 附带修复

| 文件 | 问题 | 修复 |
|------|------|------|
| `ProHistory.tsx:80` | 直接用 `navigator.clipboard.writeText()` | 改为 `_platform.writeClipboard()` |
| `tauri-k2.ts:11` | `import { open as shellOpen } from '@tauri-apps/plugin-shell'` | 改用 opener 后删除 shell 依赖 |

## Implementation Steps

### Phase 1: 接口精简 + 删除死代码
1. 更新 `kaitu-core.ts` 中 `IPlatform` 接口定义
2. 删除 `isDesktop`, `isMobile`, `nativeExec`, `showToast`, `getLocale`, `exit`, `debug`, `warn` 从接口
3. 重命名 `uploadServiceLogs` → `uploadLogs`
4. 更新三个桥实现（tauri-k2.ts, capacitor-k2.ts, web-platform.ts）删除对应方法
5. 更新 `ServiceAlert.tsx` 从 `nativeExec('admin_reinstall_service')` 改为 `reinstallService()`
6. 更新所有测试文件

### Phase 2: 原生插件改造
7. Tauri: 安装 plugin-opener + plugin-clipboard-manager，更新 capabilities
8. Capacitor: 安装 @capacitor/browser + @capacitor/clipboard
9. 更新 tauri-k2.ts: openExternal 用 opener，clipboard 用 clipboard-manager
10. 更新 capacitor-k2.ts: openExternal 用 Browser，clipboard 用 Clipboard
11. 修复 ProHistory.tsx 直接 navigator.clipboard 调用

### Phase 3: 新能力实现
12. Tauri: 实现 `sync_locale` IPC → tray 菜单更新
13. Tauri: 实现 `get_pid` IPC → `std::process::id()`
14. i18n 切换时调用 `_platform.syncLocale(locale)`

### Phase 4: 待后续实现（不阻塞本次）
- `uploadLogs` Tauri/Capacitor 实现
- `updater` Capacitor (Android 侧载) / Router (OTA)

## Acceptance Criteria

- [ ] IPlatform 接口从 19 个方法/属性精简到 12 个
- [ ] 零消费者的方法全部从接口和实现中删除
- [ ] `nativeExec` 替换为 `reinstallService`，ServiceAlert 调用正常
- [ ] Tauri clipboard 使用 plugin-clipboard-manager（Windows 不再有 focus bug）
- [ ] Capacitor clipboard 使用 @capacitor/clipboard（Android 正常工作）
- [ ] Tauri openExternal 使用 plugin-opener
- [ ] Capacitor openExternal 使用 @capacitor/browser
- [ ] `ProHistory.tsx` 改为使用 `_platform.writeClipboard()`
- [ ] `syncLocale` Tauri 侧实现，tray 菜单语言跟随 webapp 切换
- [ ] `getPid` Tauri 侧实现，传入 k2 daemon 用于进程监控
- [ ] `uploadServiceLogs` 重命名为 `uploadLogs`，消费者更新
- [ ] 所有现有测试通过
- [ ] TypeScript 编译无错误

## Version History

- v1 (2026-02-18): Initial spec — interface cleanup + native plugin migration
