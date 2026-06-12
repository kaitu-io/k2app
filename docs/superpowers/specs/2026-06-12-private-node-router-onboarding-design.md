# 专属线路路由器 onboarding（纯软件 BYO 单国家）Design

> 决策日期 2026-06-12。本文是 **Plan 5b（任务 #13）** 的设计记录：把"配额型专属线路"做成路由器（k2r）可用形态，让用户用**自带 OpenWrt 路由器**完成开通，全程软件、零 OpenWrt 系统注入。承接 Option D 计量断流（`2026-06-12-private-node-usage-reporting-in-k2s-design.md`）。

## 1. 产品框架（本次对齐结论）

路由器版重新定位为**定制线路版** —— 买了**独立 VPS 专属线路**的用户可用路由器全屋翻墙。围绕"会不会用超流量"的担心，逐项拍板如下：

| 决策点 | 结论 |
|---|---|
| 云账单归属 | **我们代购代管、我们付云账单** → 我们承担超额风险 → 必须硬断流 |
| 产品承诺 | **配额型「专属 X TB/月」**（非不限量）。X < VPS 真实额度，留安全边际 |
| 烧钱风险 | **0** —— Option D 进程内断流（Center 判 95%/100% → k2s `SetAccepting(false)` 拒新连接）已建，把流量焊死在配额内 |
| 撞墙体验 | 路由器是**全屋共享**，配额烧得快且无感；断流时**无共享池兜底**（能力矩阵：路由器只连专属节点）→ 全家海外访问一起停 |
| 预警 | **80%/95% 预警邮件 = P0**（本设计含），让全家提前知道要撞墙 |
| "充值"语义 | **作废** —— 配额绑物理 VPS 额度，卖"+GB"要么我们吃超额要么是假的。要更多流量只有 **① 升级实例档位 ② 加购第二条线路**（Phase 2，复用 provisioning 流水线）。撑不到月底的诚实答案 = **等月初 epoch 重置（免费）** |

**配额只数代理流量**：Option D 在 k2s 数 rx/tx，国内直连不经过节点 → 爱奇艺等国内流量不烧配额（卖点，写进文案）。

## 2. 范围

**本设计（#13）**：纯软件、BYO（用户自刷 OpenWrt + k2r）、**单国家**专属线路的 onboarding + 面板入口发现 + 95% 预警。

**显式不在本设计**：
- **多 SSID 多国家**（Plan 7）—— 软件可做（UCI/nftables/k2r 编排），但撞 BYO 硬件长尾（VLAN/DSA、多隧道算力、多播跨 VLAN）。归 Plan 7，并在那里加：**硬件能力探测门 + 认证路由器清单**（可选未来卖预配置路由器高端 SKU）。
- **升级/加购实例**（Phase 2）。
- **真机 smoke**（smoke-gated；在跑通前 #13 按发布框架封顶 6–7/10）。

## 3. 架构

### 3.1 关键事实（recon 坐实，地基不动）

- **路由器与 app 共用认证机制**：登录拿 JWT → `Basic(UDID:JWT)` 调 `/api/subs`，请求头 `X-K2-Client: kaitu-router/...` 令 `Device.IsGateway=true`（`middleware.go:48,93`）。Center 在 gateway 分支用 `ResolveGatewayPrivateTunnels`（`entitlement_resolver.go:14`）**只返回该用户的专属节点**（`api_subs.go:154`）。**无独立"路由器凭证类型"，不必新建匿名凭证签发面**。
- **专属节点 ↔ 用户绑定已建**（Plan 2）：claim 流水线落 `private_owner_user_id`（`model_private_node.go:35`、`slave_api_node.go:220` CAS 认领）。
- **k2r 是纯凭证消费者**：`k2r setup <url>`（`cmd/k2r/main.go:138`）消费预给的 `k2subs://`/`k2v5://`，持久化 `/etc/k2r/state.json`（`gateway/state.go`）。**无登录能力**、**不发 X-K2-Client**、嵌入式 webui 只读（`webui/embed.go:14`、`gateway/gateway.go:154`）。k2r 已在 LAN DNS 路径（`DNSRedirect`/`DNSPort`）。
- **配额数据 Center 已持有**：Option D 上报 `cumulative_bytes`，Center `/slave/usage` 算 `quota_used/quota_total` 判 verdict。

