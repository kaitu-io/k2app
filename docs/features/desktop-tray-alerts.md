# Feature: Desktop Tray & Service Alert

## Meta

| Field     | Value                    |
|-----------|--------------------------|
| Feature   | desktop-tray-alerts      |
| Version   | v1                       |
| Status    | implemented              |
| Created   | 2026-02-18               |
| Updated   | 2026-02-18               |

## Overview

桌面端系统托盘菜单 + 服务状态告警体系。涵盖三个层次：

1. **System Tray (Rust)**：macOS/Windows 系统托盘图标 + 右键菜单，提供窗口显隐、VPN 快捷操作、退出。左键点击切换窗口可见性。
2. **Service Alert Banner (webapp)**：全局顶部横幅，覆盖三种场景——组件初始化中、daemon 进程不可达超过 10 秒、VPN 100 系列网络错误。
3. **Service Error Page (webapp)**：当 daemon 持续不可达时的 fallback 诊断页面，列出可能原因并提供解决方案（重新下载安装包、提交工单）。

辅助组件：

- **AuthGate**：应用启动时的认证状态检查门卫，`isAuthChecking` 时显示 LoadingPage，否则开放访问。
- **ConnectionNotification**：右上角浮动通知芯片，根据 `ControlError.code` 分类显示 info/warning/error 图标 + i18n 文案。
- **Single-instance enforcement**：Tauri 单实例插件确保只有一个 app 窗口运行。
- **Localhost server (port 14580)**：`tauri-plugin-localhost` 解决 WebKit 混合内容阻断问题。

## Product Requirements

- **PR1**: 系统托盘图标始终显示在 macOS 菜单栏 / Windows 系统托盘区
- **PR2**: 左键点击托盘图标切换窗口显示/隐藏（toggle），隐藏时再次点击恢复并聚焦
- **PR3**: 右键菜单提供 Show/Hide、Connect、Disconnect、Quit 四个操作项
- **PR4**: Connect/Disconnect 通过 HTTP 直连 daemon（`POST http://127.0.0.1:1777/api/core`），不依赖 webapp IPC
- **PR5**: Quit 调用 `app.exit(0)` 完全退出应用
- **PR6**: 二次启动应用时，不创建新窗口，而是显示并聚焦已有窗口（single-instance）
- **PR7**: 服务组件初始化中（GeoIP / Rules / Antiblock 加载）时，显示蓝色 info banner，列出正在加载的组件名
- **PR8**: daemon 进程不可达超过 10 秒时，显示红色 serviceFailure banner，带 "Resolve" 按钮和 "More" 链接
- **PR9**: VPN 连接出现 100 系列网络错误时，显示红色 networkError banner
- **PR10**: Banner 显示优先级：initialization > serviceFailure > networkError（同时命中时只显示最高优先级）
- **PR11**: "Resolve" 按钮尝试以管理员权限重装 service（macOS: osascript、Windows: PowerShell RunAs）
- **PR12**: 重装失败或用户取消时导航到 `/service-error` 诊断页面
- **PR13**: `/service-error` 页面提供两种可能原因（自动更新失败、安全软件拦截）和一键下载最新安装包的解决方案
- **PR14**: 应用启动时 Rust 侧自动检查 daemon 版本匹配，不匹配则自动提升权限重装
- **PR15**: ConnectionNotification 根据错误码系列（100-109/110-119/510-519/520-529/570-579）显示不同级别图标

## Technical Decisions

### TD1: Tray Connect/Disconnect 绕过 webapp IPC

托盘菜单的 Connect/Disconnect 直接通过 `reqwest::blocking::Client` 发送 HTTP 请求到 `http://127.0.0.1:1777/api/core`，在 `std::thread::spawn` 中执行，不经过 webapp 或 Tauri IPC 通道。

**原因**：托盘操作必须在窗口隐藏时也能工作。webapp 可能未加载或 webview 处于 suspended 状态，IPC 不可靠。直接 HTTP 是最简单可靠的方式。

**注意**：Connect 只发 `{"action": "up"}`，不携带 config。这意味着如果 daemon 没有缓存上次的配置，connect 可能只能重连到上次的服务器。Disconnect 发 `{"action": "down"}`。

### TD2: Single-instance 通过 tauri-plugin-single-instance

使用 `tauri_plugin_single_instance` 插件。第二次启动时回调 `|app, _args, _cwd|` 会 `show()` + `set_focus()` 已有窗口。不需要自己实现 lock file 或 IPC。

