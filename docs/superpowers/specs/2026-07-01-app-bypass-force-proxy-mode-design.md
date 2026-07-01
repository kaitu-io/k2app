# AppBypass 第三种模式：强制代理（Force Proxy）

**日期**：2026-07-01
**范围**：webapp 前端 + i18n（无 store / config / Go 引擎 / 原生桥接改动）

## 背景与问题

per-app 分应用代理页（`AppBypass.tsx`，`dashboard:appBypass.v2` 命名空间）当前每行显示两个 chip：`智能`(左) / `直连`(右)。用户希望新增第三种「强制代理」模式：选上后该 app 的**全部流量总是走代理（含境内目标）**。

调研发现底层其实已存在**三种真实行为**，但 UI 只暴露了两种"结果"：

| 底层状态 | 真实行为 | 当前 UI 暴露方式 |
|---|---|---|
| `default`（无 override） | 跟随区域智能分流：境内目标直连、境外目标走代理（逐连接判断） | 与 `forceProxy` 混在「智能」chip 里 |
| `forceProxy` | 全部走代理（连境内目标也走隧道） | 与 `default` 混在「智能」chip 里 |
| `forceDirect` | 全部直连 | 「直连」chip |

当前 `AppRow`（`AppBypass.tsx:190-214`）通过 `proxyIsDefault` 按该 app 的区域默认值把 `default` 与 `forceProxy` **合并**进同一个「智能」chip：

- 对区域默认走代理的 app，「智能」chip → `onSet('default')`，`forceProxy` 态**在 UI 上不可达**。
- 对区域默认直连的 app，「智能」chip → `onSet('proxy')`（即 forceProxy）。

结果：「智能」这个标签名不副实（既代表区域分流又代表强制全代理），而用户想要的「强制代理」其实就是底层已存在的 `forceProxy`，只是从未作为独立选项暴露。

**关键结论**：store（`setOverride` 已支持 `'proxy'|'direct'|'default'`）、`config.store.buildConnectConfig`（`forceProxy → via:serverUrl`、`forceDirect → via:direct`）、Go 引擎（`match.apps` 平台派发）、Android 桥接（`parseDisallowedPackages` 仅提取 `via:direct`，forceProxy 自然进隧道）**全部已支持三态**。本次改动是纯前端 + 文案：把三态诚实地暴露为三选一，并修正标签。

## 目标

1. 每个 app 可在 `智能 / 直连 / 代理` 三者中显式三选一。
2. 语义诚实：`智能`=跟随区域分流，`直连`=全部直连，`代理`=全部走代理（含境内目标）。
3. 无功能回归：现有 `forceProxy` / `forceDirect` / `default` 的路由与各平台行为保持不变。

## 非目标（YAGNI）

- 不改 store / config / Go 引擎 / 原生桥接逻辑。
- 不新增存储字段（三态已由 `forceProxy[]` / `forceDirect[]` 的"在/不在"表达，`default`=两者皆不在）。
- 不做全局连接模式（`ruleMode: global/chnroute`）相关改动——那是另一层。
- 不为 iOS 新增 per-app 能力（该页在 iOS 本就 `unsupported`）。

## 设计

### 1. UI：三段式选择器（方案 A）

`AppBypass.tsx` 的 `AppRow` 组件：把现有两 chip 开关（`AppBypass.tsx:205-214`）替换为 MUI `ToggleButtonGroup`（`exclusive`），三段：

| 段 | value | i18n key | 含义 |
|---|---|---|---|
| 智能 | `default` | `chipSmart` | 跟随区域智能分流（境内直连·境外代理） |
| 直连 | `direct` | `chipDirect` | 全部直连 |
| 代理 | `proxy` | `chipProxy` | 全部走代理（含境内目标） |

