# k2r Headless + App 直控路由器 — 设计规格

日期：2026-07-17（2026-07-18 修订：发现机制改为锚点地址 HTTP 拦截唯一路径，见 §3.5/§5）
状态：已评审（brainstorming 完成，待实现计划）
范围：k2（submodule，gateway/ + webui 构建）、k2app（webapp/ + api/ + desktop/ + mobile/ bridges）

## 0. 决策摘要

| 决策点 | 结论 |
|---|---|
| k2r 是否嵌入 webapp | 否。k2r 变 headless，只保留 JSON 控制 API；`/` 留一页静态提示页 |
| 浏览器管理场景 | 完全放弃。App（Tauri 桌面 / Capacitor 移动）是唯一 UI |
| `k2r.local` 域名 / DNS 劫持 | 不做域名与 DNS 劫持。现有 `k2.local` mDNS responder 保留不动（调试便利） |
| 发现机制 | **唯一路径：锚点地址 HTTP 拦截**（2026-07-18 修订）——app 对固定锚点 `http://10.17.79.1:1779` 发请求，k2r 在转发路径上 DNAT 拦截应答。k2r 在网关链任意一层均可达（多层路由天然支持）；无网关探测消费、无 app 内 beacon 发现、无 mDNS browsing、无 lanIP 缓存 |
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
│      │        发现+控制: 锚点地址(常量)      │
│      │        http://10.17.79.1:1779       │
└──────┼────────────────────────┼────────────┘
       ▼                        ▼ 原生 HTTP 桥
  本机 daemon            k2r 控制 API (headless)
  (127.0.0.1:1777)       锚点流量在转发路径上被
                         k2r DNAT 拦截到本机 1779
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
  - **legacy 升级**：已配置 k2subs 的老 k2r，升级后首次完成订阅刷新即从 Center 学到 hash，鉴权自动生效——**对能完成订阅刷新的路由器无 TOFU 窗口**，LAN 攻击者无法收编存量在线路由器。前提：Center 侧对路由器归属账号 mint-on-serve（见 §4）。
  - **key 轮换**：用户在 Center 重置 key → k2r 下次订阅刷新采纳新 hash；app 侧收到 401 后向 Center 重取。
- **残留 TOFU 窗口（接受偏差，2026-07-17 终审记录）**：采纳只发生在订阅信道上，因此两类路由器窗口不闭合——① 直连 `k2v5://` 配置（无订阅信道）的 k2r：除非 app 曾经 `set-credential` 推过 controlKey，否则 `set-credential` 对 LAN 持续开放；② 断线/停摆的 k2subs 路由器：窗口开到下次成功刷新为止。威胁模型判定可接受：攻击者需持续在受害者 LAN 内，且该窗口在本设计前是「全部端点永久无鉴权」——严格变好。另注：无 CORS 不阻止跨源**副作用**——unbound 窗口期内，LAN 内任意设备浏览器访问恶意网页即可发出 `set-credential` simple POST（无 preflight），攻击面等同「LAN 内攻击者」，同窗口同判定。
- **中间件**：除 `/ping` 与 `/`（静态页）外，所有端点要求 `Authorization: Bearer <controlKey>`，本地比对 hash，失败返回 HTTP 401。未绑定 key 状态下 `set-credential` 免鉴权（TOFU 入口），其余端点即使未配置也 401。**loopback（127.0.0.1/::1）来源豁免鉴权**——`k2r up/down/status/reset` CLI 走 localhost IPC，且能在本机发起 loopback 流量者已拥有路由器本体。
- **解绑**：新增 CLI `k2r reset`——清除凭证、controlKeyHash、状态文件。app 侧「解除绑定」调用需鉴权的 reset API 端点（`/api/core` action `reset`）。

### 3.3 `/ping` 扩展（发现签名）

```json
{ "k2r": true, "version": "0.4.7", "configured": true, "name": "OpenWrt-MT7981" }
```

无鉴权（发现必需）。不暴露 udid、订阅、LAN 拓扑等敏感信息。`name` 取 hostname。

### 3.4 锚点地址拦截（2026-07-18 新增，发现+控制唯一入口）

- **锚点常量**：`10.17.79.1:1779`（RFC1918 内罕用段，避开常见家用网段；`17.79` 呼应端口 1779）。app 侧所有发现与控制请求都发往该地址——`lanIP` 概念从 app 侧消失。
- **k2r 侧规则**：PREROUTING 拦截目标为 `10.17.79.1` tcp/1779 的**转发流量** REDIRECT/DNAT 到本机控制端口。因流量物理上逐层经过网关链，k2r 在任意一层（含多层 NAT 上游）均能拦截——这是网关探测做不到的覆盖。规则随 gateway 启动装载、退出清理，nftables/iptables 双支持跟随现有防火墙管理惯例。
- **旁路由拓扑**（k2r 不在转发路径上）：锚点不可达，Router tab 不出现——接受的取舍（该形态用户可用 `k2r` CLI 管理）。
- **撞段兜底**：真实 LAN 恰好使用 `10.17.79.x` 时，锚点请求到达真实设备——`/ping` 的 `k2r:true` 签名校验不通过即视为无路由器，无害降级。
- **app 端 VPN 开启时**：锚点可达性要求 TUN 路由排除该地址（私网段常规排除）——见开放项 §10.4。

