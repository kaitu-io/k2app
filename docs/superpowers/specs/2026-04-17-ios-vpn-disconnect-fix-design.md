# iOS VPN "断不开" 修复 — Always On 开关设计

**Linear:** [ANC-13](https://linear.app/anc777/issue/ANC-13/ios-vpn-关不掉neondemandruleconnect-导致-app-内断开按钮无效)
**Date:** 2026-04-17
**Status:** Design

## 问题

iOS 用户点 App 内"断开" → VPN 被 iOS 在 ~1s 内自动重激活。过去两天同一用户日志里累计 30+ 次 startTunnel 事件。用户工单原文："打开VPN影响我微信使用"——真实痛点是关不掉。

## 根因

`mobile/plugins/k2-plugin/ios/Plugin/K2Plugin.swift:258-259` 在 `connect()` 硬编码：

```swift
manager.isOnDemandEnabled = true
manager.onDemandRules = [NEOnDemandRuleConnect()]
```

`NEOnDemandRuleConnect()` 无任何条件约束（无 `interfaceTypeMatch` / `SSIDMatch` / `probeURL`），匹配所有网络请求。微信等常驻 App 每几秒发心跳包 → iOS 立即激活 VPN。

`disconnect()` (line 320-321) 禁用 on-demand 但**不清空 `onDemandRules`**，且 `saveToPreferences` 的错误被 `_` 丢弃。即使 save 成功，系统 NE 子系统可能仍持有旧规则缓存。

原设计意图是正当的——iOS NE 进程在 50MB jetsam 限制下容易被系统释放，on-demand 能在网络恢复时自动拉起 VPN 避免用户重连。但实现方式过于激进（无条件规则），把"进程被释放后恢复"扩张成了"任何时候都要连着"。

## 目标

1. **零困惑 disconnect**：用户点"断开"后 VPN 立即断开并保持断开 ≥60s，不被系统任何机制自动拉起。
2. **保留 jetsam 恢复能力**：愿意用的用户仍能开启"保持 VPN 常开"获得原有体验。
3. **升级即刻生效**：存量用户从 0.4.1 升级后无需主动操作，旧 on-demand 规则被清理。
4. **平台边界清晰**：Always On 是 iOS 平台行为，不污染 `ClientConfig` (Go wire contract)。

## 非目标

- **不做条件型 on-demand**（`NEOnDemandRuleEvaluateConnection` + probeURL）。作为未来独立工单——probeURL 选型需要业务侧评估（kaitu.io 自家域名？国内可达性？），超出本 issue 范围。
- **不改 Android**：Android VpnService 模型没有 on-demand 规则，不受影响。
- **不改 Desktop**：launchd/systemd 是另一套自动重连机制，和 iOS on-demand 无关。
- **不加 FTUX 提示**：默认关闭后，"自动重连消失"不主动告知用户，避免首次体验噪音。在发版说明里写明。

## 设计

### UX · "保持 VPN 常开"开关

**位置：** `webapp/src/pages/Dashboard.tsx` "高级设置"折叠区（已有 `Section 3` 容器，line 529），置于 `isInteractive` 警告之后、`RoutingModeSelector` 之前。

**平台守卫：** `window._platform.os === 'ios'`——Android / Desktop / 其他完全不渲染。

**默认值：** 关闭 (`alwaysOn=false`)。新装用户、存量升级用户都从关闭态开始。

**UI 三段式：**

```
┌────────────────────────────────────────────────┐
│ 保持 VPN 常开 (Always On)          [○─── 关闭]  │
│ App 被系统释放后自动恢复连接。                  │
│ 开启后，VPN 关闭需在 iOS 系统设置中操作。        │
└────────────────────────────────────────────────┘
```

- **主标题**回答"这是什么"
- **Description** 回答"干嘛用的"
- **Warning** 明确"开启的代价"——这是"不让用户问为什么断不开"的核心承诺：开关本身就说明了代价，用户主动选择后不会困惑。

**交互规则：**

- `disabled={isInteractive}` 沿用 Dashboard 现有约定——VPN 连接中置灰，和路由模式等高级设置一致。
- Toggle 变化 → 仅持久化 webapp preference，**不触发任何 NE prefs 写入**（NE 层由 connect/disconnect/migration 三条路径管理，见下文不变量证明）。
- Toggle 变化 → 无 toast / snackbar 反馈。零噪音。

**新增 i18n keys（dashboard 命名空间，7 locale 补全）：**

| key | zh-CN |
|---|---|
| `dashboard.alwaysOn.title` | 保持 VPN 常开 (Always On) |
| `dashboard.alwaysOn.description` | App 被系统释放后自动恢复连接。 |
| `dashboard.alwaysOn.warning` | 开启后，VPN 关闭需在 iOS 系统设置中操作。 |

英文 (`en-US` / `en-AU` / `en-GB`): `Keep VPN Always On` / `Auto-reconnect after the system releases the app.` / `While on, disconnecting requires using iOS Settings.`

其他 locale（ja / zh-TW / zh-HK）按现有翻译风格补齐。

### 技术 · 架构与数据流

```
┌──────────────────────────────────────────────┐
│ webapp                                         │
│   config.store: { alwaysOn: boolean }         │
│   持久化 key: 'k2.vpn.config' (与现有合并)      │
│       ↓                                         │
│   connection.store.connect():                  │
│     const config = buildConnectConfig(...)    │
│     const { alwaysOn } = configStore          │
│     _k2.run('up', { config, alwaysOn })       │
└──────────────────────────────────────────────┘
                     ↓
┌──────────────────────────────────────────────┐
│ bridges                                        │
│   capacitor-k2.ts:                            │
│     K2Plugin.connect({                        │
│       config: JSON.stringify(params.config),  │
│       alwaysOn: params.alwaysOn === true,     │
│     })                                         │
│   tauri-k2.ts:                                │
│     wrappedParams = { ...params, pid }        │
│     (daemon 忽略 alwaysOn 未知字段 — 已验证)   │
│   standalone-k2.ts: 忽略                       │
└──────────────────────────────────────────────┘
                     ↓
┌──────────────────────────────────────────────┐
│ K2Plugin.swift                                 │
│   connect(call) {                              │
│     let alwaysOn = call.getBool("alwaysOn")   │
│                  ?? false                      │
│     if alwaysOn {                              │
│       manager.isOnDemandEnabled = true        │
│       manager.onDemandRules =                  │
│         [NEOnDemandRuleConnect()]              │
│     } else {                                   │
│       manager.isOnDemandEnabled = false       │
│       manager.onDemandRules = []   ← 显式清空  │
│     }                                          │
│   }                                            │
└──────────────────────────────────────────────┘
```

### 边界决定

**`alwaysOn` 不进 `ClientConfig`**（Go wire contract），通过 `_k2.run('up', ...)` 的**信封 params** 传递：

- 前：`_k2.run('up', ClientConfig)` — params 就是 ClientConfig
- 后：`_k2.run('up', { config: ClientConfig, alwaysOn: boolean })` — params 是信封

**为何不复用 ClientConfig 作非 Go 字段载体：** `webapp/CLAUDE.md` 明确注释 ClientConfig "mirrors the Go wire contract"。一旦破例，未来任何平台行为参数都会塞进去，边界腐败。

**为何改契约可接受：** 信封模式是 `tauri-k2.ts:45-47` 的现有 bridge 内部惯例（`{ config, pid }`）。现在把信封上提到 webapp 层，换的只是信封组装位置，概念是一致的。改动范围极小——grep 显示 `_k2.run('up'` 在 webapp 代码只有 `connection.store.ts:401,429` 两处，加上测试文件 3 处。

### 技术 · 四条路径的不变量

**安全性通过四条路径组合保证：**

| 路径 | 何时触发 | NE prefs 写入 |
|---|---|---|
| **Migration**（一次性） | 升级后首次 load() | 强制清空 `isOnDemandEnabled + onDemandRules`（仅当检测到脏状态） |
| **Connect** | 每次 `_k2.run('up', ...)` | 按 `alwaysOn` 写入相应规则 |
| **Disconnect** | 每次 `_k2.run('down')` | 无条件清空，忽略 `alwaysOn` |
| **Toggle UI** | 用户切换开关 | **不写** NE prefs（仅 webapp 持久化） |

**不变量：** disconnect 后 NE prefs 恒为 clean state；connect 后按当前 alwaysOn 生效；toggle 只改下次 connect 的行为，不触碰当前态。

**为何 toggle 可以不写 NE prefs——场景闭环证明：**

| 场景 | NE prefs 状态 | toggle 切 OFF 是否需要写 NE？ |
|---|---|---|
| Migration 后 + 从未 connect | 已清空 | 否 |
| connect(alwaysOn=true) + disconnect(App内) | disconnect 已清空 | 否 |
| connect(alwaysOn=true) + 进程被系统杀 + iOS on-demand 自动重连 | 脏，但 VPN 状态为 `connected` | **toggle 被 `isInteractive=true` 锁死，用户点不到** |
| connect(alwaysOn=false) + disconnect | 两边都 clean | 否 |

唯一理论漏洞：`alwaysOn=true` 连接中进程崩溃（非用户 disconnect）→ iOS 维持 VPN 活跃 → 下次开 App 看到 `connected` → toggle 依然被 `isInteractive` 锁死 → 用户只能走 `disconnect`，disconnect 无条件清。**漏洞闭合。**

### 技术 · Migration 详细

`K2Plugin.swift load()` 末尾，`loadVPNManager` 完成后一次性迁移：

```swift
private let kOnDemandMigrationKey = "k2.onDemandMigration.v1"

// 在 loadVPNManager completion 里，获得 manager 引用之后：
if !UserDefaults.standard.bool(forKey: kOnDemandMigrationKey) {
    let hasStaleOnDemand = manager.isOnDemandEnabled
        || !(manager.onDemandRules?.isEmpty ?? true)

    if hasStaleOnDemand {
        logger.info("migration: clearing stale on-demand rules from prior version")
        manager.isOnDemandEnabled = false
        manager.onDemandRules = []
        manager.saveToPreferences { error in
            if let error = error {
                logger.warning("migration: saveToPreferences failed: \(error.localizedDescription) — will retry next launch")
                // Don't set flag — retry on next cold start
                return
            }
            UserDefaults.standard.set(true, forKey: kOnDemandMigrationKey)
            logger.info("migration: on-demand cleanup done, flag set")
        }
    } else {
        // Already clean from a fresh install or previous migration — mark done
        UserDefaults.standard.set(true, forKey: kOnDemandMigrationKey)
    }
}
```

**副作用分析：**

- 如果用户正在连接时被升级：App 升级必杀进程 → iOS 用旧 on-demand 规则立即自动重连一次（对用户无感）→ 新版 `load()` 执行迁移清规则。用户此刻 VPN 是活跃的，migration 清空 prefs 会让**下一次**进程被杀时失去自动恢复。这是可接受的一次性代价。
- 如果用户在 alwaysOn=true 状态下卸载重装：`UserDefaults.standard` 随 App 数据被清空 → 迁移 flag 丢失 → 下次 load() 又跑一次迁移。但此时 NE prefs 可能仍然由系统持久化着旧规则（NE 配置是 system-level）。迁移正好把它清掉，符合预期。

### 技术 · Disconnect 详细

`K2Plugin.swift:305-359` 改动：

```swift
@objc func disconnect(_ call: CAPPluginCall) {
    guard let manager = vpnManager else { call.resolve(); return }
    let connection = manager.connection
    if connection.status == .disconnected { call.resolve(); return }

    // Unconditional clear — user intent is to disconnect NOW regardless of alwaysOn pref.
    // Next connect() will re-apply alwaysOn based on current preference.
    manager.isOnDemandEnabled = false
    manager.onDemandRules = []
    manager.saveToPreferences { [weak self] error in
        _ = self
        if let error = error {
            logger.error("disconnect: saveToPreferences failed: \(error.localizedDescription) — stopping tunnel anyway")
            // Continue to stop. Save failure means on-demand might remain active
            // in system cache, but stopping the tunnel still honors user intent.
        }

        // ... 保留现有 observer + 5s timeout + stopVPNTunnel 逻辑 ...
    }
}
```

关键差异：
1. 补上 `manager.onDemandRules = []`
2. 不再用 `_` 丢弃 saveToPreferences 错误
3. 即使 save 失败也继续 `stopVPNTunnel`——用户意图明确

### 改动清单

**mobile/plugins/k2-plugin/ios/Plugin/K2Plugin.swift**
- `connect()`: 读取 `call.getBool("alwaysOn")`，按值设置 on-demand
- `disconnect()`: 清空 `onDemandRules`，不丢弃 saveToPreferences 错误
- `load()` 末尾 + `loadVPNManager` completion: 加 migration 块
- 新增私有常量 `kOnDemandMigrationKey`

**webapp/src/stores/config.store.ts**
- `ConfigState` 加 `alwaysOn: boolean` (默认 `false`)
- `StoredConfig` interface 加可选 `alwaysOn?: boolean`
- `parseStored`: 从存储读取 `alwaysOn`，fallback false
- `persist`: 持久化时包含 `alwaysOn`
- 新增 action `setAlwaysOn(on: boolean)`
- `buildConnectConfig`: 不改——`alwaysOn` 走 connection.store 层

**webapp/src/stores/connection.store.ts**
- `connect()` line 401: `_k2.run('up', { config, alwaysOn })` 替代 `_k2.run('up', config)`
- `connect()` line 429 (retry): 同样传 `alwaysOn`
- 从 `useConfigStore.getState().alwaysOn` 读当前值（retry 时重新读，保持语义一致）

**webapp/src/services/capacitor-k2.ts**
- `case 'up'` 分支 line 58-73: 
  - 校验 `params?.config` 非空
  - `K2Plugin.connect({ config: JSON.stringify(params.config), alwaysOn: params.alwaysOn === true })`

**webapp/src/services/tauri-k2.ts**
- line 45-48: wrapping 逻辑改为 `wrappedParams = { ...params, ...(pid != null && { pid }) }`。信封字段透传给 daemon，`alwaysOn` 作为未知字段被 daemon JSON 解析忽略。

**webapp/src/services/standalone-k2.ts**
- 如有 up 分支，ignore alwaysOn（standalone 无 iOS 场景）

**mobile/plugins/k2-plugin/src/definitions.ts**
- `connect(options)` 类型加 `alwaysOn?: boolean`
- 同步 `dist/definitions.d.ts`（build 产物，commit 入库）

**webapp/src/pages/Dashboard.tsx**
- 高级设置折叠区内，`RoutingModeSelector` 前加 `AlwaysOnToggle` 组件
- iOS-only 渲染（`window._platform.os === 'ios'`）
- `disabled={isInteractive}`

**webapp/src/components/AlwaysOnToggle.tsx**（新增）
- MUI Switch + 三段文案
- 读 `useConfigStore(s => s.alwaysOn)`
- 触发 `useConfigStore.getState().setAlwaysOn(on)`

**webapp/src/i18n/locales/\*/dashboard.json**
- 7 个 locale 文件各加 3 个新 key（title/description/warning）

### 日志打点

| 位置 | 级别 | 内容 |
|---|---|---|
| K2Plugin.swift `connect()` | INFO | `connect: alwaysOn=\(alwaysOn)` |
| K2Plugin.swift `load()` | INFO | `migration: clearing stale on-demand rules` / `migration: done` |
| K2Plugin.swift `disconnect()` | ERROR | `disconnect: saveToPreferences failed: ...` (save 失败时) |
| webapp config.store | INFO | `setAlwaysOn: on=true/false` |
| webapp connection.store | DEBUG | 现有 `connect: config built` 行追加 `alwaysOn=...` |

## 测试

### 手工验收（Linear 验收标准）

1. ☐ disconnect 后 VPN 保持断开 ≥ 60 秒（验证不被 iOS 自动拉起）
   - 步骤：alwaysOn=false → connect → disconnect → 观察 60s + 发微信心跳触发网络 → VPN 应保持 disconnected
2. ☐ alwaysOn=true 连接后 App 被系统强制退出（模拟 jetsam），验证 VPN 能自动恢复
   - 步骤：alwaysOn=true → connect → Xcode 发送 jetsam 信号 → 观察 iOS 自动拉起 NE
3. ☐ webapp 设置页能关闭"自动重连"，关闭后 disconnect 彻底生效且不会被覆盖
   - 步骤：alwaysOn=true → connect → disconnect → 开关切 false → connect → disconnect → 观察 60s 保持断开
4. ☐ disconnect 时 saveToPreferences 失败能在日志里体现
   - 步骤：伪造 save 失败（难，改代码强制返回 error）→ 看 native.log 是否有 ERROR 行

### 升级路径验收

5. ☐ 从 0.4.1 升级后（且 0.4.1 有过 connect 经历）：
   - 直接打开 App 不做任何操作 → 等 5 分钟 → VPN 应保持 disconnected
   - native.log 应包含一条 `migration: clearing stale on-demand rules`

6. ☐ 从 0.4.1 升级后重启 iPhone：
   - 开机后不开 App → 等 5 分钟 → VPN 应保持 disconnected（迁移在首次 App 启动时生效，而非开机）
   - 首次开 App → 触发 load() → migration 清理

### 单元测试

**webapp/src/services/\_\_tests\_\_/capacitor-k2.test.ts**
- ☐ `run('up', { config, alwaysOn: true })` → K2Plugin.connect 收到 `{ config: string, alwaysOn: true }`
- ☐ `run('up', { config, alwaysOn: false })` → 收到 `alwaysOn: false`
- ☐ `run('up', { config })` (无 alwaysOn) → 收到 `alwaysOn: false`（默认 false）
- ☐ `run('up', {})` (无 config) → 返回 `{code: -1, message: 'Config is required...'}`

**webapp/src/services/\_\_tests\_\_/tauri-k2.test.ts**
- ☐ 现有测试 `run('up', config)` 改为 `run('up', { config })` 保持行为
- ☐ 新增：`run('up', { config, alwaysOn: true })` → daemon 收到 `{ config, alwaysOn: true, pid }`（daemon 侧忽略）

**webapp/src/stores/\_\_tests\_\_/connection.store.test.ts**
- ☐ connect 调用 `_k2.run('up', ...)` 第二参是 `{config, alwaysOn}` 信封
- ☐ retry 路径同样传 `alwaysOn`

**webapp/src/stores/\_\_tests\_\_/config.store.test.ts**（如有）
- ☐ `setAlwaysOn(true)` 后 state 更新并写 storage
- ☐ `parseStored` 老数据（无 alwaysOn 字段）→ 默认 false
- ☐ `parseStored` 有 alwaysOn → 读取

### Swift 侧测试

K2Plugin 无 Swift 单元测试工程。Migration 逻辑只能靠手工验收（5, 6）。`disconnect` 清 rules + 不吞错误的逻辑也是手工验收覆盖。未来若补 Plugin 测试工程再补。

### 不适用

- E2E / Playwright：无法覆盖 iOS NE 系统行为，跳过。

## 发版

**版本号：** 下一个 iOS native 版本（0.4.2 或视发版节奏定）。纯 native 代码 + webapp 改动，web OTA 可先推让存量用户拿到 UI，但 native 修复（migration + on-demand 控制）必须靠 App Store 升级。

**发版说明（Changelog）：**
- zh-CN: "修复 iOS 断开 VPN 后被系统自动重连的问题。高级设置新增「保持 VPN 常开」开关，需要该能力的用户可手动开启。"
- en-US: "Fixed iOS VPN auto-reconnecting after user-initiated disconnect. Added 'Keep VPN Always On' toggle in advanced settings for users who want it."

**发版后监控：**
- Ticket #116 工单相关用户反馈 → 再次确认断开行为符合预期
- native.log 聚合：`grep "migration: "` 分布——预期存量用户首次启动新版有一条，之后永远没有
- 支持工单关键字："关不掉" / "自动连上" / "总是打开" 的出现频率

## 后续工作（不在本 issue 范围）

1. **条件型 on-demand 可选支持**：`NEOnDemandRuleEvaluateConnection` + probeURL，让 alwaysOn=true 时只在真正无网络时激活 VPN（而非无条件重连）。需要单独 issue 评估 probeURL 选型。
2. **用户教育**：知识库里补"iOS VPN 关闭完整方法"文档，客服话术引用。
3. **Plugin 测试工程**：K2Plugin.swift 缺单元测试覆盖，migration / disconnect 逻辑目前只能手工验。可作为基础设施工单。

## 开放问题

无。所有设计决策已在上文明确。

---

**Confidence:** 9.5/10
**Reviewer:** TBD