### 3.2 数据流

```
① 买配额型专属线路 → 节点开通并绑定 private_owner_user_id            ✅ 已建
② 用户自刷 OpenWrt+k2r，路由器跑起来                                用户侧
③ webapp(已登录)「添加路由器」→ Center 铸造 IsGateway=true 设备
   + 组装 k2subs://{UDID}:{JWT}@center.kaitu.io/api/subs              🆕 A
④ webapp 显示该 URL(复制按钮)+ 图文；并尝试公网-IP 发现弹
   「检测到你局域网的路由器 http://192.168.x.1:1777 【打开】」        🆕 B
⑤ 用户打开 k2r 面板(发现见 §3.4)→ /setup 页粘贴 URL → k2r
   持久化 state.json → k2r up                                        🆕 C
⑥ k2r 调 /api/subs(带 X-K2-Client: kaitu-router)→ gateway 分支
   ResolveGatewayPrivateTunnels 返其专属节点 → 连上                  ✅ 已建
⑦ k2s 侧 Option D 逐字节计量 + 80/95% 预警 + 100% 硬断流            Option D 已建;预警 🆕 D
```

### 3.3 组件

**A. 🆕 Center：铸造 gateway 凭证端点** `POST /api/user/gateway-credential`
- 路由：`/api` 组（`ApiCORSMiddleware`），中间件 **仅 `AuthRequired()`**（**不加 `EnforceDeviceClass`** —— 调用方是用户的普通 app/web 设备，不是路由器）。信任边界 = 已登录用户会话，非新匿名面。
- 行为：`ReqUser(c)` 取当前用户 → 校验有 **active 专属线路**（`PrivateNodeSubscription` status=`PNStatusActive`，`model_private_node.go`）。无 → `Error(c, ...)` 402/403。有 → 创建 **router-class `Device`**（`IsGateway=true`，`AppPlatform="router"` 等），**服务端生成 UDID**（recon 坐实 UDID 平时由客户端提供，此处无客户端 → 服务端生成，如 `"router-"+随机`，写 `Device.UDID` 唯一索引），调 `generateTokens(ctx, userID, deviceID, roles)`（`logic_auth.go:103`）取 JWT，用 `injectSubsCreds`（`api_subs.go:84`）组装并返回 `k2subs://{UDID}:{accessToken}@{centerBase}/api/subs`。
- **centerBase**：取 Center 对外基址（配置项，与 `K2_USAGE_REPORT_URL`/对外 host 同源）；scheme 为 `k2subs`。
- **用 `k2subs://` 不用 `k2v5://`**：走 Center 已备 gateway 路径（`/api/subs` gateway 分支 `api_subs.go:154`）、跟随节点地址变更（弹性 IP/重开）、吃 Plan 4 token 滚动续期。
- 幂等：**一条线路一个 gateway 设备**；重铸 = 撤旧 router 设备（按 `idx_user_gateway`）后建新。
- 不变式：**铸造 gateway 设备不挤掉用户的 app 设备**。recon 显示 `Device` 有 `idx_user_gateway(UserID,IsGateway)` 复合索引，且 app 设备限额逻辑（`checkDeviceLimitOrKick`）按设备清点 —— 实现时**显式读该函数**确认 router-class 不计入 app 限额、不触发踢出；测试断言"铸造 router 设备后用户 app 设备仍在"。

