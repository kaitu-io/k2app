# Feature: Tauri Updater & Log Upload

## Meta

| Field     | Value                          |
|-----------|--------------------------------|
| Feature   | tauri-updater-and-logs         |
| Version   | v1                             |
| Status    | draft                          |
| Created   | 2026-02-18                     |
| Updated   | 2026-02-18                     |

## Version History

| Version | Date       | Summary                                                   |
|---------|------------|-----------------------------------------------------------|
| v1      | 2026-02-18 | Initial: wire updater + uploadLogs into Tauri bridge      |

## Problem

`platform-interface-cleanup` 确立了 `IPlatform` 接口的 12 个成员，但 Tauri 桥接层仍缺两个可选能力：

1. **updater** — `updater.rs` 已有 Rust IPC 命令（`check_update_now`, `apply_update_now`, `get_update_status`），但 `tauri-k2.ts` 未创建 `tauriPlatform.updater` 对象。现有 `get_update_status` 仅返回 `bool`，缺少 `UpdateInfo`（版本号 + release notes）。`start_auto_updater` 仅启动时检查一次，无周期巡检。前端 `useUpdater()` hook 和 `UpdateNotification.tsx` 已就绪，等待 `window._platform.updater` 注入。

2. **uploadLogs** — 完全缺失。`SubmitTicket.tsx` 和 `Dashboard.tsx` 已有消费代码（`window._platform.uploadLogs!()`），但 Tauri 侧无对应 Rust 模块和 IPC 命令。

## Solution

### Part 1: Updater Bridge (升级 updater.rs + 接入 tauri-k2.ts)

借鉴 kaitu.v0.3 `updater.rs` 的生产模式：

**Rust 侧 (`updater.rs`) 改造：**
- 添加 `UpdateInfo` struct（`current_version`, `new_version`, `release_notes`），序列化为 camelCase
- 使用 `static UPDATE_INFO: Mutex<Option<UpdateInfo>>` + `static UPDATE_READY: AtomicBool` 存储状态
- `get_update_status` 返回 `Option<UpdateInfo>` 而非 `bool`
- `start_auto_updater` 改为 30 分钟周期巡检（5s 初始延迟 → 30min 循环），已 ready 时跳过检查
- `check_download_and_install` 内部：check → download with progress → install → 存 `UPDATE_INFO` + 设 `UPDATE_READY` + emit `"update-ready"` Tauri event（附带 UpdateInfo payload）
- Windows 特殊处理：`update.install()` 后立即 `app.exit(0)`（NSIS 接管）
- macOS/Linux：install 后通知前端，用户点 "Update Now" 调 `apply_update_now` → `app.restart()`
- `install_pending_update(app)` 供 `ExitRequested` 事件调用，退出时自动应用
- `apply_update_now` 改为同步 `fn`（直接 `app.restart()`），不再重复 check+download
- `check_update_now` 保留为手动检查入口（如果已 ready 直接返回状态）

**TypeScript 侧 (`tauri-k2.ts`) 接入：**
- 创建可变的 `updaterState` 对象实现 `IUpdater`
- `isUpdateReady` / `updateInfo` / `isChecking` / `error` 为可变属性
- `applyUpdateNow()` → `invoke('apply_update_now')`
- `checkUpdateManual()` → set `isChecking=true` → `invoke('check_update_now')` → update state → return result
- `onUpdateReady(callback)` → `listen('update-ready', event => callback(event.payload))` 返回 unlisten
- 初始化时 `invoke('get_update_status')` 读取已有状态（应用启动时更新可能已下载完毕）
- 挂载到 `tauriPlatform.updater`

**main.rs 改造：**
- 在 `RunEvent::ExitRequested` 中调用 `updater::install_pending_update(app)`（macOS/Linux 退出时自动应用）

### Part 2: Log Upload (新建 log_upload.rs + 接入 tauri-k2.ts)

借鉴 kaitu.v0.3 `log_upload.rs` 的完整实现，简化 for k2app：

**Rust 侧 (`log_upload.rs`) 新建：**
- `UploadLogParams` struct — 对齐 TS 接口的 6 个字段（camelCase serde）
- `UploadLogResult` struct — `{ success: bool, error: Option<String> }`（精简版，不暴露文件列表给前端）
- 4 个日志源：
  - service log: `/var/log/kaitu/service.log`（macOS/Linux），`%ProgramData%\kaitu\logs\service.log`（Windows）
  - crash log: `panic-*.log` 同目录
  - desktop log: `~/Library/Logs/kaitu/desktop.log`（macOS），`%LocalAppData%\kaitu\logs\desktop.log`（Windows）
  - system log: macOS `log show --last 1d --predicate 'process CONTAINS "kaitu"'`，Windows Event Log 用 native API
