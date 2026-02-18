# Feature: Dashboard + VPN Connection UI

## Meta

| Field     | Value                |
|-----------|----------------------|
| Feature   | dashboard-vpn-ui     |
| Version   | v1                   |
| Status    | implemented          |
| Created   | 2026-02-18           |
| Updated   | 2026-02-18           |

## Overview

Dashboard 是 Kaitu VPN 客户端的主页面，承载 VPN 连接控制的全部核心交互。页面采用三段式布局：
顶部为可折叠的连接控制区（大圆形按钮 / 紧凑 Switch），中部为云节点选择列表，底部为可展开的
高级设置面板（代理规则、DNS 模式等）。

连接按钮是整个 UI 的视觉核心，采用 220px 圆形设计（ExpressVPN 风格），通过 gradient + glow +
CSS 动画实现 6 种 ServiceState 的差异化视觉反馈。按钮支持在 connecting/reconnecting 状态下
点击取消，hover 时显示停止提示，error 状态可一键重连。连接区支持折叠为紧凑模式（iOS 风格
list item + Switch），桌面端默认展开，路由器模式默认折叠。

VPN 状态通过 2 秒轮询机制从后端获取，经 bridge 层 `transformStatus()` 归一化后写入 Zustand
VPN Store。Store 支持乐观更新（Optimistic Update）以提供即时 UI 反馈，并有 5 秒超时保护和
3 秒防抖机制防止状态抖动。Dashboard 页面作为 keep-alive tab 始终保持 mount，通过
dashboard.store 持久化滚动位置和高级设置展开状态。

## Product Requirements

- PR1: 用户可通过大圆形按钮一键连接/断开 VPN，按钮状态实时反映 VPN 连接状态
- PR2: 用户可从云节点列表选择目标服务器，列表显示国旗、名称、国家、负载
- PR3: 连接按钮支持在 connecting/reconnecting 状态取消连接（点击即断开）
- PR4: error 状态下用户可一键重连，无需先断开再连接
- PR5: 提供 global/chnroute 两种代理规则切换，VPN 运行时禁止修改
- PR6: 连接区可在完整大按钮和紧凑 Switch 模式之间切换
- PR7: 未登录用户看到引导登录的 empty state，登录后自动加载云节点
- PR8: 节点列表支持手动刷新、自动 5 分钟刷新、登录后刷新、服务恢复后刷新
- PR9: Service 进程不可达超过 10 秒时，Dashboard 降级为半透明 + 禁止交互
- PR10: 高级设置（代理规则、Anonymity、DNS 模式）在底部可折叠面板中，不干扰核心操作

## Technical Decisions

### TD1: 状态轮询 vs 事件推送

采用 2 秒 `setInterval` 轮询 `_k2.run('status')`，而非 WebSocket/SSE 事件推送。
原因：daemon HTTP API 不支持长连接；移动端 K2Plugin 无事件通道；轮询对 localhost
请求开销可忽略（<1ms RTT）。轮询在 `initializeVPNStore()` 中启动，全局单实例。

### TD2: 乐观更新 + 超时保护

用户点击连接/断开时立即 `setOptimisticState()` 设置 UI 状态（`localState`），不等
后端响应。后端轮询到匹配状态后通过 `isValidTransition()` 验证并清除乐观状态。
5 秒超时保护确保乐观状态不会永久卡住。3 秒防抖防止 `connected → reconnecting`
抖动（wire 短暂断开 < 3s 不反映到 UI）。

### TD3: Bridge `transformStatus()` 归一化

后端原始 state 不直接透传到 webapp。每个 bridge（Tauri/Capacitor）必须实现
`transformStatus()` 将后端 state 归一化为 `StatusResponseData`：
- Daemon `"stopped"` → `"disconnected"`
- `disconnected + error` → `"error"`（bridge 合成）
- `connected_at` → `startAt`（Unix seconds）
- `retrying`、`networkAvailable` 字段补全

### TD4: 连接按钮视觉状态模型