**B. 🆕 webapp：「添加路由器」UI + 公网-IP 发现**
- 在已建的专属节点管理区加动作：调 A，展示 `k2subs://` URL（复制按钮）+ 图文（"打开路由器面板 → /setup → 粘贴"）。
- **公网-IP 发现**：页面加载时调 Center beacon 查询（§3.4 ①）；命中则显示「检测到你局域网内的路由器：`http://{lanIP}:1779` 【打开】」，点击直达 LAN 面板。多候选（CGNAT）→ 列出让用户挑。
- 凭证经 LAN HTTP 粘贴可接受（家庭同信任域）；webapp 端正常 HTTPS。

**C. 🆕 k2r：面板 LAN 暴露 + 本地 /setup + 发现 + 凭证录入**（k2 子模块）
- **⚠️ 面板 LAN 绑定（最关键修正）**：recon 坐实面板+`/api/core` 现绑 `127.0.0.1:1779`（**loopback-only**，`gateway/config.go:28,57`；`cmd/k2r/main.go:132` `g.Run(cfg.Listen)`）→ **LAN 设备根本够不到**。改：HTTP server 额外/改为绑 **LAN 网关 IP** 上（端口仍 1779，避开 LuCI :80）。**安全硬约束:只绑 LAN-facing 地址,排除 WAN/public 接口**(用 `DiscoverLANSubnets`/接口枚举区分 LAN vs WAN，`gateway/lan.go:11`)。控制面绑 LAN = 消费级路由器常态(同 wifi 即可访问);WAN 永不暴露。**v1:set-credential 在 LAN 不加面板口令(同信任域,消费级路由器初装常态)**,面板口令列 follow-up。
- **/setup 页**（shared `webapp/` 新页，`platformType==='gateway'` 时显示；recon 确认嵌入式 webui = `webapp/dist` 同源，`webui/serve.go:20`）：粘贴框 + 「连接」。
- **set-credential = `/api/core` 新 action**（`gateway/api.go:45` switch 加 `case "set-credential": g.handleSetCredential(...)`）：校验 URL 可解析为 `GatewayConfig`（`gateway.GatewayFromURL`）→ `saveState`（`gateway/state.go:33`）→ 触发 `doUp`。**凭证绝不落 k2r 日志**。
- **发 `X-K2-Client: kaitu-router/<ver> (<platform>; <arch>)` 头**（recon 确认现在没发）：在 `config/subscription.go:193` `SetBasicAuth` 后加 `req.Header.Set("X-K2-Client", ...)`，值由 `g.version`/`g.arch`（`gateway.go` 注入）构造。
- **mDNS responder**：进程内应答 `k2.local`→网关 LAN IP（§3.4 ②）。**无需新依赖** —— recon 显示 `miekg/dns v1.1.72` 已在 `go.mod`（手搓 5353 多播应答），且 `pion/mdns/v2` 已是 indirect dep（可提升为 direct）。零 OpenWrt 配置注入。
- **beacon 上报**：k2r 当前**无 Center HTTP client、无可用 Center 基址**（recon 坐实）。新增一个**编译期烘焙的 Center 基址常量**（ldflags `-X` 或 const，如 `https://kaitu.io`），未配置时（及配置后周期）POST `{lanIP, port:1779}` 到 Center beacon 端点（§3.4 ①）。这是 k2r 新增的唯一 outbound Center 调用。

