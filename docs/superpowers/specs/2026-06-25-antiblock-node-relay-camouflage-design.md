# Antiblock 强化：节点作伪装入口反代 Center API + 自愈入口池

**Date:** 2026-06-25
**Status:** Design approved, pending spec review → implementation plan
**Author:** brainstorming session (David + Claude)

---

## 1. 背景与问题

### 1.1 触发事件

用户 `53853717@qq.com`(userId 7269, iOS 0.4.5)无法拉取节点列表、提交反馈失败。设备日志(`mobile/0.4.5/f7c6dbaa.../logs-020326-*.zip`)坐实：

- **所有** Center API 请求都打到 `d1l0lk9fcyd6r8.cloudfront.net`,**每一条**连接级失败(`Load failed` / `code -1`),不是 HTTP 错误码 —— 请求根本没到达。
- 同期 S3 直传(`kaitu-service-logs.s3...`)**成功**(日志能上传),但经 CloudFront 的 API **全失败** → 锁定是 **CloudFront 域名被针对**(DNS 污染 / SNI 阻断 / IP 封锁)。
- Geo 落 `country=cn`;同期隧道握手 `x509: certificate signed by unknown authority`(QUIC,TLS 中间人)+ `connection reset by peer`(TCP-WS,RST 注入)。

经 center-ops 核实:Center 服务端两台 EC2 均健康(HTTP 200, ~4ms),正在正常服务其他用户。**故障定性 = NETWORK(GFW),非服务端。**

### 1.2 根因：控制面是唯一没有伪装的环节

数据面(k2v5 隧道)早已具备完整抗封能力(ECH / uTLS / QUIC / 跳端口 / 证书 pinning),GFW 这套手法对它无效。但**控制面(Center API)用的是 WebView 的裸 `fetch()`**(系统 DNS + 系统 TLS),直连一个 CloudFront 域名,毫无伪装 —— 它是整个系统抗封链条上唯一的薄弱点。

### 1.3 现有 antiblock 的两个缺陷

`webapp/src/services/antiblock.ts`:

1. **多 CDN 只做了一半**:6 个 jsdelivr 系镜像并发竞速只用于**取配置文件**;解出的 `entries` 数组**只用 `entries[0]`**(`antiblock.ts:145`),第二个入口 `k2.52j.me` 是死配置永不使用 —— 无入口级故障切换。
2. **缓存即永久**:解析出的入口写 `localStorage['k2_entry_url']` 后**直接返回缓存、永不校验存活**(只后台 refresh,而 refresh 又只取 `entries[0]`)。一旦缓存了被封域名 → 永久卡死。

线上 config.js 当前实际内容(已解密验证):
```json
{"entries":["https://d1l0lk9fcyd6r8.cloudfront.net","https://k2.52j.me"]}
```

---

## 2. 目标与非目标

### 2.1 目标

- **控制面可用性 = 节点车队可用性**:只要有任意一个节点可达,Center API 就可达。
- 在 GFW 实施 DNS 污染 / SNI 阻断 / IP 封锁 / TLS 中间人 / RST 注入的网络下,控制面请求仍能完成。
- 对未被封锁的正常用户**零影响**(不增加延迟)。
- 冷启动(全新安装)也能起步,且**根除 CDN 缓存陈旧**问题。

### 2.2 非目标

- 不改变数据面(隧道)行为。
- 不把全部设备流量导经节点 —— 只代理 Center API 这一类小 JSON。
- 不追求节点对控制面流量"看不到明文"(见 §3.3 决策:节点本就是中间人且已持有凭证)。

---

## 3. 关键设计决策(已与用户敲定)

