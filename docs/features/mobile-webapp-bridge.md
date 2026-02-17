# Feature: Mobile Webapp Bridge

## Meta

| Field     | Value                                    |
|-----------|------------------------------------------|
| Feature   | mobile-webapp-bridge                     |
| Version   | v2                                       |
| Status    | spec                                     |
| Created   | 2026-02-14                               |
| Updated   | 2026-02-17                               |

## Version History

| Version | Date       | Summary                                                          |
|---------|------------|------------------------------------------------------------------|
| v1      | 2026-02-14 | Initial: NativeVpnClient + K2Plugin TS + Capacitor shell setup   |
| v2      | 2026-02-17 | Align with split globals architecture (_k2 + _platform)          |

## Overview

Webapp 层的移动端桥接：在 Capacitor 环境中注入 `window._k2`（VPN 控制）和
`window._platform`（平台能力），将 K2Plugin Capacitor 方法适配为 split globals 接口。

v1 使用 VpnClient/PlatformApi 抽象工厂模式，已被 webapp v2 的 split globals 架构取代。
v2 遵循 tauri-desktop-bridge 的相同模式：检测平台 → 动态导入桥接模块 → 注入全局对象。

## Problem

webapp v2 迁移到 split globals 后，移动端桥接层断裂：

1. **无 Capacitor 桥接适配器**：`main.tsx` 只有 Tauri 检测和 standalone 兜底。
   Capacitor 环境没有 `__TAURI__`，也没有预注入的 `_k2`/`_platform`，
   因此掉入 standalone 路径——`fetch('/core')` 在移动端无 daemon HTTP 服务，完全不工作。

2. **K2Plugin API 形状不匹配**：K2Plugin 暴露独立方法（`connect`/`disconnect`/`getStatus`/`getVersion`），
   但 `IK2Vpn` 接口要求单一 `run(action, params)` 派发方法。缺少适配层。

3. **状态值 "stopped" 不在 ServiceState 中**：K2Plugin.swift/kt 将 Go 的 `"disconnected"`
   重映射为 `"stopped"`，但 webapp 的 `ServiceState` 类型没有 `"stopped"` 值，
   导致 VPN 存储的所有布尔派生状态均为 false。

4. **StatusResponseData 格式缺失**：K2Plugin 返回最小字段（state, connectedAt, uptimeSeconds, error），
   但 webapp 期望丰富的 `StatusResponseData`（running, networkAvailable, startAt, retrying, error 对象等）。

5. **连接时无配置传递**：Dashboard 调用 `_k2.run('up')` 无参数（桌面端由 daemon 管理配置），
   但移动端 Engine.Start() 需要显式 configJSON。需要 Dashboard 传递配置。

6. **无移动端 `_platform` 注入**：缺少 `window._platform` 的 Capacitor 实现
  （os, isMobile, storage, getUdid 等）。

## Product Requirements

- PR1: `capacitor-k2.ts` 将 K2Plugin 方法适配为 `IK2Vpn.run()` 接口
- PR2: `capacitor-k2.ts` 注入 `window._platform` 含移动端能力（存储、UDID、剪贴板）
- PR3: `main.tsx` 检测 Capacitor 原生环境，在 standalone 兜底之前导入 Capacitor 桥接
- PR4: K2Plugin 状态值直接透传（移除 "disconnected"→"stopped" 重映射），与 webapp ServiceState 对齐
- PR5: 状态格式适配：K2Plugin 最小状态 → webapp `StatusResponseData` 完整格式
- PR6: K2Plugin 事件（vpnStateChange、vpnError）桥接到 VPN 存储更新
- PR7: Dashboard 组装最小 `ClientConfig` 并传递给 `_k2.run('up', config)`

## Technical Decisions

### TD1: Capacitor 检测方式

使用 `@capacitor/core` 的 `Capacitor.isNativePlatform()` 判断是否在原生容器中。
此函数在 Capacitor WebView 中返回 `true`，在浏览器和 Tauri 中返回 `false`。
检测链：`__TAURI__` → `Capacitor.isNativePlatform()` → standalone 兜底。

### TD2: 状态映射修正

