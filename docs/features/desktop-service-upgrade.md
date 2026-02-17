# Feature: Desktop Service Version Check & Upgrade

## Meta

| Field     | Value                                    |
|-----------|------------------------------------------|
| Feature   | desktop-service-upgrade                  |
| Version   | v2                                       |
| Status    | implemented                              |
| Created   | 2026-02-18                               |
| Updated   | 2026-02-18                               |

## Version History

| Version | Date       | Summary                                                          |
|---------|------------|------------------------------------------------------------------|
| v1      | 2026-02-18 | Initial: over-engineered Rust event + frontend UI flow           |
| v2      | 2026-02-18 | Simplified: keep Rust startup auto-install, just add nativeExec bridge |

## Overview

桌面端 k2 service 版本检测与升级。Rust 侧已有完整的启动时自动检测 + 安装流程
（`ensure_service_running` → `admin_reinstall_service`）。运行时 service 故障由
前端 `ServiceAlert` 处理，但 Tauri 桥缺少 `nativeExec` 实现导致"修复"按钮无法工作。

## Problem

### P0: 前端无法触发 service 重装

`tauri-k2.ts` 的 `tauriPlatform` 对象没有实现 `nativeExec` 方法。
Webapp 的 `ServiceAlert.tsx` 调用 `platform.nativeExec('admin_reinstall_service')`，
但 Tauri 桥没有对接，导致点击"修复"按钮回退到 navigate `/service-error`。

## Current State

### 已完成（不需改动）

**Rust 侧**：
- `service.rs`: `ensure_service_running()` — 启动时自动检测版本 + `admin_reinstall_service()` 安装
- `admin_reinstall_service` IPC command — macOS osascript / Windows PowerShell 提权
- `versions_match()` — 版本比较（忽略 build metadata）
- `detect_old_kaitu_service()` / `cleanup_old_kaitu_service()` — 旧 kaitu-service 迁移
- CLI: `k2 run --install` — 统一服务安装入口

**Webapp 侧**：
- `IPlatform.nativeExec` 类型定义 ✓ (`kaitu-core.ts:197`)
- `ServiceAlert.tsx`: 三种告警 + "修复"按钮调用 `nativeExec` ✓
- VPN store: `serviceConnected`, `isServiceFailedLongTime` 检测 ✓
- ServiceError 页面: `/service-error` 回退页面 ✓

### 缺失

- `tauri-k2.ts`: `tauriPlatform` 对象没有 `nativeExec` 方法

## Solution

在 `tauri-k2.ts` 的 `tauriPlatform` 对象中实现 `nativeExec`，透传到 Tauri IPC：

```typescript
nativeExec: async <T = any>(action: string, params?: Record<string, any>): Promise<T> => {
  return invoke<T>(action, params ?? {});
},
```

`admin_reinstall_service` 已是注册的 Tauri IPC command（`main.rs:27`），无需额外 Rust 改动。

## Flow

### 启动时（Rust 自动处理，不变）
```
app 启动 → ensure_service_running(app_version)
  → cleanup_old_kaitu_service()
  → check_service_version()
  → 版本匹配 → Ok
  → 版本不匹配/未运行 → admin_reinstall_service() → osascript 密码框 → 安装
```

### 运行时（前端 ServiceAlert，本次修复）
```
daemon 无响应 >10s → isServiceFailedLongTime → ServiceAlert 显示
  → 用户点"修复" → _platform.nativeExec('admin_reinstall_service')
  → invoke('admin_reinstall_service') → Rust → osascript 密码框 → 安装
  → 安装成功 → daemon 恢复 → ServiceAlert 自动消失
  → 用户取消 → ServiceAlert 保留 → "更多"跳转 /service-error
```

## Acceptance Criteria

### AC1: nativeExec 桥接正常工作
- `window._platform.nativeExec('admin_reinstall_service')` 正确调用 Tauri IPC
- 返回 Promise，resolve 表示成功，reject 表示失败/取消

### AC2: ServiceAlert "修复"按钮触发 service 重装
- daemon 无响应 >10s → ServiceAlert 显示
- 点击"修复" → osascript 弹密码框 → 安装成功 → ServiceAlert 消失

### AC3: 用户取消密码框 → 保持提示
- 取消 osascript → ServiceAlert 继续显示
- "更多"链接跳转到 /service-error

### AC4: 已有功能不被破坏
- 启动时 ensure_service_running 自动安装流程不变
- 旧 kaitu-service 清理不变
- 版本比较逻辑不变
- 其他 _platform 方法不受影响

## Files to Change

| File | Change |
|------|--------|
| `webapp/src/services/tauri-k2.ts` | `tauriPlatform` 加 `nativeExec` 方法 |
| `webapp/src/services/__tests__/tauri-k2.test.ts` | 添加 nativeExec 测试 |