- 日志消毒：移除 token/password/secret/Authorization/X-K2-Token
- Gzip 压缩后 PUT 上传到 S3 public bucket（`kaitu-service-logs.s3.ap-northeast-1.amazonaws.com`）
- Slack webhook 通知（含 S3 链接、用户/版本/原因/大小）
- `upload_service_log_command` Tauri IPC 命令，用 `spawn_blocking` 包装（reqwest blocking client）
- Cargo 依赖：`flate2`, `chrono`, `uuid`, `glob`（`reqwest` 已有）

**TypeScript 侧 (`tauri-k2.ts`) 接入：**
- `tauriPlatform.uploadLogs = async (params) => invoke('upload_service_log_command', { params })`
- 返回值对齐 `{ success: boolean; error?: string }`

**main.rs 注册：**
- `mod log_upload;`
- `log_upload::upload_service_log_command` 加入 `generate_handler![]`

### Part 3: Tauri Capabilities

- `default.json` 添加 `"event:default"` 权限（Tauri event listen/emit 需要）

## Technical Decisions

- **复用 kaitu.v0.3 模式**：updater 和 log_upload 均已在生产验证，直接移植核心逻辑，减少设计风险 (v1)
- **S3 public bucket PUT**：无需 AWS credentials，日志桶已有 CORS + public write policy (v1)
- **spawn_blocking for log upload**：log 读取 + gzip + HTTP 均为阻塞操作，不能在 async runtime 中直接执行 (v1)
- **UpdateInfo 存 static Mutex**：跨 async 边界共享状态的标准 Rust 模式，kaitu.v0.3 已验证 (v1)
- **Event bridge for update-ready**：前端通过 `@tauri-apps/api/event` listen Tauri 事件，避免轮询 (v1)
- **Windows NSIS 特殊路径**：`update.install()` 启动 NSIS installer 进程，必须立即 exit 避免双实例 (v1)
- **UploadLogResult 精简**：前端只需 `success` + `error`，文件列表仅 Slack 通知用 (v1)

## Acceptance Criteria

- AC1: `window._platform.updater` 在 Tauri 桌面端存在且实现 `IUpdater` 所有属性和方法 (v1)
- AC2: 应用启动后 5s 自动检查更新，之后每 30 分钟检查一次 (v1)
- AC3: 更新下载完成后，前端收到 `update-ready` 事件，`useUpdater()` 状态更新为 `isUpdateReady: true` + `updateInfo` 填充 (v1)
- AC4: 用户点击 "Update Now" 调用 `applyUpdateNow()` → 应用重启 (v1)
- AC5: `get_update_status` IPC 返回 `Option<UpdateInfo>`（含版本号和 release notes），非 `bool` (v1)
- AC6: macOS/Linux 退出时如有 pending update 则自动应用（`ExitRequested` handler） (v1)
- AC7: Windows 更新安装后立即退出，NSIS installer 接管 (v1)
- AC8: `window._platform.uploadLogs` 在 Tauri 桌面端存在且可调用 (v1)
- AC9: `uploadLogs` 收集 4 个日志源（service, crash, desktop, system），消毒后 gzip 上传 S3 (v1)
- AC10: 上传后发送 Slack 通知，包含 S3 链接、用户信息、版本、原因 (v1)
- AC11: `SubmitTicket.tsx` mount 时调用 `uploadLogs` 成功上传（不崩溃、不报错） (v1)
- AC12: `Dashboard.tsx` 故障上报路径调用 `uploadLogs` 成功 (v1)
- AC13: 所有新 Rust 代码通过 `cargo check` 无警告，`cargo test` 通过 (v1)
- AC14: tauri-k2.test.ts 新增 updater 和 uploadLogs 测试覆盖 (v1)

## Testing Strategy

- **Rust unit tests**: `updater.rs` — `versions_match` 等纯函数（已有），新增 `UpdateInfo` 序列化测试 (v1)
- **Rust unit tests**: `log_upload.rs` — `sanitize_logs` 测试（含各种 token 模式），`generate_s3_key` 格式验证，`compress_gzip` 往返测试 (v1)
- **TypeScript unit tests**: `tauri-k2.test.ts` — mock `invoke` + mock `listen`，验证 updater 状态管理和 uploadLogs 调用 (v1)
- **手动集成测试**: Tauri dev 模式下验证 UpdateNotification 弹出、SubmitTicket 日志上传 (v1)

## Impact Analysis

- **Affected modules**: `desktop/src-tauri/src/updater.rs`（重写），`desktop/src-tauri/src/log_upload.rs`（新建），`desktop/src-tauri/src/main.rs`（注册），`webapp/src/services/tauri-k2.ts`（添加 updater + uploadLogs），`webapp/src/services/__tests__/tauri-k2.test.ts`（新增测试） (v1)
- **Cargo.toml**: 新增 `flate2`, `chrono`, `uuid`, `glob` 依赖 (v1)
- **Capabilities**: `default.json` 添加 event 权限 (v1)
- **Scope**: medium — 2 个 Rust 模块 + 1 个 TS 文件改动，无前端组件变更 (v1)
- **零 breaking change**: 所有新增均为可选成员（`updater?`, `uploadLogs?`），前端消费代码已就绪且有 `?.` 守卫 (v1)