| # | 决策 | 选择 | 理由 |
|---|------|------|------|
| D1 | 入口形态 | **节点作伪装入口,反代到 Center** | 复用现成 k2v5 伪装栈 + 节点车队 IP 多样性,GFW 要封控制面就得封整个 VPN(它封不动) |
| D2 | 中继方式 | **节点终止 TLS 后 HTTP 反代到 Center(做法一)** | 节点本就是中间人;连任何节点本就要把同一把设备 token 交给它,节点看到 API 明文**不是新增暴露**。最省代码 |
| D3 | 节点路由信号 | **内层 SNI**(`tlsConn.ConnectionState().ServerName`) | 外层 ECH SNI=`cloudflare-ech.com` 对 GFW 伪装;内层 SNI=`k2.52j.me` 当路由标签,节点据此识别"这是控制面" |
| D4 | 客户端入口管理 | **antiblock 升级为自愈加权入口池** | 逐个试连、降权死 IP、每次 `/api/tunnels` 成功就灌入节点 IP、持久化 |
| D5 | IP 试连方式 | **只走伪装 wire 通道**(不裸连) | 裸连 IP 同样会被 MITM/RST;只有 ECH 伪装握手能突破 |
| D6 | 冷启动分发 | **DNS 指针(DoH 阿里) + 不可变 CDN 文件** | 根除缓存陈旧;保留中国友好 CDN 源;复用已有"从 DNS 取配置"(ECH)模式 |
| D7 | 触发时机 | **直连失败才走节点中继**(fallback) | 不拖慢正常用户 |
| D8 | native 配合 | **必须**:伪装握手只有 Go(wire)会做,WebView fetch 做不了 | 桌面走 daemon 端点,移动走 appext 导出函数,webapp 只做兜底编排 |
| D9 | 敏感端点是否走中继 | **全部走中继**(含密码/支付) | 节点本就是中间人且已持设备凭证;明文密码/支付暴露由 R1 补偿控制(审计+per-account 限流)兜底 |
| D10 | 冷启动内置锚点 | **保留 2-4 个稳定锚点 IP** | DNS 指针只解新鲜度;为对抗"全新安装+全 CDN 被封"永久砖,需内置伪装锚点兜底(运维维护稳定 IP) |
| D11 | ECH 分发 | **随描述符内嵌**(`{ip,pin,ech,hop}`),不实时拉 DNS | `BuildClientTLSConfig` 强制要求 ECH 入参;实时拉 DNS 会新增可污染依赖、自毁伪装(评审 #1) |

### 3.3 D2 决策备注(记录在案)

节点终止 TLS 后能看到 API 明文 + Bearer token。用户判定可接受:**连任何节点本就需要把同一把设备 token 交给它(k2v5 连接凭证里就带着),节点早已持有该凭证,故非新增暴露。**

> ⚠️ 残留风险(记录,当前不阻塞):若未来某接口传输"比连节点凭证更值钱的东西"(如登录明文密码、更高权限令牌),节点也会看到。当前登录为邮箱验证码、无密码(`hasPassword:false`),基本不涉及。实现时应避免让需要更高保密级别的流(若有)走此通道。

---

## 4. 架构总览

```
正常用户(未被封):
  WebView fetch() --直连--> Center entry(CloudFront / k2.52j.me)   [快路径,不变]

被封用户(GFW):
  WebView fetch() --直连失败(code -1)-->
  cloudApi 兜底 --> native relay-fetch(从入口池挑节点 IP) -->
    [Go wire] 客户端 --外层 ECH(SNI=cloudflare-ech.com) / 内层 SNI=k2.52j.me--> 节点(:443)
    节点 handleK2V5 终止 TLS --读内层 SNI=k2.52j.me--> HTTP 反代 --> https://k2.52j.me:443 (Center ALB)
  响应原路返回 webapp
```

GFW 在外面只看到「一次发往节点 IP、外层 SNI=cloudflare-ech.com 的握手」—— 与一次普通"连 VPN"字节级无法区分。

---

## 5. 组件设计

### 5.1 节点侧(k2 server) — 内层 SNI 反代

**文件:** `k2/server/server.go`(`handleK2V5`,约 653 行起)、`k2/config/config.go`

**Seam(已核实):** `handleK2V5` 在 `tlsConn.Handshake()` 成功后、HTTP/smux 分发(约 678 行)之前,`tlsConn.ConnectionState().ServerName` 即为解密后的内层 SNI。在 `learnOuterSNI` 之后插入控制面分支:

```go
// handleK2V5,握手成功 + learnOuterSNI 之后,Peek(4) 分发之前:
innerSNI := tlsConn.ConnectionState().ServerName
if origin, ok := s.matchControlPlaneRoute(innerSNI); ok {
    select {
    case s.proxySem <- struct{}{}:
        defer func() { <-s.proxySem }()
    default:
        slog.Warn("server: control-plane proxy at capacity", "sni", innerSNI)
        tlsConn.Close()
        return
    }
    s.reverseProxyHTTP(tlsConn, origin) // 终止的 tlsConn 上读 HTTP,反代到 origin(node→Center 重新发起 TLS)
    return
}
// ... 既有 HTTP/smux 分发不变 ...
```

**新增配置(`config.go`):**
```go
// ControlPlaneRoutes: 内层 SNI -> Center origin 地址。允许的控制面目的地白名单(防开放中继)。
ControlPlaneRoutes map[string]string `yaml:"control_plane_routes"`
// 示例: {"k2.52j.me": "k2.52j.me:443"}
```

**`reverseProxyHTTP`:** 在已终止的 `tlsConn` 上按 HTTP/1.1 读请求,用 `net/http` 或 `httputil.ReverseProxy` 向 `origin`(HTTPS)转发,回写响应。要点:
- 复用既有 `s.proxySem` 限流。
- node→Center 走标准 HTTPS(系统 CA 校验 Center 真证书),设置 `Host` 头为 origin 主机。
- 仅允许白名单 origin;非白名单内层 SNI 落入既有 k2v5 分发(向后兼容,正常隧道不受影响)。
- 不消耗隧道配额、不要求有效隧道 token —— 该分支在认证/smux 之前,登出/过期/超额用户也能借此到达 Center(否则登录、续费无法进行)。
- **白名单加载时校验(评审 #8)**:`ControlPlaneRoutes` 加载时拒绝私网/环回/链路本地 origin,防止配置被污染后变成打内网的开放代理。
- **结构化审计日志(评审 #8)**:每次中继记录 `{时间, 内层SNI, origin, method, path, 状态码, 字节数}`(不记 body/token),便于节点被攻破后的事后取证。

**安全:** 白名单(`ControlPlaneRoutes`)是防止节点变成开放 HTTP 代理的唯一闸门 —— 必须严格限定为 Center 主机。

**限流绕过(评审 #2,关键安全):** Center 用 `c.ClientIP()`(`api/telemetry.go:69` 等)做 per-IP 限流,且未配 trusted proxies → 取 RemoteAddr。中继后所有请求来自**节点 IP**,攻击者轮换节点即可绕过 `/api/auth/code`、`/api/auth/login` 的 per-IP 限流(验证码轰炸/账号枚举/撞库)。**对策(二选一或叠加,实现时定):**
- **首选**:Center 增加 **per-device/per-account 限流维度**(键用认证后的 device_id/user_id 或请求体里的 email,而非仅 ClientIP),登录类端点尤其需要。
- 可选:节点在中继时注入可信的 `X-K2-Relayed-By`/真实客户端标识头,Center **仅对已知节点 IP** 经 `SetTrustedProxies` 信任 —— 风险高(头可伪造、节点 IP 需严格白名单),非必要不做。**默认不设 XFF**(Gin 默认忽略 XFF,当前安全)。

**传输范围:** 控制面中继走 **TCP+TLS(tcpws)** 路径(`handleRawTCP`→`handleK2V5`)。**不覆盖 QUIC**(QUIC 内层 SNI 需 `GetConfigForClient` 回调,复杂且控制面无需 QUIC 的低延迟)。客户端发起控制面连接时固定用 TCP+TLS+ECH。

### 5.2 客户端伪装中继 — native(Go)

WebView 的 `fetch()` 做不了 ECH/uTLS/pin,所以中继必须在 Go 侧。复用 `wire.BuildClientTLSConfig`:

**核心 Go 函数(放 `k2/` 合适位置,如新增 `k2/relay/` 或 `appext`/`daemon` 共用):**
```go
// RelayFetch 经一个节点(伪装入口)向 Center 发一次 HTTP 请求并返回响应。
// 描述符: {ip, pin, ech, hop} —— 与 k2v5 URL 同构(见下方 ⚠️)。
// centerHost: 内层 SNI 路由标签(如 k2.52j.me)。
// 外层 ECH(public_name=cloudflare-ech.com,取自描述符的 ech 字段)+ pin 校验(忽略主机名)。
// 内层 SNI = centerHost(路由标签)。建立后按普通 HTTP/1.1 收发。
func RelayFetch(ctx, nodeIP, pin, ech, centerHost, method, path, headersJSON, body string) (respJSON string, err error)
```
- TLS 配置:`wire.BuildClientTLSConfig` with `Host=centerHost`、`Pin=pin`、`ECH=<描述符内嵌>`。

> ⚠️ **ECH 必须随描述符分发,不可实时从 DNS 拉(评审 #1,已核实)**:`wire.BuildClientTLSConfig`(`k2/wire/ech.go:67`)在 `cfg.ECH` 为空时**直接报错** `"wire: ECH config required (k2v5 never exposes real SNI)"`。正常客户端的 ECH 是从 **k2v5 URL 的 `?ech=` 内嵌**取得(`k2/wire/wire.go:169` `ParseURL`),**不是**连接时从 DNS 拉(`LocalECHProvider.FetchTemplate` 仅服务端生成用)。若改成实时拉 DNS,等于新增一个可被 GFW 污染的 DNS 依赖,**抵消整套伪装**。故入口池条目 = `{ip, pin, ech, hop}`,照搬 k2v5 URL 结构;ECH 随 `/api/tunnels`/CDN config 一起分发。ECH 轮换时随节点列表刷新自然更新。

- gomobile 约束:全 string/[]byte 参数与返回,内部完成整个 HTTP 往返。

**桌面(Tauri):** k2 daemon 新增端点 `POST /api/relay`(参数同上),内部调 `RelayFetch`。`tauri-k2.ts` 经 IPC 暴露。

**移动(iOS/Android):** `appext` 导出 `RelayFetch`(在 **App 进程**运行 —— VPN 关着也行,仅一次普通出站连接,与 NE/VpnService 进程无关)。K2Plugin(Swift/Kotlin)桥接,经 capacitor-k2.ts 暴露。

**桥接动作:** webapp 经 `window._k2.run('relay-fetch', {...})` 或专用平台方法调用。

### 5.3 客户端入口池 — antiblock 升级

**文件:** `webapp/src/services/antiblock.ts` + 新增 store(参考 `probe.store` / `recommendScore` 打分模式)

**入口池条目类型:**
```ts
type Entry =
  | { kind: 'direct'; url: string }                                  // 裸 fetch 快路径(如 cloudfront、k2.52j.me)
  | { kind: 'node'; ip: string; pin: string; ech: string; hop?: string }; // 经 native relay-fetch 的伪装通道
```

**持久化加权池(`localStorage`):** 每条带 `{score, lastOkAt, lastFailAt}`。
- 成功 → 提分;失败 → 降分。死 IP 自动靠后/剔除(参考 `recommendScore`)。
- 跨重启保留。
- **池上限 + 淘汰(评审 #8)**:`direct` ≤ 8 条、`node` ≤ 64 条;按 `score` 降序、`lastOkAt` 为 tiebreak 的 LRU 淘汰;7 天无成功的条目自动剪除。防 localStorage 无界增长/排序变慢。

**粘性「直连已死」标记(评审 #4,关键 UX):**
- per-network 键(如以默认网关/SSID 或简单 `k2_direct_blocked_until` 时间戳)记录「本网络直连不可用」。
- 命中标记时**跳过 direct,直接走 node 中继**,避免每个请求都先吃 15s 超时。标记 TTL ~5min,过期后再探一次 direct。
- direct 探测超时由 15s **缩短到 ~3-5s**(仅探测阶段),进一步压缩首屏卡顿。

**解析顺序(`resolveAndFetch`,取代裸 `resolveEntry`):**
1. 若粘性标记未命中 → 池中按分数排序的 `direct` 条目:裸 `fetch()`(快,正常用户在此完成)。
2. direct 失败/被标记 → 池中 `node` 条目 **Happy Eyeballs 并发竞速**(复用 `antiblock.ts` 现有 `promiseAny`,**不是逐个**,评审 #4),首个成功的 relay 胜出。
3. 任一成功 → 提分 + 作为该会话首选缓存;清/续粘性标记。
4. 全失败 → 触发冷启动重新拉取(§5.4)。

**池子充实:**
- **每次 `/api/tunnels` 成功** → 把返回节点的 `{ip, pin, ech, hop}` 灌入池(`kind:'node'`)。这保证用过一次的用户永远握有活节点入口。
- 描述符取自 tunnels 响应(k2v5 URL 的 `pin=` / `ech=` / `hop=` 参数)。

> **cloud-api 重构约束(评审 #5,保 401 刷新原子性):** `cloud-api.ts` 当前每次请求调 `resolveEntry()` 后裸 `fetch`。改为调用新的 `resolveAndFetch(method, path, body)`,把"直连→兜底中继→入口池打分"封装在内。**硬约束**:(a) 模块级 `_refreshPromise` 去重 + `requestEpoch` 守卫**保留在 cloud-api 层**,不下沉进池解析器;(b) `resolveAndFetch` 遇 `401` **不得内部重试/换节点**,必须原样上抛给 `_handle401` 处理,由它统一刷新+重试,避免刷新落到不同节点、计时器重置、并发去重失效;(c) 保持 `SResponse` 契约与 15s 总超时语义不变(粘性快速失败是缩短"探测",不改"整体请求超时")。

### 5.4 冷启动分发 — DNS 指针 + 不可变 CDN 文件

**根除缓存陈旧的机制:**

1. **不可变内容**:发布时写**带版本号/哈希的新文件名**(如 `config.v38.js`),老文件永不改写 → CDN 缓存从坏事变好事(不可变文件随便缓存,永不陈旧)。这些文件放在**保留的中国友好 CDN 源**上。
2. **极小的最新版指针**:仅"当前最新 = 第几版"这一句话放进 **DNS 记录**,客户端用 **DoH 查阿里 DNS(223.5.5.5,国内域内、墙友好)**。TTL 自控(~60s),无 CDN 缓存层,DoH 加密 → GFW 看不到 qname、无法定点污染。

**CDN 源策略:** 对齐 `k2/rule/downloader.go` 的硬集思路,**保留 config.js 现有的中国友好源**并补充异构源:
- 保留:`cdn/fastly/testingcf/gcore.jsdelivr.net`、`cdn.jsdmirror.com`、`jsd.onmicrosoft.cn`
- 补充(GitHub 反代形态):`ghfast.top/...raw...`、`gh-proxy.com/...`
- (注:`downloader.go` 因 Age 头陈旧曾移除 jsdmirror/onmicrosoft;但本设计用**不可变文件名**,陈旧问题已从根上消除,故这些中国友好源可安全保留。)

**冷启动流程:**
```
内置锚点(保底,见下) 与 DoH→CDN(优先) 并行:
DoH(多解析器) 查指针 → "最新=v38"
  → 任一中国友好 CDN 抓 config.v38.js(不可变,随便缓存)
  → 解密得节点入口列表 → 灌入入口池 → 走伪装中继连节点
若 DoH+CDN 全失败 → 回退到内置锚点节点 IP
```

**⚠️ 防陈旧 ≠ 可达(评审 #6,务必区分):** DNS 指针只解决**新鲜度**,不解决**可达性** —— `config.vN.js` 仍要经系统 DNS+TLS 从 CDN 域名取;若 GFW 把这几个 CDN 域名全 DNS/SNI/IP 封了,冷启动照样失败。本次事件已证明 CN 会狠封我们的基础设施,故"全 CDN 同时被封"并非不可能。

**内置稳定锚点(评审 #6,决策已定 = 保留):** 内置**极少量(2-4 个)承诺长期稳定 IP 的锚点节点**描述符 `{ip, pin, ech, hop}` 进二进制,作为"全新安装 + DoH + 全 CDN 皆封"时的最后退路。
- 锚点经**伪装中继**连接(同 §5.2),非裸连;只要锚点 IP 未被封即可起步。
- 运维约束(kaitu-node-ops):这几个锚点 IP 须长期不变;换 IP 需发版。ECH 可随 config 刷新覆盖,pin 用 Kaitu CA(稳定)。
- 锚点是"保底",不是主路径 —— DoH/CDN 仍是首选(IP 永远最新);锚点仅在主路径全灭时兜底。

**DoH 实现要点(评审 #6):**
- **在 native(Go)做 DoH**,不在 WebView。WebView 对 `https://223.5.5.5/dns-query` 裸 IP 取证书有 SAN 坑、且要手搓 DNS wire 格式;native 侧用现成 DNS 库干净(与 ECH-from-DNS 同栈)。冷启动若 native 未就绪,先用内置锚点起步,再由 native 刷新。
- **多解析器**(阿里 223.5.5.5 + 腾讯 119.29.29.29 等国内 DoH),不单点;定义超时(如 2s)与回退顺序。
- 指针域名用我们控制、国内可解析的域(如 `cfg.52j.me`);DoH 加密隐藏 qname,GFW 要拦只能整体封该 DoH(伤及全国)。

**CDN 戏份收敛:**
- 老用户:自愈池,**不碰 CDN**。
- 全新冷启动:才走 DNS 指针 + 不可变 CDN。
- **首次连上任意节点后**:之后配置刷新**直接走节点伪装通道**(实时、零缓存)。

**config 文件内容(演进):** 从今天的 `{entries:[域名...]}` 扩展为携带节点入口描述符:
```json
{ "v": 2,
  "entries": ["https://k2.52j.me"],            // direct 快路径(可选保留;旧客户端只读 entries[0])
  "nodes": [{"ip":"x.x.x.x","pin":"sha256:...","ech":"AEX-DQBB...","hop":"40000-40019"}, ...]  // 伪装中继入口种子(含 ECH,评审 #1)
}
```
> 向后兼容:保留 `entries` 数组(旧客户端读 `entries[0]`);新客户端读 `nodes`。

---

## 6. 数据流(被封用户完整路径)

1. webapp 调 `cloudApi.get('/api/tunnels/k2v4')`。
2. `resolveAndFetch`:池中 direct 条目裸 fetch → `code -1`(被封),降分。
3. 回退到 `node` 条目 → `_k2.run('relay-fetch', {ip, pin, centerHost:'k2.52j.me', method:'GET', path:'/api/tunnels/k2v4', ...})`。
4. native `RelayFetch`:`wire.BuildClientTLSConfig`(ECH outer=cloudflare-ech.com / inner SNI=k2.52j.me / pin=节点pin)→ TCP+TLS 连节点 IP:443 → 发 HTTP 请求。
5. 节点 `handleK2V5`:握手 → 内层 SNI=k2.52j.me 命中 `ControlPlaneRoutes` → `reverseProxyHTTP` 到 `k2.52j.me:443`。
6. 响应原路返回 → webapp 拿到节点列表 → §5.3 把这些节点 `{ip,pin}` **灌回入口池**。

---

## 7. 测试策略

| 层 | 测试 |
|----|------|
| k2 server | 单测 `matchControlPlaneRoute` 白名单;集成测:ECH 连接 + 内层 SNI=k2.52j.me → 反代命中;非白名单内层 SNI → 落 k2v5 正常分发(回归);限流(proxySem)行为 |
| k2 relay | `RelayFetch` 对一个本地起的假"节点+Center"做端到端往返;ECH/pin 失败路径;超时 |
| webapp antiblock | 入口池打分/排序/持久化;direct 失败→node 回退顺序;`/api/tunnels` 成功灌池;`resolveAndFetch` 取代 `resolveEntry` 后 cloud-api 行为(沿用现有 vitest 套路,注意 `vi.clearAllMocks` 重置实现) |
| 桥接 | desktop `/api/relay` 端点;mobile appext `RelayFetch` 导出 + K2Plugin 桥(真机) |
| 冷启动 | DoH 指针解析;不可变文件名取用;CDN 多源竞速;全源失败降级 |
| 端到端 smoke | 真机:模拟"直连 Center 不可达"(hosts/防火墙封 CloudFront),验证自动经节点中继成功 |

**置信门控(参考 david-fix / kaitu-support 阶梯):** 跨层(server+native+webapp+CDN/DNS infra),真机 smoke 未跑前封顶 6-7/10。

---

## 8. 分阶段实施(建议拆成独立 plan)

本设计跨 k2 server、k2 relay/appext/daemon、webapp、CDN/DNS 发布基建,体量大,建议拆分:

- **Phase 0(纯 webapp,快速缓解,可先发):** 修 antiblock 现有两个 bug —— `entries[]` 真故障切换 + 缓存存活校验。不依赖 native。**⚠️ 效果未经验证(评审 #7)**:`entries[1]=k2.52j.me` 与被封的 CloudFront 同属一套 ALB,GFW 若按 SNI/IP 封,它大概率一起死;该用户日志从未试过 k2.52j.me,无证据。**定位 = 低成本止血,不是根治**;发 Phase 0 前应先验证 k2.52j.me 在目标网络可达,否则真正解法是 Phase 1/2 的节点中继。
- **Phase 1(节点侧):** k2 server `ControlPlaneRoutes` + `reverseProxyHTTP` + 配置;单测/集成测。可独立部署铺到节点(向后兼容)。
- **Phase 2(客户端中继):** `RelayFetch`(Go)+ daemon `/api/relay` + appext 导出 + 桥接;cloud-api 兜底接线。
- **Phase 3(入口池):** antiblock 自愈加权池 + `/api/tunnels` 灌池 + 持久化。
- **Phase 4(冷启动分发):** 不可变文件名发布流程 + DoH 指针 + CDN 源对齐 + config v2 格式。
- **Phase 5:** 真机 smoke + 各平台发版。

**部署序:** Phase 1(节点反代铺开,向后兼容)→ Phase 4(分发就绪)→ Phase 2/3(客户端发版)。节点侧先行,客户端发版时入口已就绪。

---

## 9. 风险与开放问题

- **R1 节点见明文密码/支付/token**:**决策已定 = 所有端点都走中继(含 `/api/user/password`、支付类)**,节点(含第三方 VPS)会看到明文密码与支付参数。判定依据同 D2(节点本就是中间人且已持凭证)。**补偿控制(必须做)**:节点中继审计日志(§5.1)+ Center per-account 限流(§5.1 评审 #2)。残留风险:被攻破/恶意的第三方 VPS 节点可批量截获密码 → 运维侧需对节点准入与镜像完整性把关(kaitu-node-ops)。
- **R2 内层 SNI 用真实 `k2.52j.me`(已定:不做地址抽象层)**:node→Center 在墙外、节点本就持凭证,留痕风险低;且内层 SNI 已被外层 ECH 加密,GFW 看不到。**前提(2026-06-27 用户确认):Center 真实地址 `k2.52j.me` 已按"会被封锁"设计,永久不再更换** —— 抗封锁靠节点中继,不靠换域名。故**放弃**早先设想的"代号 / 内部路由标签(`cp.k2.internal`)映射"间接层(它本是为"地址会变"准备的,该前提不成立 → 纯属多余)。客户端 `CONTROL_PLANE_HOST` 与节点 `control_plane_routes` 永久硬写 `k2.52j.me`,一次性配置,不存在持续耦合。**Option D「把 cpHost 做成下发字段」一并取消,不做。**
- **R3 DoH 指针**:多解析器(阿里 223.5.5.5 + 腾讯 119.29.29.29 等),native 侧实现,定义超时/回退(§5.4)。需确认各 DoH 端点与记录类型(TXT/HTTPS)。
- **R4 QUIC 不覆盖控制面中继**:控制面固定走 TCP+TLS+ECH。可接受(JSON 小、延迟不敏感)。
- **R5 appext 双进程**:`RelayFetch` 必须在 App 进程(非 NE)运行;确认 gomobile 绑定与 NE 进程隔离无冲突。
- **R6 cloud-api 改造面**:`request`/`_handle401`/`_doRefresh` 三处都调 `resolveEntry`,统一切到 `resolveAndFetch`;保 401 刷新原子性(§5.3 评审 #5 硬约束)。
- **R7 冷启动可达性**:DNS 指针只解新鲜度不解可达性;已加内置稳定锚点兜底(§5.4 评审 #6)。残留:锚点 IP 也被封 + DoH/CDN 全灭 = 全新安装仍可能起不来(已是物理下限,blast radius 已最小化)。
- **R8 锚点运维**:内置锚点 IP 须长期稳定,换 IP 需发版(kaitu-node-ops 维护清单)。
- **R9【backlog,时机待定】老 antiblock"换域名"逻辑退役**:基于 R2 前提(Center 地址永久不变,抗封锁靠节点中继),`antiblock.ts` 的"从 CDN 取备用直连域名"(`fetchEntryFromCDN`/`entries[]`/JSONP 解密)是"换域名躲猫猫"旧策略,直连永远只试 `k2.52j.me`,该层可瘦身为常量。**但 CDN 通道本身保留**——它将承担冷启动向新装用户分发"节点种子清单 `{ip,pin,ech}`"(Phase 4)。属独立的老代码清理,不并入 Phase 1–3 分支。

---

## 10. 受影响文件清单(预估)

| 层 | 文件 |
|----|------|
| k2 server | `k2/server/server.go`(handleK2V5 seam)、`k2/config/config.go`(ControlPlaneRoutes)、新增 `reverseProxyHTTP` |
| k2 relay | 新增 `k2/relay/`(或复用 wire)`RelayFetch`;`k2/daemon/`(/api/relay 端点);`k2/appext/`(导出) |
| 移动桥 | `mobile/plugins/` K2Plugin(Swift/Kotlin);`webapp/src/services/capacitor-k2.ts` |
| 桌面桥 | `webapp/src/services/tauri-k2.ts`;`desktop/src-tauri/`(IPC 命令) |
| webapp | `webapp/src/services/antiblock.ts`(池+解析)、新增入口池 store、`webapp/src/services/cloud-api.ts`(resolveAndFetch 接线) |
| 发布基建 | config 发布流程(不可变文件名)、DNS 指针记录、`kaitu-io/ui-theme` 仓库 dist |
| 节点配置 | 各节点 `control_plane_routes` 下发(kaitu-node-ops) |
```