### TD3: Localhost plugin (port 14580) 解决 WebKit 混合内容

WebKit WKWebView 拦截从 HTTP origin 到外部 HTTPS 的 fetch 请求。`tauri-plugin-localhost` 在 `127.0.0.1:14580` 起一个本地 HTTP 服务器来 serve webapp 静态文件，使 webview 有一个真实的 HTTP origin。外部 HTTPS 请求通过 `@tauri-apps/plugin-http` 从 Rust 侧发出（详见 tauri-desktop-bridge spec）。

### TD4: ServiceAlert 三态优先级模型

ServiceAlert 组件在 Layout 中全局渲染（fixed 定位在页面顶部），通过 VPN Store 的派生状态判断显示哪种 banner：

```
优先级：initialization > serviceFailure > networkError

initialization:
  condition: status.initialization && !initialization.ready
  style: 蓝色 (#EFF6FF bg, #2563EB icon)
  icon: InfoIcon (圆形)
  附加信息: 列出正在加载的组件 (geoip, rules, antiblock)
  无按钮

serviceFailure:
  condition: isServiceFailedLongTime (serviceFailedSince > 10 秒)
  style: 红色 (#FEF2F2 bg, #DC2626 icon)
  icon: WarningIcon (三角形)
  按钮: "Resolve" (尝试 admin_reinstall_service) + "More" (导航到 /service-error)

networkError:
  condition: error && isNetworkError(error.code) (code 100-109)
  style: 红色 (#FEF2F2 bg, #DC2626 icon)
  icon: WarningIcon (三角形)
  按钮: "Resolve" + "More"
```

### TD5: Service 不可达判定机制

VPN Store 每 2 秒轮询 `_k2.run('status')`：
- 成功（code=0 + data）→ `setServiceFailed(false)` → 清除 `serviceFailedSince`
- 失败（异常或 code!=0）→ `setServiceFailed(true)` → 首次失败记录 `serviceFailedSince = Date.now()`

派生状态 `isServiceFailedLongTime` 在 `serviceFailedSince` 距今超过 `SERVICE_FAILURE_THRESHOLD_MS`（10000ms）时为 true。`useVPNStatus()` hook 内部有 1 秒 tick 定时器来驱动该计算的更新。

### TD6: Admin 重装 service 的平台实现

**macOS**:
```
osascript -e 'do shell script "/Applications/Kaitu.app/Contents/MacOS/k2 run --install" with administrator privileges'
```
通过 macOS 原生授权对话框请求管理员权限，运行 k2 binary 的 `run --install` 命令安装 launchd service。

**Windows**:
```
Start-Process -FilePath 'k2.exe' -ArgumentList 'run','--install' -Verb RunAs -Wait -WindowStyle Hidden
```
通过 PowerShell `RunAs` 触发 UAC 提权对话框。

用户取消授权时，macOS 返回 error code -128 / "User canceled"，webapp 导航到 `/service-error`。

### TD7: 启动时版本校验 (Rust 侧)

`main.rs` setup 阶段 spawn async task 调用 `ensure_service_running(app_version)`：
1. 清理旧版 kaitu-service（legacy plist/Windows service）
2. `check_service_version()`：调用 `action:version` 获取 daemon 版本
3. 版本比对用 `versions_match()` — 忽略 `+` 后的 build metadata
4. 匹配 → 直接返回
5. 不匹配或未运行 → `admin_reinstall_service()` → 等待 5 秒（500ms 间隔轮询）→ 再次校验

版本校验逻辑已从 webapp VPN Store 中移除，完全由 Rust 处理。

### TD8: ConnectionNotification 错误码分级

| 错误码范围 | NotificationType | 图标 | 语义 |
|-----------|-----------------|------|------|
| 100-109   | warning         | WarningIcon | 网络错误（超时、不可达、DNS、TLS） |
| 110-119   | error           | ErrorIcon   | 服务器错误（不可用、过载、维护） |
| 510-519   | error           | ErrorIcon   | VPN 服务错误（启动/停止/重连失败） |
| 520-529   | warning         | WarningIcon | 网络相关错误 |
| 570-579   | error           | ErrorIcon   | 连接错误（致命/所有地址失败） |
| 其他       | error           | ErrorIcon   | 未分类错误 |

错误文案通过 `getErrorI18nKey(code)` 映射到 `common:errors.*` i18n 键，fallback 到 `error.message` 或 `common:status.error`。

### TD9: AuthGate 简化为开放访问