行为：
- 选中值 = `modeOf(app, forceDirect, forceProxy)` 返回的 `mode`（已返回 `'default'|'direct'|'proxy'`）。
- `onChange={(_, v) => { if (v) void onSet(v); }}`——点击已选中段返回 `null` 时忽略，保证始终有一个选中态。
- 移除 `proxyIsDefault` / `effective` 那套按区域默认高亮的重映射逻辑：三段与模式一一对应。
- 新装 app 无 override → `mode==='default'` → 默认高亮`智能`（符合"多数 app 跟随智能分流"的预期）。
- `def`（`classify-apps` 的区域默认）prop 不再用于着色。可保留 prop 以便未来做提示，但本次不渲染额外提示（模式含义统一放 intro）。

尺寸/风格：`ToggleButtonGroup size="small"`，沿用 MUI 暗色主题 token，右对齐，保持行高与现状一致。

### 2. i18n（7 个 locale 文件）

命名空间 `dashboard:appBypass.v2`：

- **重定义** `chipProxy`：`智能`(Smart) → `代理`(Proxy / 全部走代理)。
- **保持** `chipDirect` = `直连`(Direct)。
- **新增** `chipSmart` = `智能`(Smart，跟随区域分流)。
- **改写** `intro`：一句话解释三种模式，如 zh-CN：「为每个应用选择路由方式：智能（境内直连、境外代理）、直连（全部直连）、代理（全部走代理）。」

7 个 locale：`zh-CN`(主，先改) / `en-US` / `ja` / `zh-TW` / `zh-HK` / `en-AU` / `en-GB`。英文建议：`chipSmart`=Smart、`chipDirect`=Direct、`chipProxy`=Proxy；日文：スマート / 直接接続 / プロキシ。

key↔mode 映射固定为 `chipSmart/chipDirect/chipProxy` ↔ `default/direct/proxy`。

### 3. 测试（TDD）

- `AppBypass.tsx` 组件测试：渲染三段；点击各段调用 `onSet('default'|'direct'|'proxy')`；给定 `forceProxy`/`forceDirect`/皆空的 store 状态时，正确段被高亮（`aria-pressed`/selected）。
- `app-routes.store` 已有对 `setOverride` 三态的覆盖，无需改动；如缺 `mode==='proxy'` 用例则补齐。

## 数据流（不变，仅确认）

```
AppRow ToggleButtonGroup(value=mode)
  └─ onSet(mode) → app-routes.store.setOverride(app, mode)
        default → 从 forceProxy/forceDirect 双删（回归区域分流）
        proxy   → 加入 forceProxy[]
        direct  → 加入 forceDirect[]
  └─ persist(forceProxy, forceDirect) → _platform.storage['k2.routes.overrides']

连接时 config.store.buildConnectConfig:
  forceDirect → { match:{apps:[...]}, via:'direct' }   (Tier-1)
  forceProxy  → { match:{apps:[...]}, via:serverUrl }   (Tier-1)
  其余 app → 区域路由 (Tier-2, match.region)
```

## 平台行为矩阵（不变，仅记录）

| 平台 | 智能(default) | 直连(direct) | 代理(proxy) |
|---|---|---|---|
| 桌面(macOS/Win/Linux) | 区域分流 | direct 路由 | via serverUrl 全走隧道 |
| Android | 进隧道·引擎内区域分流 | `addDisallowedApplication`（内核级排除） | 进隧道·全代理（不 disallow） |
| iOS | 该页 `unsupported`，无 per-app | — | — |

## 风险与回归防护

- **主要风险**：`chipProxy` 文案含义翻转（智能→代理）可能让老用户困惑。缓解：intro 明确解释三种模式；三段式让"智能"与"代理"并列可见、语义自证。
- **无路由行为变化**：三种底层路由完全沿用现状，仅 UI 表达方式变化，无隧道侧回归。
- **验证**：`npx tsc --noEmit`、`npx vitest run`（含新 AppBypass 组件测试）、桌面手动确认三段切换后 override 正确落盘并作用于连接。
```