### 3.5 明确不做

- 不做 CORS（app 走原生 HTTP 桥，无浏览器 origin 语义）。
- 不动 `k2.local` mDNS responder 与 beacon 上报逻辑（beacon 上报保留，但 app 侧不再消费其发现结果）。
- 不做域名 / DNS 劫持——锚点是 IP 直连，TCP 建连前无需任何解析。

## 4. Center 侧（k2app 仓库，`api/`）

- 新增 `POST /api/user/router-control-key`（用户鉴权）：账号级 controlKey，首调生成、后续幂等返回明文 key。同账号多设备天然共享控制权。存储：User 关联新字段/表存 key（server 端存明文或可逆——需向 app 重复下发；见开放项 §10）。
- 新增 `POST /api/user/router-control-key/reset`（用户鉴权）：轮换。
- `/api/subs` 响应（k2r 订阅信道）新增 `controlKeyHash` 字段：按请求 udid 归属账号查 key、下发 sha256。老客户端忽略未知字段，无兼容问题。**gateway 分支 mint-on-serve**（2026-07-17 终审补）：k2r 客户端命中 gateway 分支时若账号尚无 key，则先幂等铸 key 再下发 hash——保证「用户从不打开新 app」的存量 k2subs 路由器也能闭合 TOFU 窗口（否则 §3.2 legacy 保证落空）。shared 分支（app 客户端）保持只读注入，不铸 key。
- beacon/discover 端点不变。

## 5. App 侧发现（k2app 仓库，webapp + bridges）

新增 `webapp/src/services/router-service.ts` + `router.store.ts`（zustand）。

### 5.1 锚点探测（唯一机制，2026-07-18 修订）

发现 = 原生 HTTP 桥 GET `http://10.17.79.1:1779/ping`，1.5s 超时，校验 `k2r: true`（见 §3.4）。锚点是常量，因此：

- 无探测链、无降级层级——通就是有路由器，不通就是没有（或 k2r 不在转发路径上）。
- 无 lanIP 缓存与失效逻辑——持久化仅剩 `{name, configured}` 等展示态。
- 控制请求与发现同一 URL，`Bearer <controlKey>` 鉴权（§3.2）不变。
- **已实现未消费的桥能力**：`_platform.getDefaultGateway()`（B4 桌面 / B5 移动已落地）保留为 IPlatform 可选能力，本设计不消费；后续 Router tab 展示本地网络信息或诊断可用。router-service 不引用它。
- app 侧不再调用 `/api/pair/discover` 做发现（Center beacon 上报与端点保留不动）。

### 5.2 触发时机

App 启动、回前台、Router tab 手动刷新。探测结果进 `router.store`：`none / unconfigured / online / offline`。

### 5.3 传输层

`routerHttp` 适配器：走 `_platform.routerRequest`（B4/B5 已落地：Tauri 自建 Rust command（reqwest，禁 redirect）、Capacitor 用 `CapacitorHttp`，两端均强制 `http://` + 私网 IPv4 字面量的 SSRF 门——锚点 `10.17.79.1` 天然通过），`make dev-standalone`（纯浏览器开发模式）降级 `fetch`（该模式下路由器功能仅限开发验证，不承诺 mixed content 场景）。所有请求注入 `Authorization: Bearer <controlKey>`。controlKey 缓存于现有 app 存储（桌面加密 storage / 移动 Preferences）。

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

检测到「锚点可达的已配对 k2r 且其隧道 connected」且本机 VPN 未连接时，Dashboard 未连接态不显示裸的「未连接」，叠加横幅：「已由路由器接管保护 →」，点击跳 Router tab。消除「在路由器 WiFi 下看到未连接以为没被保护」的误解。

### 6.3 互斥强提醒

- 触发条件：**双连即将成立**的两个瞬间——
  - Router tab 内点连接，而本机 VPN 已连接 → Dialog：「路由器接管后本机无需再开 VPN，双重代理会变慢。断开本机 VPN？」〔断开并继续（默认）/ 保持双连〕。
  - Dashboard 点连接，而当前网关是已连接的 k2r → 反向文案同款 Dialog。
