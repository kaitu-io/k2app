# k2r Headless + App 直控路由器 — 设计规格

日期：2026-07-17
状态：已评审（brainstorming 完成，待实现计划）
范围：k2（submodule，gateway/ + webui 构建）、k2app（webapp/ + api/ + desktop/ + mobile/ bridges）

## 0. 决策摘要

| 决策点 | 结论 |
|---|---|
| k2r 是否嵌入 webapp | 否。k2r 变 headless，只保留 JSON 控制 API；`/` 留一页静态提示页 |
| 浏览器管理场景 | 完全放弃。App（Tauri 桌面 / Capacitor 移动）是唯一 UI |
| `k2r.local` 域名 / HTTP 劫持 | 不做。App 用 IP:port 直连，域名只对浏览器有意义。现有 `k2.local` mDNS responder 保留不动（调试便利） |
| 发现机制 | 主路径：默认网关探测；兜底：Center beacon 配对（已有）。不做 app 内 mDNS browsing |
| 鉴权模型 | 账号绑定 controlKey，k2r 经自身订阅信道向 Center 校验绑定（无 legacy TOFU 竞态） |
| UI 形态 | 顶层 Router tab（与 Dashboard 平级）+ Dashboard「路由器接管中」联动横幅。SmartServerSelector 三个 tab 零改造 |
| 互斥策略 | 本机 VPN 与路由器隧道双连即将成立时弹 MUI Dialog 强提醒（不在 tab 切换时骚扰） |
| app→k2r 传输 | 原生 HTTP 桥（Tauri plugin-http / CapacitorHttp）。无 CORS、无 mixed content 问题 |
| 状态同步 | Router tab 可见时 2s 轮询 `/api/core` status。不依赖 SSE（CapacitorHttp 不支持流式响应） |

## 1. 背景与动机

现状：k2r 通过 `k2/webui/` 嵌入完整 webapp SPA（1779 端口），用户在浏览器里访问 `http://lanIP:1779` 管理路由器；webapp 存在 `platformType === 'gateway'` 专用模式（GatewaySetup 页、RouterDevices tab）。发现机制已有三条：`k2.local` mDNS responder（`gateway/mdns.go`）、Center beacon 配对（`gateway/beacon.go` → `/api/pair/beacon` / `/api/pair/discover`，同公网 IP 匹配）、LAN-only 面板绑定（`gateway/bind.go`）。

问题：

1. 嵌入 SPA 使 k2r 二进制携带整个 webapp 资产——MIPS 小闪存路由器（8–16MB flash）压力大，且面板 UI 与 app UI 双轨维护。
2. 浏览器面板无鉴权（同 LAN 即可控制），信任模型薄弱。
3. **beacon 配对在隧道已连接时失效**：k2r 自发流量走直连（TPROXY 只劫持转发流量），Center 看到家宽 IP；而手机在路由器 WiFi 下访问 Center 的流量已进隧道，Center 看到 k2s 节点 IP——两侧公网 IP 不匹配，`/api/pair/discover` 返回空。即最高频的「日常控制运行中的路由器」场景恰好发现不了。

目标：k2r headless 化；app 成为唯一管理 UI；发现与鉴权补强。

## 2. 总体架构

```
┌─ App (Tauri / Capacitor) ──────────────────┐
│  Dashboard tab            Router tab       │
│  (主语=本机 VPN)          (主语=路由器)      │
│      │  「路由器接管中」横幅 ↔ 互斥 Dialog    │
│      │                        │            │
│      │        发现: ① 接口级默认网关探测      │
│      │              ② beacon 兜底(已有)     │
└──────┼────────────────────────┼────────────┘
       ▼                        ▼ 原生 HTTP 桥
  本机 daemon            k2r 控制 API (headless)
  (127.0.0.1:1777)       http://<lanIP>:1779
                         Bearer <controlKey>
                                │ k2subs 订阅信道
                                ▼
                    Center: mint k2subs 凭证（已有）
                            controlKey 托管 + hash 下发（新）
                            beacon 配对（已有）
```

设计原则：每个 surface 主语单一。Dashboard 的状态机永远只反映本机 daemon；Router tab 的状态永远来自远端 k2r；两者互不渗透（独立 zustand store）。