4 种视觉状态 `VisualStatus`（与 6 种 `ServiceState` 多对一映射）：
- `connected` — 绿色 gradient + 稳定 glow
- `transitioning` — 橙色 gradient + pulse 动画（connecting/reconnecting/disconnecting/error+retrying）
- `disconnected` — 蓝色 gradient + breathe 动画
- `stop` — 红色 gradient（仅 hover 时在 connected/connecting/reconnecting 上显示）

### TD5: CollapsibleConnectionSection 折叠策略

折叠状态存储在 `layout.store` 的 `connectionButtonCollapsed`，桌面端默认展开，
路由器模式（`VITE_CLIENT_IS_ROUTER=true`）默认折叠。折叠/展开通过 MUI `Collapse`
动画切换，300ms 过渡。error 信息在折叠模式下显示为紧凑的 InlineErrorBar。

### TD6: 云节点列表 SWR 缓存策略

`CloudTunnelList` 采用 stale-while-revalidate：优先返回 `cacheStore` 缓存数据（TTL 10s），
同时后台请求新数据。加载失败时自动指数退避重试（3s/6s/12s/24s/48s，最多 5 次）。
列表在以下事件后自动刷新：登录成功、服务连接恢复、5 分钟定时器。

### TD7: Config 组装 — 最小化 ClientConfig

Dashboard 只组装最小 `ClientConfig`（`server` + `rule.global`），Go 侧
`config.SetDefaults()` 填充其余默认值。避免前端维护完整 config 的复杂性。

### TD8: Keep-alive Tab + 滚动位置恢复

Dashboard 作为 keep-alive tab 始终 mount。`dashboard.store` 持久化 `scrollPosition` 和 `advancedSettingsExpanded`。keep-alive 架构详见 [layout-navigation.md](layout-navigation.md) PR-3。

## Key Files

| File | Description |
|------|-------------|
| `webapp/src/pages/Dashboard.tsx` | Dashboard 主页面：三段式布局（连接控制 + 节点列表 + 高级设置），组装 ClientConfig，处理连接/断开逻辑 |
| `webapp/src/components/ConnectionButton.tsx` | 220px 圆形连接大按钮：6 种 ServiceState → 4 种 VisualStatus，gradient/glow/pulse/breathe 动画，hover 停止提示 |
| `webapp/src/components/CompactConnectionButton.tsx` | 紧凑连接按钮（iOS 风格 list item）：左侧国旗+服务器名、中间 StatusDot+状态文字、右侧 Switch 开关 |
| `webapp/src/components/CollapsibleConnectionSection.tsx` | 折叠控制区容器：管理 ConnectionButton/CompactConnectionButton 切换，InlineErrorBar 错误提示 |
| `webapp/src/components/CloudTunnelList.tsx` | 云节点选择列表：Cloud API 数据加载、SWR 缓存、自动刷新、排序、Radio 选择、VerticalLoadBar 负载指示 |
| `webapp/src/components/VerticalLoadBar.tsx` | 节点负载条：4px 宽竖向进度条，绿/黄/红三色编码（<50/<80/>=80） |
| `webapp/src/stores/vpn.store.ts` | VPN 状态 Store：轮询调度、乐观更新、防抖、派生状态计算（isConnected/isError/isRetrying 等）、Service 可达性跟踪 |
| `webapp/src/stores/dashboard.store.ts` | Dashboard 持久化 Store：advancedSettingsExpanded、scrollPosition |
| `webapp/src/stores/layout.store.ts` | Layout Store：connectionButtonCollapsed 状态、桌面/移动/路由器模式检测 |
| `webapp/src/services/control-types.ts` | 控制协议类型：ServiceState、StatusResponseData、ControlError、错误码常量、getErrorI18nKey() |
| `webapp/src/core/polling.ts` | 状态轮询 Hook：2s 间隔 `_k2.run('status')`，连接状态变化回调，`pollStatusOnce()` 手动刷新 |
| `webapp/src/theme/colors.ts` | 主题色配置：APP_COLORS（light/dark）、getStatusGradient()、getStatusShadow()、getStatusColor() |
| `webapp/src/utils/tunnel-sort.ts` | 节点排序：按 RouteQuality 降序 → load 升序，当前 quality 均为 0（无 evaluation） |
| `webapp/src/config/apps.ts` | 应用配置：proxyRule feature flag（visible + defaultValue）控制代理规则选择器显示 |
| `webapp/src/services/api-types.ts` | API 类型：Tunnel、SlaveNode（country/load/isAlive）、TunnelListResponse |
| `webapp/src/utils/country.ts` | 国家工具：getFlagIcon() SVG 国旗、getCountryName() 国家名称 |