移除 K2Plugin.swift 和 K2Plugin.kt 中的 `"disconnected"→"stopped"` 重映射。
Go 引擎的三个状态（`disconnected`、`connecting`、`connected`）直接透传。
webapp 的 `ServiceState` 已包含这三个值。
旧 v1 的 "stopped" 状态是为旧 VpnClient 接口设计的，split globals 不需要。

### TD3: StatusResponseData 适配

K2Plugin 返回 gomobile 最小格式，bridge 负责填充 webapp 缺失字段：
- `running`: 根据 state 推导（connecting/connected = true）
- `networkAvailable`: 默认 true（移动端依赖系统网络检测）
- `startAt`: 从 connectedAt ISO 字符串转换为 Unix seconds
- `error`: 从 plain string 包装为 `{code: 570, message: string}`
- `retrying`: 默认 false（gomobile 引擎不做自动重试）

### TD4: 配置组装

Dashboard 选中隧道后组装最小 `config.ClientConfig` JSON：
```json
{
  "server": "k2v5://udid:token@host:port?addrs=...",
  "rule": { "global": true }
}
```
Go 的 `config.SetDefaults()` 填充所有其他字段。
桌面端：daemon 接受 config 参数（已支持）。移动端：adapter 序列化为 JSON 传给 K2Plugin.connect()。

### TD5: 事件 vs 轮询

K2Plugin 提供 `vpnStateChange` 和 `vpnError` 事件。Capacitor bridge 同时支持：
- 轮询：`run('status')` 每 2 秒（复用 vpn.store 现有逻辑）
- 事件：注册 K2Plugin 监听器，收到事件时立即触发一次额外轮询
这样事件加速状态更新，轮询保证兜底，无需改动 vpn.store 逻辑。

### TD6: registerPlugin 模式（延续 v1）

使用 `registerPlugin('K2Plugin')` from `@capacitor/core`，不用 npm 动态 `import('k2-plugin')`。

### TD7: 本地插件同步陷阱（延续 v1）

`file:` 协议插件被 copy 到 `node_modules/`。编辑后：
```bash
rm -rf node_modules/k2-plugin && yarn install --force && cap sync
```

## Acceptance Criteria

- AC1: Capacitor 环境中 `window._k2.run('status')` 返回 `SResponse<StatusResponseData>` 格式
- AC2: `window._k2.run('up', config)` 调用 K2Plugin.connect() 并传递 config JSON
- AC3: `window._k2.run('down')` 调用 K2Plugin.disconnect()
- AC4: `window._platform.os` 返回 `'ios'` 或 `'android'`，`isMobile` 为 `true`
- AC5: `window._platform.getUdid()` 通过 K2Plugin.getUDID() 获取设备 ID
- AC6: K2Plugin 状态直接透传 Go 引擎值（不再映射为 "stopped"）
- AC7: `getK2Source()` 在 Capacitor 环境返回 `'capacitor'`
- AC8: Standalone 兜底不受影响（回归测试）
- AC9: Dashboard 连接时组装并传递 ClientConfig 到 `_k2.run('up', config)`

## Testing Strategy

- `webapp/src/services/__tests__/capacitor-k2.test.ts` — 桥接适配器单元测试（mock K2Plugin）
- `npx tsc --noEmit` — 类型检查通过
- `npx vitest run` — 全量回归测试
- 设备验证：iOS + Android 通过 debug.html 验证原生桥接

## Key Files

| File | Role |
|------|------|
| `webapp/src/services/capacitor-k2.ts` | NEW: Capacitor 桥接适配器 |
| `webapp/src/services/__tests__/capacitor-k2.test.ts` | NEW: 适配器测试 |
| `webapp/src/main.tsx` | MODIFY: 添加 Capacitor 检测分支 |
| `webapp/src/pages/Dashboard.tsx` | MODIFY: 连接时传递 config |
| `mobile/ios/Plugin/K2Plugin.swift` | MODIFY: 移除 stopped 映射 |
| `mobile/plugins/k2-plugin/android/.../K2Plugin.kt` | MODIFY: 移除 stopped 映射 |
| `mobile/plugins/k2-plugin/src/definitions.ts` | 现有 K2Plugin TS 接口 |
| `mobile/plugins/k2-plugin/src/index.ts` | 现有 registerPlugin 注册 |
