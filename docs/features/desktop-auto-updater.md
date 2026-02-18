# Feature: Desktop Auto-updater

## Meta

| Field     | Value                    |
|-----------|--------------------------|
| Feature   | desktop-auto-updater     |
| Version   | v1                       |
| Status    | implemented              |
| Created   | 2026-02-18               |
| Updated   | 2026-02-18               |

## Overview

桌面端应用自动更新。Tauri v2 updater 插件 + CloudFront CDN 分发 + minisign 签名验证。
启动后 5 秒自动检查更新，发现新版本后通过 Tauri 事件通知前端，用户可选择立即更新（下载 + 安装 + 重启）或稍后（关闭通知条）。

整个更新流程：
```
CI build → 签名产物上传 S3 → publish-release.sh 生成 latest.json → 上传 S3/CloudFront
  → 客户端 updater 轮询 latest.json → 版本比较 → emit "update-available" → 前端通知
  → 用户点击 "Update Now" → download_and_install → app.restart()
```

## Product Requirements

### P0: 启动自动检查
- 应用启动 5 秒后自动向 CDN endpoints 检查更新
- 发现新版本 → emit `update-available` 事件到前端
- 检查失败（网络错误等）仅 log warning，不影响应用使用

### P0: 用户通知 + 安装
- 前端收到更新通知 → 顶部蓝色通知条显示新版本号
- "Update Now" 按钮：下载 + 安装 + 自动重启
- "Later" 按钮：关闭通知条，本次会话不再提示
- 安装过程中按钮显示 "Installing..." 并禁用交互

### P0: 签名验证
- 所有更新包必须通过 minisign 签名验证
- 公钥编译进客户端（`tauri.conf.json` `plugins.updater.pubkey`）
- 签名不匹配 → 拒绝安装

### P1: 双 CDN 容灾
- 主端点：CloudFront（`d13jc1jqzlg4yt.cloudfront.net`）
- 备端点：S3 直连（`d0.all7.cc`）
- Tauri updater 按顺序尝试，第一个成功即可

### P2: 手动检查
- `check_update_now` IPC command 支持前端手动触发检查
- 返回新版本号或 null

## Technical Decisions

### TD1: Tauri v2 updater 插件
- 使用 `tauri-plugin-updater` v2，不自建更新逻辑
- 插件处理：manifest 下载、版本比较、签名验证、差异下载、安装
- 通过 `UpdaterExt` trait 在 Rust 侧构建 updater 实例

### TD2: 更新 manifest 格式（latest.json）
- 由 `scripts/publish-release.sh` 手动生成，非 CI 自动发布
- 两份 manifest 文件（对应两个 CDN 源）：
  - `cloudfront.latest.json` — 下载 URL 指向 CloudFront
  - `d0.latest.json` — 下载 URL 指向 S3 直连
- manifest 结构：
```json
{
  "version": "0.4.0",
  "notes": "See https://github.com/.../releases/tag/v0.4.0",
  "pub_date": "2026-02-18T00:00:00.000Z",
  "platforms": {
    "darwin-universal": {
      "url": "https://d13jc1jqzlg4yt.cloudfront.net/kaitu/desktop/0.4.0/Kaitu.app.tar.gz",
      "signature": "<minisign base64 sig>"
    },
    "windows-x86_64": {
      "url": "https://d13jc1jqzlg4yt.cloudfront.net/kaitu/desktop/0.4.0/Kaitu_0.4.0_x64-setup.exe",
      "signature": "<minisign base64 sig>"
    }
  }
}
```

### TD3: 平台更新产物
| 平台 | Installer | Auto-update 产物 | 签名文件 |
|------|-----------|-----------------|---------|
| macOS (Universal) | `.pkg` | `.app.tar.gz` | `.app.tar.gz.sig` |
| Windows (x64) | NSIS `.exe` | NSIS `.exe` (same) | `.exe.sig` |

- macOS: `tauri build` 生成 `.app.tar.gz`（updater 专用） + `.app.tar.gz.sig`
- Windows: NSIS installer `.exe` 本身即 updater 产物 + `.exe.sig`
- `createUpdaterArtifacts: true` in `tauri.conf.json` 启用产物生成

### TD4: 签名机制
- **Update 签名**：minisign（`TAURI_SIGNING_PRIVATE_KEY` + `TAURI_SIGNING_PRIVATE_KEY_PASSWORD`），CI 环境变量注入
- **代码签名**：macOS = Developer ID Application（Apple notarization）；Windows = SQS 签名服务（kaitu-signer）
- 两种签名独立：minisign 保护更新完整性，代码签名满足 OS 安全策略
- `.exe.sig` 是 Tauri update signature（minisign），不是 Windows code signature

### TD5: 前端集成架构
- **不在 tauri-k2.ts 注入 `_platform.updater`**：当前实现中 `tauri-k2.ts` 的 `injectTauriGlobals()` 没有设置 `updater` 字段
- Rust 侧 `start_auto_updater()` 直接 emit Tauri 事件 `update-available`
- `useUpdater` hook 通过 `window._platform?.updater` 读取状态。若无 updater（当前 Tauri 桥未注入），hook 返回空状态
- `UpdateNotification` 组件条件渲染：`!isUpdateDownloaded` → return null
- 三个 IPC commands 直接暴露给前端：`check_update_now`、`apply_update_now`、`get_update_status`

### TD6: Rust 侧状态管理
- `HAS_PENDING_UPDATE: AtomicBool` — 全局原子标志，记录是否有待安装更新
- `apply_update_now()` 内部重新 `check().await` 获取最新 update 对象（非缓存），然后 `download_and_install` + `app.restart()`
- `install_pending_update()` — 退出时回调（预留，当前仅 emit 事件）