## 3. k2r 侧（k2 仓库，`gateway/` + 构建）

### 3.1 去嵌入

- 移除 `/`（SPA handler）与 `/api/storage` 的注册（storage 是嵌入 webapp 的持久化，headless 后无消费者）。
- 保留：`/ping`、`/api/core`（up/down/status/set-credential）、`/api/log-level`、`/api/upgrade`（OTA）、router-devices 系列、`/api/events`（SSE 保留但 app 不依赖，供调试/未来使用）。
- `/` 返回一页内联静态 HTML（品牌中立措辞，不含 Kaitu/开途字样，例如 "This router is managed by the companion app."），老面板书签用户不落 404。
- 构建剥离：k2r 构建启用 `nowebapp` tag（或等价拆包），确保 embed 资产不进二进制。`webui.Upgrader` / `webui.Storage`（文件层）与 embed 解耦保留——k2 Linux desktop 的 embed 不受影响。

### 3.2 鉴权（controlKey）

- **数据**：k2r 在 `/etc/k2r/` 持久化 `controlKeyHash = sha256(controlKey)`。
- **校验路径（防抢占的关键）**：controlKey 的权威来源是 Center，规则统一为**「Center 权威、adopt 覆盖」**——k2r 每次订阅刷新（`/api/subs`，已有的后台刷新信道，携带自身 udid:token）时，响应新增 `control_key_hash` 字段；k2r 与本地不一致即采纳落盘（无回滚分支，一条规则同时覆盖三个场景）：
  - **首次配置**：未配置的 k2r 接受第一个 `set-credential` 推送 `{url, controlKey?}`（TOFU——「谁配置谁拥有」，与现状语义一致；`controlKey` 参数可选，缺省时等待首次订阅刷新采纳，兼容 `k2r setup` CLI 流程）。推送的 key 先作临时绑定即时生效；首次订阅成功后被 Center 权威 hash 覆盖——若 app 推了过期 key，覆盖后 app 收 401 → 向 Center 重取 → 收敛。
  - **legacy 升级**：已配置但无 key 的老 k2r，首次订阅刷新即从 Center 学到 hash，鉴权自动生效。**无任何 TOFU 窗口**，LAN 攻击者无法收编存量路由器。
  - **key 轮换**：用户在 Center 重置 key → k2r 下次订阅刷新采纳新 hash；app 侧收到 401 后向 Center 重取。
- **中间件**：除 `/ping` 与 `/`（静态页）外，所有端点要求 `Authorization: Bearer <controlKey>`，本地比对 hash，失败返回 HTTP 401。未绑定 key 状态下 `set-credential` 免鉴权（TOFU 入口），其余端点即使未配置也 401。**loopback（127.0.0.1/::1）来源豁免鉴权**——`k2r up/down/status/reset` CLI 走 localhost IPC，且能在本机发起 loopback 流量者已拥有路由器本体。
- **解绑**：新增 CLI `k2r reset`——清除凭证、controlKeyHash、状态文件。app 侧「解除绑定」调用需鉴权的 reset API 端点（`/api/core` action `reset`）。

### 3.3 `/ping` 扩展（发现签名）

```json
{ "k2r": true, "version": "0.4.7", "configured": true, "name": "OpenWrt-MT7981" }
```

无鉴权（发现必需）。不暴露 udid、订阅、LAN 拓扑等敏感信息。`name` 取 hostname。

### 3.4 明确不做

- 不做 CORS（app 走原生 HTTP 桥，无浏览器 origin 语义）。
- 不动 `k2.local` mDNS responder 与 beacon 上报逻辑。

## 4. Center 侧（k2app 仓库，`api/`）

- 新增 `POST /api/user/router-control-key`（用户鉴权）：账号级 controlKey，首调生成、后续幂等返回明文 key。同账号多设备天然共享控制权。存储：User 关联新字段/表存 key（server 端存明文或可逆——需向 app 重复下发；见开放项 §10）。
- 新增 `POST /api/user/router-control-key/reset`（用户鉴权）：轮换。
- `/api/subs` 响应（k2r 订阅信道）新增 `controlKeyHash` 字段：按请求 udid 归属账号查 key、下发 sha256。老客户端忽略未知字段，无兼容问题。
- beacon/discover 端点不变。

