# Disable Antiblock Relay — Restore config.js Direct Transport

**Date**: 2026-07-17
**Status**: Approved
**Context**: #3288 / #3289 — antiblock relay 节点对 CN 可达率仅 2/21，embedded seed 3/5 IP 被 GFW 封，relay-first 传输反而拖垮墙内用户的 API 可达性。决定关停 relay 使用（代码保留），恢复 config.js 直连为唯一传输。

## 目标

- Webapp 的所有 cloud API 请求回到 **config.js 直连**路径：`antiblock.ts resolveEntry()`（localStorage 缓存 → 7 个 jsdelivr 镜像赛跑拉加密 `config.js` → 兜底 `https://k2.52j.me`）→ 直接 `fetch()`。
- Go 封装的 antiblock relay（`_k2.run('relay-fetch')` / `relay-add-nodes`，Go `wire/relay_manager.go`）**零调用但零删除**。
- 恢复 relay = 改一行常量重发版。

## 非目标

- 不删除任何 relay 代码（TS / Go / Swift / Kotlin / Rust 全保留）。
- 不动 Go 侧：`daemon/api.go` relay-fetch handler、`appext/relay.go`、`K2Plugin.relayFetch` 成为死分支即可。Go core 自身（daemon subs 刷新等）本就不走 RelayManager，无需改动。
- 不改 entry 域名运营策略（config.js 内容更新是运营侧动作，不在本次范围）。

## 方案：单常量 kill-switch，接触点自我短路

### 1. 新文件 `webapp/src/services/relay-flag.ts`

```ts
/**
 * Antiblock relay kill-switch. 2026-07-17 关停（CN 可达率 2/21，见 #3288/#3289）。
 * 恢复 relay：改回 true 重发版。
 */
export const RELAY_ENABLED = false;
```

独立模块（而非塞进 entry-pool.ts）的原因：测试可以 `vi.mock` 翻转它，让既有 relay-first 测试保活，同时新增「默认关」行为测试。

### 2. 接触点改动

| 文件 | 改动 |
|------|------|
| `resolve-and-fetch.ts` | `resolveAndFetch()` relay 分支 gate 为 `if (RELAY_ENABLED && pool.isRelaySupported())`；直连超时改为 `RELAY_ENABLED ? 5000 : 14000`（原 5s 是 relay-first 下给 relay 让预算的产物；关停后直连独享 15s 外层预算，14s 留 1s 余量） |
| `entry-pool.ts` | `addNodes()`、`ensureSeeded()` 顶部 `if (!RELAY_ENABLED) return`——覆盖 `relay-add-nodes` IPC 与启动灌池 |
| `antiblock-seed.ts` | `bootstrapAntiblockSeed()` 顶部 gate——种子 CDN 拉取（版本化 seed `/v/<n>.js`）一并停掉 |
| `main.tsx` | **不动**——`await ensureSeeded()` 变即时返回，`bootstrapAntiblockSeed()` 自我短路 |
| `cloud-api.ts` | **不动**——`addNodes()` 调用自我短路 |

### 3. 行为效果

- 三端（desktop / mobile / web）统一：每个 cloud API 请求直接 `resolveEntry()` → `fetch`，14s 预算，外层 `REQUEST_TIMEOUT_MS = 15000` 不变。
- 启动路径少一次最多 2s 的 seed prime 阻塞等待；不再发种子 CDN 请求。
- 不再发出任何 `relay-fetch` / `relay-add-nodes` IPC。
- 401 刷新原子性不变（本就在 `cloud-api.ts`，传输层从不处理 401）。

### 4. 测试

- **保活既有 relay 测试**：`resolve-and-fetch.test.ts`、`entry-pool.test.ts`、`antiblock-seed.test.ts` 中依赖 relay-first 行为的用例，通过 `vi.mock('../relay-flag', () => ({ RELAY_ENABLED: true }))`（或等效路径）翻转开关继续运行——代码没删，测试也不删。
- **新增默认关断言**：
  - `resolveAndFetch` 不调用 `_k2.run('relay-fetch')`，直接走 direct；direct 失败时不回落 relay，返回 `{transport:'fail'}`。
  - `addNodes` / `ensureSeeded` 不发出 `relay-add-nodes` IPC。
  - `bootstrapAntiblockSeed` 不发起任何 fetch。
  - 直连超时为 14000ms。
- 验证命令：`cd webapp && npx vitest run && npx tsc --noEmit && yarn build`。

## 风险与取舍

- 抗封锁能力回到 config.js 层：依赖 jsdelivr 镜像可达 + entry 域名未被墙。这是当初上 relay 的动因，但当前 relay 可达率（2/21）比直连更糟，回退是净收益；entry 域名可由运营侧通过 config.js 快速轮换。
- `k2_entry_url` localStorage 缓存的后台刷新逻辑保持原样，但增加**一次性值匹配清除**（最终审查修订，用户批准）：relay 时代的 seed bootstrap 每次启动都把该缓存覆盖成 embedded 的 CloudFront entry（对 CN 被 GFW 封），而 `resolveEntry()` 只要缓存存在就永不回落兜底——存量用户升级后会卡在被封 entry 上。修法：`bootstrapAntiblockSeed()` 的关停路径中，仅当缓存值 ∈ `EMBEDDED_SEED.entries` 时删除该键（CDN 解析出的 entry 绝不误伤）。
- Go relay 池仍会持有历史持久化节点数据，无害（无人查询）。

## 恢复路径

`relay-flag.ts` 的 `RELAY_ENABLED` 改回 `true`，重跑测试（默认关断言需同步翻转预期），发版。