## Data Flow

### VPN 状态数据流

```
Backend (daemon/engine)
  ↓  raw status JSON (state/error/connected_at/...)
Bridge (tauri-k2.ts / capacitor-k2.ts)
  ↓  transformStatus() → StatusResponseData
VPN Store (vpn.store.ts)
  ↓  initializeVPNStore() 2s 轮询
  ↓  setStatus() + 乐观更新合并 + 防抖
  ↓  computeDerivedState()
useVPNStatus() Hook
  ↓  serviceState, isConnected, isError, isRetrying, ...
Dashboard.tsx + ConnectionButton.tsx
  ↓  UI 渲染
```

### 连接操作数据流

```
User clicks ConnectionButton
  ↓
Dashboard.handleToggleConnection()
  ↓  setOptimisticState('connecting'/'disconnecting')  → UI 立即响应
  ↓  assembleConfig() → { server, rule: { global } }
  ↓  window._k2.run('up'/'down', config)
  ↓
Backend processes → state changes
  ↓
VPN Store 轮询 picks up new state
  ↓  isValidTransition() → 清除乐观状态
  ↓
UI reflects real backend state
```

### 节点列表数据流

```
CloudTunnelList mount
  ↓  check cacheStore('api:tunnels')
  ├─ cache hit → 立即渲染 + 后台 revalidate
  └─ cache miss → cloudApi.get('/api/tunnels/k2v4')
  ↓  sortTunnelsByRecommendation() → 按 quality/load 排序
  ↓  渲染 List + Radio 选择
  ↓
User selects tunnel → Dashboard.setSelectedCloudTunnel()
  ↓  activeTunnelInfo = { domain, name, country }
  ↓  传递给 CollapsibleConnectionSection 显示
```

## State Machine

### ServiceState 状态机

```
                    ┌──────────────────────────────────┐
                    │                                  │
           ┌───────▼───────┐                          │
           │  disconnected  │◄─── user 'down'          │
           └───────┬───────┘                          │
                   │ user 'up'                        │
                   │ setOptimisticState('connecting')   │
           ┌───────▼───────┐                          │
           │  connecting    │────► cancel → 'down'     │
           └───────┬───────┘      setOptimistic-      │
                   │              State('disconnecting')│
                   │ backend reports 'connected'       │
           ┌───────▼───────┐                          │
           │   connected    │                          │
           └───┬───────┬───┘                          │
               │       │                              │
     network   │       │ user 'down'                  │
     change    │       │ setOptimisticState            │
               │       │ ('disconnecting')             │
       ┌───────▼──┐ ┌──▼──────────┐                   │
       │reconnect-│ │disconnecting│                   │
       │ing       │ │(UI only)    │───────────────────┘
       └───────┬──┘ └─────────────┘
               │ wire rebuild
               │ success/fail
       ┌───────▼───────┐
       │ connected /   │
       │ error         │
       └───────────────┘

  error 状态（bridge 合成：disconnected + lastError）：
  - isRetrying=true  → 自动重连中（脉冲动画）
  - isRetrying=false → 需用户操作（点击重连）
```

### 视觉状态映射