## 5. App 侧发现（k2app 仓库，webapp + bridges）

新增 `webapp/src/services/router-service.ts` + `router.store.ts`（zustand）。

### 5.1 探测链

1. **接口级默认网关探测（主路径）**：bridge 新增 `_platform.getDefaultGateway(): Promise<string | null>`——必须返回**物理接口**（WiFi/以太网）的网关，而非路由表全局默认（本机 VPN 开启时全局默认指向 TUN）。
   - Tauri（Rust）：枚举接口路由（`default-net` 类 crate 或读系统路由表），排除 TUN/虚拟接口。
   - Android（K2Plugin）：`ConnectivityManager` → WiFi/Ethernet network 的 `LinkProperties.routes` 取 gateway（不是 active default network）。
   - iOS（K2Plugin）：sysctl 路由表 dump 取 en0 系网关。
   - 拿到网关 IP 后，原生 HTTP 桥 GET `http://<gw>:1779/ping`，1.5s 超时，校验 `k2r: true`。
2. **beacon 兜底**：`discoverRouter()`（已有 `/api/pair/discover`），覆盖旁路由拓扑（k2r 不是默认网关）及网关查询不可用平台。对返回候选逐个 `/ping` 验证。
3. **已配对缓存**：命中后持久化 `{lanIP, port, name}`；下次启动直接 ping 缓存地址，失败再走探测链。

### 5.2 触发时机

App 启动、回前台、Router tab 手动刷新。探测结果进 `router.store`：`none / unconfigured / online / offline`。

### 5.3 传输层

`routerHttp` 适配器：Tauri 用 `@tauri-apps/plugin-http`，Capacitor 用 `CapacitorHttp`，`make dev-standalone`（纯浏览器开发模式）降级 `fetch`（该模式下路由器功能仅限开发验证，不承诺 mixed content 场景）。所有请求注入 `Authorization: Bearer <controlKey>`。controlKey 缓存于现有 app 存储（桌面加密 storage / 移动 Preferences）。

### 5.4 iOS 本地网络权限

首次向 LAN IP 发请求会触发 iOS 本地网络权限弹窗——这是 LAN 直控的必要成本，只弹一次。Info.plist 补 `NSLocalNetworkUsageDescription` 文案。探测链设计已避免 mDNS browsing，无需 `NSBonjourServices` 与组播 entitlement。

## 6. UI/UX（webapp）

### 6.1 Router tab（顶层，与 Dashboard 平级）

- **出现条件**：`router.store` 状态非 `none`（当前发现或曾配对）。离线时 tab 保留、内容置灰（避免闪烁消失）。
- **内容三段**：
  1. **连接卡**：路由器隧道状态大按钮（up/down）、出口节点（读 k2r 订阅的节点列表与当前选择）、流量/运行时长。数据来自 2s 轮询（tab 可见时才轮询，隐藏即停）。
  2. **设备列表**：迁移现有 `RouterDevices` 内容（LAN 设备、MAC 白名单、open/allowlist 模式），数据源从同源 API 改为 `routerHttp`。
  3. **路由器设置**：版本 + OTA 升级、日志级别、解除绑定（reset）。
- **未配置态**（`configured: false`）：显示「设置此路由器」引导页——一键完成：mint k2subs 凭证（已有 `/api/user/gateway-credential`）→ 取 controlKey → 推送 `set-credential {url, controlKey}` → 轮询确认 → 转入连接卡。未登录先走登录 guard；mint 失败（无套餐）按 Center 错误文案引导购买。
- **退役**：`GatewaySetup` 页、`platformType === 'gateway'` 分支（App.tsx / Layout.tsx）、`AddRouterCard` 的「打开外部浏览器」链接（卡片改为跳 Router tab 的入口）。

### 6.2 Dashboard 联动横幅

检测到「当前物理网关是已配对 k2r 且其隧道 connected」且本机 VPN 未连接时，Dashboard 未连接态不显示裸的「未连接」，叠加横幅：「已由路由器接管保护 →」，点击跳 Router tab。消除「在路由器 WiFi 下看到未连接以为没被保护」的误解。

### 6.3 互斥强提醒