AuthGate 不再做 service readiness 或 version check 的阻断（这些已移至 Rust 侧和 ServiceAlert）。当前行为：
- `isAuthChecking` 为 true → 显示 LoadingPage（等待 token 验证完成）
- 其他情况 → 直接渲染 children

需要登录的页面使用独立的 `LoginRequiredGuard` 包裹，不由 AuthGate 负责。

## Key Files

| File | Role |
|------|------|
| `desktop/src-tauri/src/tray.rs` | System tray：菜单构建 + 事件处理（Show/Hide, Connect, Disconnect, Quit, 左键 toggle） |
| `desktop/src-tauri/src/main.rs` | App setup：localhost plugin (14580), single-instance, autostart, tray init, service version check |
| `desktop/src-tauri/src/service.rs` | Daemon 通信：`core_action()`, `ping_service()`, `check_service_version()`, `admin_reinstall_service()`, `ensure_service_running()`, `daemon_exec()` IPC |
| `desktop/src-tauri/tauri.conf.json` | Window config (430x956, hidden title, non-maximizable), bundle config, updater endpoints |
| `webapp/src/components/ServiceAlert.tsx` | 服务状态告警 banner：三种 alert type (initialization/serviceFailure/networkError)，Resolve 按钮 + More 链接 |
| `webapp/src/components/__tests__/ServiceAlert.test.tsx` | ServiceAlert 单元测试：可见性、网络错误检测、导航、样式验证 |
| `webapp/src/pages/ServiceError.tsx` | 服务错误诊断页面：可能原因 + 下载解决方案 + 工单入口 |
| `webapp/src/components/AuthGate.tsx` | 认证状态门卫：isAuthChecking → LoadingPage，否则开放访问 |
| `webapp/src/components/ConnectionNotification.tsx` | 连接状态通知芯片：根据错误码分级显示 info/warning/error + i18n 文案 |
| `webapp/src/components/Layout.tsx` | 全局布局：渲染 ServiceAlert（固定顶部，offset sidebar 宽度） |
| `webapp/src/stores/vpn.store.ts` | VPN Store：2 秒轮询 + serviceFailedSince 计时 + isServiceFailedLongTime 派生状态 |
| `webapp/src/services/control-types.ts` | 错误码常量 + `isNetworkError()` / `isServerError()` / `getErrorI18nKey()` 分类函数 |

## Acceptance Criteria

- **AC1**: 系统托盘图标在 macOS 菜单栏 / Windows 系统托盘区可见，使用应用默认图标
- **AC2**: 左键点击托盘图标 toggle 窗口显示/隐藏，隐藏后再点击恢复并获得焦点
- **AC3**: 右键菜单 Show/Hide 与左键点击行为一致（toggle）
- **AC4**: 右键菜单 Connect 发送 `{"action":"up"}` 到 daemon，窗口隐藏时也生效
- **AC5**: 右键菜单 Disconnect 发送 `{"action":"down"}` 到 daemon
- **AC6**: 右键菜单 Quit 退出应用（exit code 0）
- **AC7**: 第二次启动应用时不创建新窗口，而是显示并聚焦已有窗口
- **AC8**: 服务组件初始化期间（geoip/rules/antiblock 加载中）显示蓝色 info banner
- **AC9**: daemon 不可达超过 10 秒后显示红色 serviceFailure banner
- **AC10**: VPN 100 系列网络错误时显示红色 networkError banner
- **AC11**: 三种 banner 同时命中时只显示最高优先级（initialization > serviceFailure > networkError）
- **AC12**: Resolve 按钮触发 `admin_reinstall_service`，macOS 弹出管理员授权对话框
- **AC13**: 用户取消授权或重装失败时导航到 `/service-error` 页面
- **AC14**: `/service-error` 页面的下载按钮通过 `_platform.openExternal` 打开 `https://kaitu.io/install`
- **AC15**: `/service-error` 页面的"问题持续"卡片导航到 `/faq`
- **AC16**: 应用启动时 Rust 侧自动检查 daemon 版本，不匹配时自动提升权限重装
- **AC17**: ConnectionNotification 对 100-109 错误显示 warning 图标，对 510-519/570-579 显示 error 图标
- **AC18**: ConnectionNotification 文案来自 i18n 映射 `common:errors.*`，非原始 message
- **AC19**: AuthGate 在 `isAuthChecking` 时显示 LoadingPage，其他情况直接渲染 children
- **AC20**: ServiceAlert banner 使用 fixed 定位，left 偏移量等于 sidebar 宽度（桌面端），不遮挡侧边栏
