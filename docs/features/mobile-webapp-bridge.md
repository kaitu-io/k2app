# Feature: Mobile Webapp Bridge

## Meta

| Field     | Value                                    |
|-----------|------------------------------------------|
| Feature   | mobile-webapp-bridge                     |
| Version   | v1                                       |
| Status    | implemented                              |
| Created   | 2026-02-14                               |
| Updated   | 2026-02-16                               |

## Version History

| Version | Date       | Summary                                                          |
|---------|------------|------------------------------------------------------------------|
| v1      | 2026-02-14 | Initial: NativeVpnClient + K2Plugin TS + Capacitor shell setup   |

## Overview

Webapp 层的移动端桥接：NativeVpnClient 实现 VpnClient 接口调用 Capacitor K2Plugin，
CapacitorPlatform 实现跨平台能力抽象，以及 Capacitor 项目搭建（与桌面复用同一 webapp dist）。

这是 iOS 和 Android 的共享前置层——两个平台的原生实现都依赖此桥接层。

## Product Requirements

- PR1: NativeVpnClient 实现 VpnClient 接口，通过 K2Plugin Capacitor 方法调用原生层
- PR2: VpnClient factory 自动检测 Capacitor 原生平台，返回 NativeVpnClient
- PR3: Mobile bootstrap 在 React render 之前 await initVpnClient()（异步插件加载）
- PR4: K2Plugin TypeScript 定义（接口 + 事件 + registerPlugin 注册）
- PR5: CapacitorPlatform 实现 PlatformApi（剪贴板、外部浏览器、语言同步）
- PR6: Capacitor 项目结构复用桌面 webapp/dist，零移动端专属 UI 代码
- PR7: Go→JS 键名映射（snake_case→camelCase）+ 状态映射（"disconnected"→"stopped"）在原生桥接层完成

## Technical Decisions

### TD1: registerPlugin 模式

使用 `registerPlugin('K2Plugin')` from `@capacitor/core`，不用 npm 动态 `import('k2-plugin')`。
后者在 WebView 运行时失败。

### TD2: 异步 VpnClient Factory

`initVpnClient()` 使用 dynamic import 加载 NativeVpnClient：
```typescript
const { NativeVpnClient } = await import('./native-client');
```
确保 Capacitor 相关代码不进入桌面 bundle（tree-shaking）。桌面端使用同步 `createVpnClient()`。

### TD3: Capacitor 项目结构

```
mobile/
├── package.json           # Capacitor app (workspace member)
├── capacitor.config.ts    # webDir: '../webapp/dist'
├── plugins/k2-plugin/     # 本地 Capacitor 插件 (file: 协议)
│   ├── src/               # TypeScript 定义 + web stub
│   ├── ios/Plugin/        # K2Plugin.swift
│   └── android/src/       # K2Plugin.kt
├── ios/                   # Xcode 项目
└── android/               # Gradle 项目
```

### TD4: 本地插件同步陷阱

`file:` 协议插件被 copy（不是 symlink）到 `node_modules/`。编辑插件源码后必须：
```bash
rm -rf node_modules/k2-plugin && yarn install --force
```
然后再 `cap sync`，否则部署的是旧代码。

## Acceptance Criteria

- AC1: NativeVpnClient 调用 K2Plugin 完成所有 VPN 操作（connect/disconnect/getStatus/subscribe）
- AC2: `initVpnClient()` 在 Capacitor native 环境返回 NativeVpnClient
- AC3: `main.tsx` 在 `ReactDOM.render()` 之前 await `initVpnClient()`
- AC4: K2Plugin 通过 `registerPlugin` 注册，非 npm 动态导入
- AC5: CapacitorPlatform 处理 clipboard、external browser、locale sync
- AC6: 同一 `webapp/dist/` 部署到 iOS 和 Android，零平台分支代码
- AC7: Go 引擎返回的 snake_case JSON 在原生桥接层转换为 camelCase 再传给 JS
- AC8: 引擎 "disconnected" 状态映射为 webapp "stopped" 状态

## Testing Strategy

- Webapp unit tests: NativeVpnClient mock tests via MockVpnClient (existing test infrastructure)
- TypeScript strict mode: `npx tsc --noEmit` must pass with K2Plugin type definitions
- Capacitor integration: Verified via `debug.html` on iOS + Android devices
- VpnClient factory: Covered by existing webapp test suite (284 tests)
- Build verification: `yarn build` produces dist/ with both index.html and debug.html

## Key Files

| File | Role |
|------|------|
| `webapp/src/vpn-client/native-client.ts` | NativeVpnClient 实现 |
| `webapp/src/vpn-client/index.ts` | VpnClient factory（initVpnClient + createVpnClient） |
| `webapp/src/main.tsx` | Mobile bootstrap（await before render） |
| `webapp/src/platform/capacitor.ts` | CapacitorPlatform 实现 |
| `mobile/plugins/k2-plugin/src/definitions.ts` | K2Plugin TypeScript 接口 |
| `mobile/plugins/k2-plugin/src/index.ts` | registerPlugin 注册 |
| `mobile/plugins/k2-plugin/src/web.ts` | Web 环境 stub（抛 unavailable） |
| `mobile/package.json` | Capacitor 项目依赖 |
| `mobile/capacitor.config.ts` | Capacitor 配置（webDir、StatusBar 等） |