- 不在 tab 切换时弹（有 k2r 但无有效套餐的用户日常用本机 VPN，查看 Router tab 不应被打断）。
- 一律 MUI Dialog——Capacitor WebView 静默吞 `window.confirm`（返回 false），constitutional 约束。

## 7. 错误处理

| 场景 | 行为 |
|---|---|
| 锚点探测失败 | Router tab 离线态（曾配对）或不出现（从未配对）；后台静默按触发时机重试 |
| 控制请求 401 | 先向 Center 重取 controlKey 重试一次（key 轮换场景）；仍 401 → 「重新配对」CTA |
| 控制请求超时/网络错 | 标记 offline，轮询继续（下轮恢复即回 online） |
| 推送的 controlKey 与账号不一致 | 首次订阅刷新后被 Center 权威 hash 覆盖 → app 旧 key 401 → 自动重取收敛（无回滚分支） |
| 路由器隧道错误 | 复用现有 EngineError code→i18n 文案映射（1xx 网络 / 4xx 客户端 / 5xx 服务端语义不变；402 引导购买） |
| mint 失败（无套餐） | 按 Center 错误文案引导购买，Router tab 管理功能（设备/OTA）不受影响 |

## 8. 测试

- **k2 gateway 单测**：鉴权中间件（401 / 未配置 set-credential 免鉴权 / hash 比对）、TOFU→Center 校验回滚、`/ping` 应答形态、`k2r reset`、订阅响应 controlKeyHash 落盘、**锚点 DNAT 规则装载/清理（规则文本级断言 + gateway-uat 容器内实测拦截）**。`make gateway-check`。
- **gateway-uat（Docker）**：headless 验收——`/` 返回静态页非 SPA、无鉴权请求 401、set-credential→up 全流程、容器内经转发路径请求锚点地址可达 `/ping`。
- **Center（api/）**：control-key 幂等、reset 轮换、`/api/subs` 按 udid 归属下发 hash。
- **webapp vitest**：router-service 锚点探测（可达/超时/签名不符三态）、router.store 四态迁移、Router tab 三态渲染、互斥 Dialog 触发矩阵（4 组合）、401 重取重试。
- **真机 smoke**（release 信心门槛）：iOS 本地网络权限弹窗路径、Android CapacitorHttp 到 LAN、桌面 Tauri 到 LAN。

## 9. 明确不做（YAGNI）

- Center 中继远程管理（人在外控制家中路由器）——API 形态不排斥未来叠加（同一套 JSON API 换传输），本期不做。
- app 内 mDNS/Bonjour browsing、`k2r.local` 域名、DNS 劫持、配对码、网关探测消费与 beacon 发现消费（getDefaultGateway 桥与 beacon 端点保留，均无 app 侧发现消费方）。
- k2r 面板 UI 的任何形态复活。
- SSE 作为 app 依赖（保留端点但不消费）。

## 10. 开放项（进实现计划前需拍板）

1. **controlKey 服务端存储形态**：需向同账号 app 重复下发明文 key → Center 存明文（或可逆加密）。风险可接受（key 只控制家庭路由器，且 Center 本就托管订阅凭证）；实现时与现有凭证存储惯例对齐。
2. **`/api/subs` 响应字段的 JSON key**：遵循 Go snake_case → 桥不经手（k2r 内部消费），直接 `control_key_hash`。
3. **实现拆仓顺序**：k2 仓库（gateway 鉴权 + headless + subs 字段消费）先行合并出测试版二进制 → k2app（api/ 下发 + webapp/bridges）跟进。k2 submodule 在父仓只读，两边各走独立 worktree/分支。锚点 DNAT 拦截（§3.4）作为 k2 仓库独立增量任务（2026-07-18 修订产生，晚于首轮 k2 合并）。
4. **TUN 路由对锚点地址的排除（2026-07-18 新增，实现前必须验证）**：app 端本机 VPN 开启时，到 `10.17.79.1` 的流量必须走物理接口而非 TUN，否则「VPN 开着时发现/控制路由器」失效。需查 k2 引擎 TUN 路由表是否排除 RFC1918（或显式排除锚点 /32）；若未排除，k2 侧需补路由排除。

## 11. 迁移与兼容

- 老 k2r（有面板）→ 新 k2r（headless）经现有 OTA 通道升级；升级后书签用户看到静态提示页。
- 老 app + 新 k2r：老 app 的 AddRouterCard 仍指向 `http://lanIP:port`，落到静态提示页——可接受的过渡降级。
- 新 app + 老 k2r：老 k2r 无锚点 DNAT 规则 → 锚点不可达 → Router tab 不出现（比旧探测更彻底的静默降级）；AddRouterCard 文案提示升级路由器固件（兜底）。
- 版本门槛：Router tab 功能要求 k2r ≥ 本设计落地版本。