- 触发条件：**双连即将成立**的两个瞬间——
  - Router tab 内点连接，而本机 VPN 已连接 → Dialog：「路由器接管后本机无需再开 VPN，双重代理会变慢。断开本机 VPN？」〔断开并继续（默认）/ 保持双连〕。
  - Dashboard 点连接，而当前网关是已连接的 k2r → 反向文案同款 Dialog。
- 不在 tab 切换时弹（有 k2r 但无有效套餐的用户日常用本机 VPN，查看 Router tab 不应被打断）。
- 一律 MUI Dialog——Capacitor WebView 静默吞 `window.confirm`（返回 false），constitutional 约束。

## 7. 错误处理

| 场景 | 行为 |
|---|---|
| 探测全链失败 | Router tab 离线态（曾配对）或不出现（从未配对）；后台静默按触发时机重试 |
| 控制请求 401 | 先向 Center 重取 controlKey 重试一次（key 轮换场景）；仍 401 → 「重新配对」CTA |
| 控制请求超时/网络错 | 标记 offline，轮询继续（下轮恢复即回 online） |
| 推送的 controlKey 与账号不一致 | 首次订阅刷新后被 Center 权威 hash 覆盖 → app 旧 key 401 → 自动重取收敛（无回滚分支） |
| 路由器隧道错误 | 复用现有 EngineError code→i18n 文案映射（1xx 网络 / 4xx 客户端 / 5xx 服务端语义不变；402 引导购买） |
| mint 失败（无套餐） | 按 Center 错误文案引导购买，Router tab 管理功能（设备/OTA）不受影响 |

## 8. 测试

- **k2 gateway 单测**：鉴权中间件（401 / 未配置 set-credential 免鉴权 / hash 比对）、TOFU→Center 校验回滚、`/ping` 应答形态、`k2r reset`、订阅响应 controlKeyHash 落盘。`make gateway-check`。
- **gateway-uat（Docker）**：headless 验收——`/` 返回静态页非 SPA、无鉴权请求 401、set-credential→up 全流程。
- **Center（api/）**：control-key 幂等、reset 轮换、`/api/subs` 按 udid 归属下发 hash。
- **webapp vitest**：router-service 探测降级链（网关→beacon→缓存）、router.store 四态迁移、Router tab 三态渲染、互斥 Dialog 触发矩阵（4 组合）、401 重取重试。
- **真机 smoke**（release 信心门槛）：iOS 本地网络权限弹窗路径、Android CapacitorHttp 到 LAN、桌面 Tauri 到 LAN。

## 9. 明确不做（YAGNI）

- Center 中继远程管理（人在外控制家中路由器）——API 形态不排斥未来叠加（同一套 JSON API 换传输），本期不做。
- app 内 mDNS/Bonjour browsing、`k2r.local` 域名、DNS/HTTP 劫持、配对码。
- k2r 面板 UI 的任何形态复活。
- SSE 作为 app 依赖（保留端点但不消费）。

## 10. 开放项（进实现计划前需拍板）

1. **controlKey 服务端存储形态**：需向同账号 app 重复下发明文 key → Center 存明文（或可逆加密）。风险可接受（key 只控制家庭路由器，且 Center 本就托管订阅凭证）；实现时与现有凭证存储惯例对齐。
2. **`/api/subs` 响应字段的 JSON key**：遵循 Go snake_case → 桥不经手（k2r 内部消费），直接 `control_key_hash`。
3. **实现拆仓顺序**：k2 仓库（gateway 鉴权 + headless + subs 字段消费）先行合并出测试版二进制 → k2app（api/ 下发 + webapp/bridges）跟进。k2 submodule 在父仓只读，两边各走独立 worktree/分支。

## 11. 迁移与兼容

- 老 k2r（有面板）→ 新 k2r（headless）经现有 OTA 通道升级；升级后书签用户看到静态提示页。
- 老 app + 新 k2r：老 app 的 AddRouterCard 仍指向 `http://lanIP:port`，落到静态提示页——可接受的过渡降级。
- 新 app + 老 k2r：`/ping` 无 `k2r:true` 签名字段 → 探测不认，Router tab 不出现；beacon 卡片提示升级路由器固件（文案兜底）。
- 版本门槛：Router tab 功能要求 k2r ≥ 本设计落地版本。