| ServiceState | VisualStatus | 颜色 | 动画 | 按钮图标 |
|-------------|-------------|------|------|---------|
| `disconnected` | `disconnected` | 蓝色 info | breathe 3s | PlayArrow |
| `connecting` | `transitioning` | 橙色 warning | pulse 2s | CircularProgress |
| `connected` | `connected` | 绿色 success | 无 | Stop |
| `reconnecting` | `transitioning` | 橙色 warning | pulse 2s | CircularProgress |
| `disconnecting` | `transitioning` | 橙色 warning | pulse 2s | CircularProgress |
| `error` (retrying) | `transitioning` | 橙色 warning | pulse 2s | CircularProgress |
| `error` (no retry) | `disconnected` | 蓝色 info | breathe 3s | PlayArrow |
| hover on connected | `stop` | 红色 error | 无 | Stop |
| hover on connecting | `stop` | 红色 error | 无 | Stop |

## Constants

| Constant | Value | Location | Description |
|----------|-------|----------|-------------|
| `POLL_INTERVAL_MS` | 2000 | vpn.store.ts | VPN 状态轮询间隔 |
| `OPTIMISTIC_TIMEOUT_MS` | 5000 | vpn.store.ts | 乐观更新超时保护 |
| `STATE_DEBOUNCE_MS` | 3000 | vpn.store.ts | 连接抖动防抖时间 |
| `SERVICE_FAILURE_THRESHOLD_MS` | 10000 | vpn.store.ts | Service 不可达降级阈值 |
| `ConnectionButton.size` | 220 | ConnectionButton.tsx | 大按钮直径（px） |
| `DESKTOP_BREAKPOINT` | 768 | layout.store.ts | 桌面/移动布局断点（px） |
| Cache TTL (tunnels) | 10s | CloudTunnelList.tsx | SWR 缓存有效期 |
| Auto-refresh interval | 5min | CloudTunnelList.tsx | 节点列表自动刷新间隔 |
| Retry backoff | 3s-48s | CloudTunnelList.tsx | 加载失败指数退避（5 次上限） |

## Acceptance Criteria

- [ ] AC1: 断开状态下选择节点后点击按钮，VPN 进入 connecting → connected
- [ ] AC2: connected 状态下点击按钮，VPN 进入 disconnecting → disconnected
- [ ] AC3: connecting 状态下点击按钮可取消连接，VPN 回到 disconnected
- [ ] AC4: error 状态下点击按钮直接重连（不先 disconnect），进入 connecting
- [ ] AC5: 未选择节点时，连接按钮 disabled 且显示 "选择服务器" 提示
- [ ] AC6: VPN 运行时节点列表禁止选择（disabled），高级设置禁止修改
- [ ] AC7: 按钮 hover 在 connected/connecting/reconnecting 状态显示红色停止提示
- [ ] AC8: 折叠模式下显示紧凑 Switch + 服务器名 + 状态指示器，功能等同大按钮
- [ ] AC9: 折叠/展开切换平滑动画（300ms Collapse），状态在 layout.store 持久化
- [ ] AC10: 云节点列表认证用户可见，未认证显示登录引导 empty state
- [ ] AC11: 节点列表 SWR 缓存命中时即时渲染 + 后台刷新；缓存 miss 时显示 loading
- [ ] AC12: 节点列表加载失败自动指数退避重试（最多 5 次），支持手动刷新
- [ ] AC13: Service 不可达超过 10 秒后 Dashboard 降级（opacity:0.5 + pointerEvents:none）
- [ ] AC14: 代理规则 global/chnroute 切换正确组装到 ClientConfig.rule.global
- [ ] AC15: 乐观更新 5 秒超时后自动清除，UI 回落到真实后端状态
- [ ] AC16: 连接抖动（connected→reconnecting <3s 恢复）被防抖过滤，UI 不闪烁
- [ ] AC17: Tab 切换后返回 Dashboard，滚动位置和高级设置展开状态恢复
- [ ] AC18: error 状态下折叠模式显示 InlineErrorBar，包含 i18n 错误文案（基于 error.code）
- [ ] AC19: 节点列表显示 VerticalLoadBar 负载指示器（绿<50/黄<80/红>=80）
- [ ] AC20: error+retrying 状态视觉等同 reconnecting（脉冲动画），networkAvailable=false 时显示 "等待网络"