### TD7: 启动延迟
- 5 秒延迟（`tokio::time::sleep(Duration::from_secs(5))`）让应用完成初始化后再检查
- 不阻塞主线程 — `tauri::async_runtime::spawn` 异步执行

## Key Files

### Rust 侧
| 文件 | 作用 |
|------|------|
| `desktop/src-tauri/src/updater.rs` | 更新模块：`start_auto_updater()`、3 个 IPC commands |
| `desktop/src-tauri/src/main.rs` | 插件注册 + setup 中调用 `start_auto_updater()` |
| `desktop/src-tauri/tauri.conf.json` | updater 端点 + pubkey + `createUpdaterArtifacts` |
| `desktop/src-tauri/capabilities/default.json` | `updater:default` 权限声明 |
| `desktop/src-tauri/Cargo.toml` | `tauri-plugin-updater = "2"` 依赖 |

### Webapp 侧
| 文件 | 作用 |
|------|------|
| `webapp/src/hooks/useUpdater.ts` | 通用更新 hook，读取 `_platform.updater` |
| `webapp/src/components/UpdateNotification.tsx` | 更新通知 UI 组件（固定顶部蓝色条） |
| `webapp/src/types/kaitu-core.ts` | `IUpdater` + `UpdateInfo` 接口定义 |
| `webapp/src/App.tsx` | 挂载 `<UpdateNotification />` |
| `webapp/src/i18n/locales/*/startup.json` | i18n keys: `app.updateNow`、`app.readyToInstall`、`app.installing`、`app.later` |

### CI / 发布
| 文件 | 作用 |
|------|------|
| `.github/workflows/release-desktop.yml` | CI 构建：签名 + 产物上传 S3 |
| `scripts/publish-release.sh` | 生成 latest.json + 上传 S3 + 创建 GitHub Release |
| `scripts/build-macos.sh` | macOS 构建：收集 `.app.tar.gz` + `.sig` 更新产物 |

## IPC Commands

| Command | 参数 | 返回值 | 说明 |
|---------|------|--------|------|
| `check_update_now` | 无 | `Ok(Some(version))` / `Ok(None)` / `Err(msg)` | 手动检查更新 |
| `apply_update_now` | 无 | 不返回（重启） / `Err(msg)` | 下载 + 安装 + 重启 |
| `get_update_status` | 无 | `bool` | 是否有待安装更新 |

## Update Flow

```
┌─────────────┐     5s delay      ┌──────────────┐
│  App Start  │──────────────────→│ Auto-check   │
└─────────────┘                   └──────┬───────┘
                                         │
                              ┌──────────┴──────────┐
                              │ updater.check()     │
                              │ → GET latest.json   │
                              │ → compare versions  │
                              └──────────┬──────────┘
                                         │
                              ┌──────────┴──────────────────┐
                              │                             │
                         No update                    Update found
                              │                             │
                         log + done           ┌─────────────┴─────────────┐
                                              │ HAS_PENDING_UPDATE = true │
                                              │ emit "update-available"   │
                                              └─────────────┬─────────────┘
                                                            │
                                              ┌─────────────┴─────────────┐
                                              │ Frontend: notification    │
                                              │ "v0.5.0 is ready"        │
                                              │ [Update Now] [Later]      │
                                              └──────┬──────────┬────────┘
                                                     │          │
                                              "Update Now"   "Later"
                                                     │          │
                                              ┌──────┴──────┐  dismiss
                                              │ apply_now() │
                                              │ → check()   │
                                              │ → download  │
                                              │ → install   │
                                              │ → restart() │
                                              └─────────────┘
```

## Release Pipeline

> CI 构建、签名、上传、发布的完整流程详见 [cicd-release-pipeline.md](cicd-release-pipeline.md)。
>
> 关键点：CI 构建签名产物 -> 上传 S3 -> `publish-release.sh` 生成 latest.json -> CloudFront CDN 分发。

## Acceptance Criteria

- **AC1**: 应用启动 5 秒后自动检查更新，有新版本时前端收到通知
- **AC2**: 用户点击 "Update Now" 后自动下载、安装、重启到新版本
- **AC3**: 用户点击 "Later" 后通知条消失，本次会话不再弹出
- **AC4**: 更新包 minisign 签名验证失败时拒绝安装
- **AC5**: CloudFront 端点不可用时自动 fallback 到 S3 直连端点
- **AC6**: 网络错误/检查失败仅 log warning，不影响应用正常使用
- **AC7**: `check_update_now` IPC command 可手动触发检查
- **AC8**: macOS `.app.tar.gz` 和 Windows NSIS `.exe` 均可正确更新
- **AC9**: `createUpdaterArtifacts: true` 确保 CI 构建产出签名文件
- **AC10**: `publish-release.sh` 正确生成 latest.json 并上传，包含双平台签名

## Known Gaps

1. **`_platform.updater` 未注入**：`tauri-k2.ts` 的 `injectTauriGlobals()` 没有设置 `updater` 字段。`useUpdater` hook 从 `_platform.updater` 读取，但 Tauri 桥不提供，导致 hook 返回 `isAvailable: false`。当前更新通知依赖 Rust 侧 emit 的 `update-available` 事件 + 前端 Tauri event listener（如果有额外 wiring），或 `_platform.updater` 实际在别处注入。
2. **无定时轮询**：仅启动时单次检查，没有周期性轮询（如每 6 小时）。长时间运行的应用不会再次检查。
3. **`install_pending_update()` 无实际效果**：`on exit` 回调仅 emit 事件，不执行安装。退出时更新安装是预留接口。
4. **`get_pending_update_info()` 无 target version**：返回 `("current_version", "pending")` 硬编码字符串，不记录实际 target 版本。
