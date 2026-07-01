# AppBypass 第三种模式：强制代理（Force Proxy）

> **⛔ 已放弃（2026-07-01）** —— 不实施。
> **原因**：核心承诺"整 app 全部流量走代理"只有桌面能兑现。**Android** 无 per-connection app 归因（sing-tun 的 Android 模型是 `BuildAndroidRules` package→uid→`IncludeUID/ExcludeUID` 的 **whole-app、配置期**内核路由；包进 TUN 后是裸 IP 流不带 app 身份；k2 唯一的 per-connection 归因 `LinuxProcessSearcher` 读 `/proc/net/tcp` 在 Android 10+ 被 SELinux 挡；全仓无 `getConnectionOwnerUid`）→ forceProxy 命中不了 `{apps,via:serverUrl}`，**退化成 = 智能**（境内目标仍按目的地可能直连）。**iOS** `expandAppsRoute` 返 nil，本就无 per-app。于是三态里「代理」与「智能」在 Android 不可区分、在 iOS 无效——功能名不副实，放弃。
> **若将来要做**：先在 k2 实现 Android `getConnectionOwnerUid` 版 ProcessSearcher（sing-box 的做法）→ 复用 `PackageResolver.PackageForUID` 喂规则引擎，per-connection 命中后 forceProxy 才在 Android 名副其实；独立 k2 子模块工程 + 真机验证。
> 下方原始设计仅作记录。

**日期**：2026-07-01
**范围**：webapp 前端 + i18n（无 store / config / Go 引擎 / 原生桥接改动）

## 背景与问题

per-app 分应用代理页（`AppBypass.tsx`，`dashboard:appBypass.v2` 命名空间）当前每行显示两个 chip：`智能`(左) / `直连`(右)。用户希望新增第三种「强制代理」模式：选上后该 app 的**全部流量总是走代理（含境内目标）**。

调研发现底层其实已存在**三种真实行为**，但 UI 只暴露了两种"结果"：

| 底层状态 | 真实行为 | 当前 UI 暴露方式 |
|---|---|---|
| `default`（无 override） | 跟随引擎默认：按 k2-rules 的 app 区域分类 + 目的地区域规则决定直连/代理 | 与 `forceProxy` 混在「智能」chip 里 |
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
- **不做"全网 publisher 强制代理清单"（k2-rules 侧）**。曾讨论过为美股券商等"需整 app 全隧道"的特殊 app 在 k2-rules 加一张对称的 force-proxy 清单（新 TypeID + 库 + 编译 + classify 引擎 + webapp 注入）。**决策：否掉（2026-07-01）**。理由：本功能已提供 per-app「强制代理」toggle，用户对这类特殊 app 直接在 UI 选「强制代理」即可达到"整 app 全隧道"，无需跨仓库改 k2-rules。k2-rules 保持只读、只表达区域归类默认。若未来出现"必须全网默认、用户无从知晓要手动开"的规模化需求，再单独立项。

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

## k2-rules 分层与引擎短路（已逐行核实）

**尖锐问题**：k2-rules（krs）确实有 app 维度（`app-bypass/{cn,ir}.yaml` → `.krs` 的 `AndroidApps/WindowsApps/DarwinApps` glob 段，TypeID 0x0100–0x0500）。那"强制代理"在 k2-rules 里有表达吗？

**答：没有，也不需要。两者是两个层，强制代理是 route 层覆盖，短路 krs。**

- **k2-rules 的 app 维度 = 区域归类默认**。语义是归属声明（`app-bypass/README.md:38-42`）："这些 app 属于本区域"。它被 webapp `classify-apps`（`services/classify-apps.ts`，走引擎连接时同一条 `krs.MatchInstalled` 代码路径）消费，产出每个 app 的 `def`（`'direct'|'proxy'`），即**「智能」模式下该 app 的默认走向**。这是全网公开、发到 CDN 的 publisher 数据，不含用户维度。
- **强制代理 / 强制直连 = 每用户运行时覆盖**，表达在 `ClientConfig.routes` 的 Tier-1 覆盖里，不能也不该塞进公开 krs bundle。

**引擎侧已逐行核实的事实**：

| 事实 | 代码依据 |
|---|---|
| 路由严格 first-match-wins（命中即返回） | `k2/rule/engine.go:254-266` |
| `match:{apps:[X]}` 纯源匹配（只看进程名/包名，不看目的地）→ app X 的任意目的地（含境内）都命中 Tier-1 | `rule/engine.go:44-49 hasMetaCriteria`；`k2/engine/region_expand.go:99-103`（无 Sets 目的地条件） |
| 命中 `via:serverUrl` 后 **100% 走隧道**：SetTmpRule 无二次判定 | `rule/engine.go:434-482` |
| SNI 嗅探重路由**仅在 `action==directTarget` 时触发**，代理态不回改 | `k2/core/tunnel.go:267-274`（TCP）、`419-426`（UDP） |
| `allProxy` 空-routes 分支仅在真·全局单路由时走，forceProxy 路由存在时不触发 | `k2/engine/engine.go:1458-1461` |
| krs 纯目的地维度（域名/IP/region/preset），app 匹配由 rule engine 的 ProcessNames/PackageNames 独立处理 | `rule/engine.go` RouteEntry |

**判定**：命中 `via:serverUrl` 的连接 100% 走隧道，不会因区域规则 / DNS 学习 / SNI 嗅探漏成直连。**forceProxy 正确压过 krs 的 app 分类，且完全不需要 k2-rules 侧改动。**

**既有边角（非本次范围，仅记录）**：同段 SNI 逻辑意味着现有「直连」(forceDirect) 并非 100% 直连——`action==direct` 的连接会被 SNI 嗅探，命中代理规则时可能被拉回代理。强制**代理**气密，强制**直连**有此 SNI 回改路径。这是既有行为，本功能不引入、不修复。

## 风险与回归防护

- **主要风险**：`chipProxy` 文案含义翻转（智能→代理）可能让老用户困惑。缓解：intro 明确解释三种模式；三段式让"智能"与"代理"并列可见、语义自证。
- **无路由行为变化**：三种底层路由完全沿用现状，仅 UI 表达方式变化，无隧道侧回归。
- **验证**：`npx tsc --noEmit`、`npx vitest run`（含新 AppBypass 组件测试）、桌面手动确认三段切换后 override 正确落盘并作用于连接。
```