**D. 🆕 Center：80%/95% 预警 worker + beacon 端点**
- **配额数据源（修正）**：recon 坐实用量在 **`CloudInstance`** 行（`traffic_used_bytes`/`traffic_total_bytes`/`traffic_epoch`，`model.go:1179`），非 `PrivateNodeSubscription`。percent = `used*100/total`（整数运算，参 `slave_api_usage.go:86`）。
- **预警 worker**：Asynq cron（`asynq.Cron(...)`，`worker_integration.go:35` 范式，如 `*/30 * * * *`）扫 `PNStatusActive` 专属线路 → join `CloudInstance` → 算 percent。跨 80%/95% 阈值发邮件。**去重列加在 `CloudInstance`**：`Warn80SentEpoch int64` / `Warn95SentEpoch int64`，与 `TrafficEpoch` 比对（`!=` 才发，发后置为当前 epoch；epoch 变即自然重置）。
- **邮件（修正）**：用 **Go-code 模板**（recon 坐实邮件是泛型 `EmailTemplate[T]` Go 模板，非 DB EDM）。新增 `privateNodeTrafficWarningTemplate`（参 `verificationCodeTemplate`），经 `emailToUser(ctx, userID, tmpl, meta)`（`logic_email.go:232`）发给 `PrivateNodeSubscription.UserID`。中文用"开途/专属线路/路由器"，**禁裸"Kaitu"**。
- **beacon 端点**（§3.4 ①）：`POST /api/pair/beacon`（`/api` 组，**无 auth** —— k2r 未配置时无凭证；CORS 不挡 server-to-server）。按请求**公网源 IP**（`c.ClientIP()`）记 `{lanIP, port, ttl}` 入 Redis（短 TTL 如 10min，限速）。
- **beacon 读端点**：`GET /api/pair/discover`（`AuthRequired`），按 web 请求公网 IP 取候选 `{lanIP, port}[]` 返 webapp。**发现-only，绝不碰凭证**。

**E. 真机 smoke**（smoke-gated，§2）。

### 3.4 面板入口发现（三层，全程不依赖 unicast DNS）

**问题**：用户找不到 k2r 面板入口；OpenWrt 版本太多注入不进 dashboard；且用户 DNS 不一定走路由器，甚至 DoH/DoT（加密 DNS 绕过路由器）。故**不能把发现挂在 unicast DNS 上**。

| 层 | 机制 | 抗性 | 局限 |
|---|---|---|---|
| **① 公网-IP 关联发现（主力）** | k2r 发 outbound beacon（LAN IP）→ Center 按公网源 IP 关联 → webapp 弹「打开你的路由器」链接 | **DoH 无关**（非 DNS）；**Android 照样工作**（本质是 webapp 显示链接） | CGNAT 多户共享公网 IP 可能误配 → **仅提示+用户确认，绝不据此塞凭证**；浏览设备开 VPN 换出口；纯 IPv6 各设备地址不同 → 降级多候选 |
| **② mDNS `k2.local:1779`** | k2r 进程内 mDNS responder，链路本地多播 | **零 OpenWrt 注入**；**DoH/DoT/手设 8.8.8.8 全拦不住**（`.local` 与 unicast 解析器两套）；永不出 LAN | **Android 浏览器 `.local` 残缺**（软肋，中国 Android 盘痛点）；AP 多播隔离/部分 Linux 无 avahi |
| **③ 认证机型固定 IP（地板）** | 清单写死 `http://192.168.8.1:1779` | **零名字解析、零 DNS 依赖**，永远能用 | 用户改过网关 IP（认证机型默认不改） |
| ④（可选）captive-portal 探测拦截 | k2r 网关拦 OS captive 明文探测 → 弹面板 | DNS 无关 | OpenWrt 跨版本实现重，列可选增强 |

**端口**：统一 `:1779`（k2r 面板/控制面端口，recon 坐实，**非 1777**——1777 是桌面 daemon 端口），不跟 LuCI 的 uhttpd 抢 :80。**前置依赖：面板须先 LAN 绑定（组件 C 的最关键修正），否则三层发现把用户引到一个 loopback-only 够不到的入口。**
**发现 ≠ 鉴权（硬边界）**：①②③ 只负责"把面板入口递到用户面前"；真正鉴权恒走"用户已登录铸造凭证 → 自己面板粘贴"。即便发现配错（CGNAT），最坏只是给错一个打开链接，泄不了凭证。
**`router.kaitu.io` unicast** 仅在设备恰好走路由器 DNS 时锦上添花，**不作依赖**；公网可挂一个 kaitu.io 帮助页，把"既没走路由器 DNS 又没 mDNS"的罕见设备引导到 `k2.local:1779` 或固定 IP。

## 4. 安全考量

