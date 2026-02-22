# Feature: Service Startup Race Condition Fix

## Meta

| Field     | Value                                    |
|-----------|------------------------------------------|
| Feature   | service-startup-race-fix                 |
| Version   | v1                                       |
| Status    | draft                                    |
| Created   | 2026-02-22                               |
| Updated   | 2026-02-22                               |
| Depends   | macos-pkg-service-lifecycle              |

## Version History

| Version | Date       | Summary                                              |
|---------|------------|------------------------------------------------------|
| v1      | 2026-02-22 | Initial: wait-before-install in ensure_service_running |

## Overview

PKG 安装后存在竞态条件：`postinstall` 以 root 执行 `k2 service install` 后立即 `open Kaitu.app`，但 k2 daemon 尚未完成初始化（端口 `:1777` 未就绪）。Tauri app 启动时 `ensure_service_running()` 单次检测失败 → `ServiceNotRunning` → 触发 `admin_reinstall_service()` → 不必要的 osascript 密码弹窗。

## Problem

`ensure_service_running()` 在 `ServiceNotRunning` 分支直接跳到 `admin_reinstall_service()`，没有容忍正常的服务启动延迟（1-3 秒）。这个竞态不限于 PKG 安装，任何服务刚启动时打开 app 都可能触发。

## Product Requirements

- PR1: PKG 升级安装后不出现不必要的密码弹窗 (v1)
- PR2: 服务正在启动时打开 app，应等待而非立即要求重装 (v1)
- PR3: 服务真的不存在时，用户仍能在合理时间内看到安装提示 (v1)

## Technical Decisions

### TD1: Rust 侧 wait-before-install (scrum-decided)

在 `ensure_service_running()` 检测到 `ServiceNotRunning` 后，先调用 `wait_for_service(8000, 500)` 等待服务启动。只有等待超时后才触发 `admin_reinstall_service()`。

**否决方案**: postinstall 脚本中等待服务就绪。理由：
- 只修复 PKG 一个路径，不覆盖其他竞态场景
- macOS Installer 对 postinstall 脚本超时行为未文档化，hang 风险高
- bash 中做 HTTP 轮询不优雅且难以维护

**否决方案**: 两层都加等待（belt-and-suspenders）。理由：
- Option A 已充分解决，postinstall 等待是死代码
- 增加维护负担，无额外收益

### TD2: 分层超时 (v1)

- **Pre-install wait**: 8 秒（覆盖 k2 的 1-3 秒正常启动 + 慢机器余量）
- **Post-install wait**: 保持现有 5 秒（`admin_reinstall_service` 是同步安装，启动更快）

首次安装场景：8 秒等待 + 密码弹窗。可接受，因为用户预期首次需要安装。
升级场景：1-3 秒等待 → 服务已就绪 → 跳过密码弹窗。

### TD3: 不修改 postinstall (v1)

`scripts/pkg-scripts/postinstall` 保持现状。服务启动竞态由 Rust 消费端统一处理。

## Design

### 修改前流程

```
ensure_service_running(app_version)
  → cleanup_old_kaitu_service()
  → check_service_version()
  → VersionMatch → Ok
  → ServiceNotRunning → admin_reinstall_service() → 密码弹窗  ← BUG
  → VersionMismatch → admin_reinstall_service() → 密码弹窗
```

### 修改后流程

```
ensure_service_running(app_version)
  → cleanup_old_kaitu_service()
  → check_service_version()
  → VersionMatch → Ok
  → ServiceNotRunning
      → wait_for_service(8000, 500)          ← NEW
      → 成功 → check_service_version() 再验  ← NEW
          → VersionMatch → Ok
          → VersionMismatch → admin_reinstall_service()
      → 超时 → admin_reinstall_service()      ← 原有路径
  → VersionMismatch → admin_reinstall_service()
```

### Files to Change

| File | Change |
|------|--------|
| `desktop/src-tauri/src/service.rs` | `ensure_service_running()`: ServiceNotRunning 分支加 wait_for_service + re-check |

### 代码变更

`service.rs` `ensure_service_running()` 中，`ServiceNotRunning` 分支改为：

```rust
VersionCheckResult::ServiceNotRunning => {
    log::info!("[service] Not running, waiting for startup...");
    let ver2 = app_version.clone();
    let wait_result = tokio::task::spawn_blocking(move || {
        if wait_for_service(8000, 500) {
            check_service_version(&ver2)
        } else {
            VersionCheckResult::ServiceNotRunning
        }
    })
    .await
    .map_err(|e| format!("spawn_blocking failed: {}", e))?;

    match wait_result {
        VersionCheckResult::VersionMatch => return Ok(()),
        VersionCheckResult::VersionMismatch { service_version, .. } => {
            log::info!("[service] After wait: mismatch service={}, app={}", service_version, app_version);
        }
        VersionCheckResult::ServiceNotRunning => {
            log::info!("[service] Not reachable after 8s wait");
        }
    }
}
```

然后统一执行 `admin_reinstall_service().await?` + post-install wait。

## Acceptance Criteria

### AC1: PKG 升级后无多余密码弹窗
- postinstall 安装服务后 app 启动
- `ensure_service_running` 等待服务就绪（1-3 秒内）
- 版本匹配 → 直接 Ok，不触发 `admin_reinstall_service`

### AC2: 首次安装仍触发安装提示
- 服务从未安装，app 启动
- `ensure_service_running` 等待 8 秒超时
- 触发 `admin_reinstall_service` → 密码弹窗 → 安装服务

### AC3: 版本不匹配仍触发重装
- 服务运行但版本不同
- 直接（不等待）触发 `admin_reinstall_service`

### AC4: 等待后版本不匹配触发重装
- 服务启动中，等待后版本不匹配
- 触发 `admin_reinstall_service`

### AC5: 诊断日志
- ServiceNotRunning 时日志输出 "waiting for startup"
- wait 成功时日志输出等待耗时
- wait 超时时日志输出 "not reachable after 8s"

## Testing Strategy

- 单元测试：`versions_match()` 已有覆盖，不需新增 (v1)
- 集成测试：手动验证 PKG 安装 → app 启动流程 (v1)
- 验证方法：查看 `~/Library/Logs/io.kaitu.desktop/` 日志确认 wait 行为 (v1)

## Deployment & CI/CD

- 代码变更仅在 `service.rs`，随正常 desktop release 部署 (v1)
- CI `cargo test` 验证编译 (v1)
- 无新依赖、无配置变更 (v1)