- **A 铸造端点**：authed web 会话内为自己账号创建设备 —— 等同 app 登录建设备，trust 边界是现有会话。轻量安全复核即可，非重型新匿名签发面。
- **beacon 端点**：未配置 k2r 无凭证 → 端点近乎无鉴权，只存 `{publicIP→lanIP,port}`。攻击面：伪造 beacon 让受害者 webapp 显示错误 LAN 链接 → 最坏点开打不开/打开错设备，**无凭证暴露**（发现≠鉴权）。缓解：短 TTL + 限速 + 多候选呈现。
- **⚠️ 面板 LAN 暴露**：面板从 loopback-only 改为 LAN 绑定后，控制面（set-credential / up / down / status）对**整个 LAN 可达** = 同 wifi 的人都能操作。这是消费级路由器常态（每台路由器后台都如此）。**安全硬约束：绑定地址必须排除 WAN/public 接口**（只绑 `DiscoverLANSubnets` 命中的 LAN 接口 IP），WAN 永不暴露 —— 这是防"控制面被公网打"的关键。set-credential 在同信任 LAN 内 v1 不加面板口令（消费级初装常态）；面板口令 = follow-up。测试守卫：绑定地址集合不含公网 IP。
- **凭证卫生**：`k2subs://` 含 `UDID:JWT` bearer。k2r 端不记录；LAN HTTP 粘贴限家庭同信任域。
- **设备类别**：router-class 与 app-class 隔离，铸造不踢 app 设备（测试守卫）。

## 5. 测试策略（test = gate）

| 层 | 测试 | gate |
|---|---|---|
| A 铸造端点 | 有 active 线路→返合法 k2subs URL（解析出 UDID/JWT/host）；无线路→402/403；重铸→旧设备失效新 JWT 生效；**铸造不踢 app 设备**（建 router 设备后 app 设备仍在） | 凭证铸造正确 + 设备隔离不变式 |
| C 面板 LAN 绑定 | 绑定地址集合含 LAN 接口 IP、**不含 WAN/public IP**（mock 接口枚举验证）；loopback 仍可达 | LAN 暴露 + WAN 排除（安全硬约束） |
| C k2r set-credential | 合法 URL→写 state.json→up；非法 URL→拒不改状态；**凭证不入日志**（捕获日志断言）；X-K2-Client 头出现在 /api/subs 请求 | 录入安全 + 头加固 |
| C mDNS | responder 应答 `k2.local`→网关 LAN IP（mock/loopback 验证） | 发现② |
| D 预警 worker | 跨 80%/95% 各发一次；同 epoch 不重发（去重列）；epoch 变后重置可再发；未跨阈值不发 | 预警正确 + 去重 |
| D beacon 关联 | 同公网 IP beacon→webapp 查询命中；不同公网 IP 不串；TTL 过期不返；多 beacon→多候选 | 发现① + CGNAT 隔离 |
| 回归 | /api/subs gateway 分支仍返专属节点；app 登录/设备流不回归 | 不回归 |

## 6. 部署

- **k2 子模块**（C：面板 LAN 绑定 + /api/core set-credential + mDNS + beacon + X-K2-Client + 烘焙 Center 基址）在 k2 仓内 commit，**parent submodule 指针保持 unstaged**。k2r 二进制随版本重编 + 路由器升级。`/setup` 页在 `webapp/` 编译进 `k2/webui/dist`。
- **Center**（A 端点 + D 预警 worker + Go 邮件模板 + beacon 双端点）随 center 部署。**`CloudInstance.Warn80SentEpoch`/`Warn95SentEpoch` 两列经 AutoMigrate 加列（additive 非破坏，无需手动迁移）**。
- **webapp**（B：添加路由器 UI + 公网-IP 发现 + `platformType==='gateway'` 的 /setup 页）随 webapp bundle 发版（同时编译进 k2/webui）。
- **DNS/ops**：`k2.local` 为 mDNS 无需公网记录；可选公网 `router.kaitu.io` → kaitu.io 帮助页。
- 共享池/普通 app 用户：A/B/C/D 全不触达，零变化。
