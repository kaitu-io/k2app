# k2v5 认证重构：隧道凭据 + 节点签发 cookie

日期：2026-07-30
状态：设计已确认，待实施
影响层：`k2/wire/`、`k2/server/`、`k2/config/`、`k2/engine/`、`k2/daemon/`、`k2/gateway/`、`api/`、`webapp/`、`docker/`
k2 侧分支基底：`fix/quic-device-auth`（master + 5 commits，无分叉）

---

## 1. 问题

### 1.1 k2v5 的设备认证事实上不存在

三条独立的证据：

**QUIC 路径**：`wire/quic.go:602` 的 `if c.cfg.Mode != ""` 意味着只有 k2r 网关客户端会发 metadata stream。普通客户端（桌面 App、iOS、Android）**从不发送任何凭据**，因此服务端的 `handleMetadataStream` 对它们从不执行。

**TCP-WS 路径**：`sendMetadataBestEffort` 会发 metadata，但服务端 `wire/tcpws.go:733` 收下后只做 `dev.set(meta.UDID)`，**不校验**。WebSocket upgrade（`/k2v5/tunnel`）不带任何凭据也不检查。

**HTTP 票据路径**：`wire/auth.go` 的 `TicketStore` + `AuthHandler` + `POST /k2v5/auth` 完整实现且已挂载（`server/server.go:148`），但**没有任何客户端调用过它**。它是一条从未通电的线路。

后果：拿到一份 `k2v5://` URL（其中 `ech`、`pin` 都是明文参数）即可无限期免费使用任意共享节点；`udid` 由客户端自报，流量归属可任意伪造；`docs/superpowers/specs/2026-07-22-per-user-traffic-accounting-design.md` §8 记录的自动配额处置因此没有可信依据，其 §170 明写自动处置的硬前置是"metadata UDID 与 ticket 交叉验证"。

分支 `fix/quic-device-auth` 已经修复了准入结构（见 §3.1），但它把每条新连接都变成一次同步的 Center 校验，且强制开关默认关闭——所以今天线上仍然是完全不设防的。

### 1.2 认证一旦打开，凭据活不过 24 小时

`k2v5://UDID:TOKEN@…` 里的 TOKEN 是用户的 **web access token**，寿命 86400 秒（viper 键 `jwt.access_token_expiry`；`api/config.yml` 不在 git 里，值由 `api/logic_config.go` 读取）。今天它过期完全无害（无人校验）；强制认证打开后它是承重的。

而有 **6 条路径**在 token 过期后**无法自愈**：

| # | 路径 | 代码定位 | 为什么恢复不了 |
|---|---|---|---|
| P1 | **k2r 企业路由器** | `api/api_gateway_credential.go:66-90`；`k2/config/subscription.go:26-33,158,302-309` | `generateTokens` 同时签了 access + refresh，但只把 access 塞进 URL，**refresh 被直接丢弃**。`Subscription.creds` 是构造后不可变字段，订阅刷新只换节点列表不换 token。绑定约 24h 后 `/api/subs` 稳定 401，隧道列表永久停在绑定当天的磁盘缓存 |
| P2 | **桌面 daemon 7 天自动重连** | `k2/daemon/daemon.go:620-661` | 重放磁盘上的 `persistedState.Config`，token 冻结在上次连接时刻；webapp 不在链路上，无刷新机会 |
| P3 | **iOS on-demand / alwaysOn 重启** | `mobile/…/K2Plugin.swift:276,286-288`；`PacketTunnelProvider.swift:178-190` | 系统无参数拉起 NE，configJSON 从 App Group 读取，token 冻结；NE 内无 Center 通信能力 |
| P4 | **Android always-on VPN 重启** | `mobile/android/…/K2VpnService.kt:133-142` | `SharedPreferences("k2vpn")` 重放 configJSON，同 P3 |
| P5 | **桌面 k2subs 订阅静默降级** | `k2/subscription/resolve.go:74-84`；`k2/config/subscription.go:540-564` | fetch 401 → 只要有磁盘缓存就 `slog.Warn` 后继续用。缓存加载**无 TTL、无年龄检查**，三天前的 `subs-*.json` 照用，带着过期 token 去连节点 |
| P6 | **App 冷启动竞态** | `webapp/src/stores/connection.store.ts:451-556`；`hooks/useUser.ts:92-99`；`CloudTunnelList.tsx:158` | 唯一的 token 刷新触发器是后台 revalidate 撞到 401（`cloud-api.ts:253-347`）。它与用户点连接之间**没有任何 gate**；`connect()` 不检查 auth 状态、不等刷新、不主动刷新 |

附加伤：engine 的 401 与 webapp cloudApi 的 401 是**两套互不相通的机制**。engine 状态经 `_k2.run`/SSE 上报，从不进入 `cloudApi._handle401`。节点拒绝 → `status-transform.ts:33-38` 判定 `retrying=false` → `vpn-machine.store.ts` 落到 `idle` 终态 → 弹一句 `errors.vpn.authFailed`。用户手动重试仍用同一个过期 token，死循环。`webapp/src/services/vpn-types.ts:50-52` 定义了 `isAuthError(code)`，**全仓零消费者**。

### 1.3 为什么这两件事必须一起做

单做 §1.1，强制认证永远开不了：开的那一天，P1 的所有路由器和 P3/P4 的所有 always-on 用户会成片掉线，且无法自愈。

单做 §1.2，白嫖和归属伪造继续存在。

而 §1.2 的修法**不能是给 6 条路径各打一个补丁**——那是同型缺陷补第 N 个洞，第 7 条路径出现时会静默重现。结构性修法是让凭据本身不在 24 小时内死，"新鲜度"就不再是任何一层需要执行的规则（§4）。

---

## 2. 目标与非目标

### 目标

1. 任何 k2v5 连接（QUIC 与 TCP-WS）都必须出示可验证的设备凭据才能产生 egress。
2. 服务端记录的 `udid` 来自服务端的判定，而非客户端自报——使自动配额处置有可信依据。
3. QUIC ↔ TCP-WS 切换、端口跳变、网络切换、节点重启后重连，**不产生额外的 Center 往返、不引入额外延迟**。
4. 凭据在所有分发路径上都能自我续期，离线任意时长后重新上线都能自愈。
5. 用户退订/被封/设备被删后，**最坏 15 分钟内**失去访问，且该窗口不可被任何续期机制延长。
6. Center 不可达时，已授权用户不受影响；Center 明确拒绝时，用户立即失去访问。这两者必须可区分。
7. `mode=gateway`（企业路由器）成为服务端判定的属性，而非客户端自报。

### 非目标（本次明确不做）

- **会话级无缝切换**：不做 QUIC 与 TCP-WS 同时在线、在途代理连接跨传输迁移。当前 L4 模型下每条代理流绑定在具体 stream 上，迁移需要重放与缓冲，量级与本次不相称。本设计做到的是**身份无缝**——切换不重新认证、不回 Center、归属连续。
- **并发使用限制**：同一 udid 可在无限多节点同时使用（`/slave/device-check-auth` 无速率限制、无并发限制，设备数只在登录时卡）。本次只做**可观测**（cookie 携带 `session_id`，随流量上报），拿到真实分布后再决定阈值。先定规则容易误伤——快速切节点会制造正常的短暂重叠。
- **跨节点通用凭据**：cookie 是节点本地的。换节点必然回一次 Center。已知局限见 §10.2。
- **替换传输层伪装**：Tessera（`k2/docs/tessera/`）是独立工作。它明写隧道层"完全复用 k2v5 的 metadata 设备认证 + admissionPolicy"，因此本设计的产出会被它直接继承，两者不冲突、不需要协调发布。

---

## 3. 架构总览

### 3.1 保留分支已有的准入结构

`fix/quic-device-auth` 的核心贡献不是"加了个检查"，而是**让门成为 egress 的唯一供给者**：

- `admitTCP` 是代理流通往 `AcceptTCP` 的唯一路径。
- `admitUDP` 是服务端 `UDPHandler` 的唯一读取者。

它的注释记录了为什么：门最初写成"在每个 egress 分发点重复检查"，结果 TCP-WS 代理流、QUIC datagram（主 UDP 路径）、QUIC overflow stream 三条路各自漏掉了一次，每次都是在后续 review 里才发现，而这个门**失败时是静默放行的**。`TestAdmissionGate_EgressSinksAreGated` 把这个性质钉住。

本设计**完整保留**这个结构，只替换它下面"每条新连接同步验一次"的部分。

同时保留的还有：两个传输共用同一个 `enforce` 开关（只覆盖 QUIC 会让攻击者强制走 TCP-WS 绕过）、`NewRemoteValidator` 补上 Basic auth（缺它导致 `/slave/device-check-auth` 在生产恒 401、命中数为 0）、相机伪装中继不再信任客户端自选的 SNI。

### 3.2 三层凭据模型

| 层 | 凭据 | 签发方 | 寿命 | 验证方 | 作用域 |
|---|---|---|---|---|---|
| 用户会话 | access token | Center | 24h | Center | `/api/*` |
| | refresh token | Center | 30d | Center | `/api/auth/refresh` |
| **隧道凭据** | **tunnel token** | **Center** | **90d** | **Center** | **`/slave/device-check-auth` + `/api/subs`（见下）** |
| **连接凭据** | **cookie** | **节点** | **15min** | **节点（本地 HMAC）** | **仅该节点的 k2v5 连接** |

每一层泄漏的爆炸半径都被下一层限制住：cookie 泄漏 = 一个节点 15 分钟；tunnel token 泄漏 = 隧道访问 90 天（但拿不到账户，改不了密码、退不了款、看不到订单）；access token 泄漏才是账户级问题，而它不再出现在任何隧道 URL 里。

这一点本身就是对现状的改进：今天每一条 k2v5 连接都在线上重传用户的**账户级** access token。

**tunnel token 的作用域是两个端点，不是一个。** `k2subs://udid:token@host/api/subs` 里的 token 就是 Basic auth 的 password——订阅刷新本身要用它。所以 `/api/subs` 必须接受 tunnel token，否则 P1 的路由器在换用新凭据后**第一次刷新就死**，正好把要修的问题原样复现。

但它**不进通用的 `handleJWTAuth`**：那会让一个 90 天的凭据获得整个 `/api/*` 面（改密码、退款、看订单）。两个端点是白名单，不是默认。`/api/subs` 因此是双 token 认证——access 与 tunnel 都收，按 `claims.Type` 分派。

### 3.3 数据流

```
                          ┌─────────── Phase 0 ───────────┐
  登录/绑定 ──► Center 签 tunnel token（90d）
                    │
                    ├─► /api/subs         ─► k2subs:// 与 k2v5:// URL 内嵌 tunnel token
                    ├─► /api/tunnels      ─► webapp 拼接时使用 tunnel token
                    └─► gateway-credential ─► k2r 凭据内嵌 tunnel token
                                              （每次 subs 成功顺带滚动续期）

                          ┌─────────── Phase 1+2 ─────────┐
  客户端 connect
    │
    ├─ 有 cookie？──是──► metadata{udid, token, cookie, mode, version}
    │                        └─► 节点本地验签（微秒级）──► 准入，零 Center
    │
    └─ 无 cookie / 已过期 ──► metadata{udid, token, mode, version}
                                └─► validator 链 ──► Center /slave/device-check-auth
                                      └─► 通过：签发 cookie，随响应回给客户端
```

---

## 4. Phase 0 — 隧道专用凭据

### 4.1 Center 侧

**新增 token 类型**（`api/logic_auth.go:25-26` 已有 `TokenTypeAccess`/`TokenTypeRefresh` 的先例）：

```go
TokenTypeTunnel = "tunnel"
```

寿命由新配置项控制：viper 键 `jwt.tunnel_token_expiry`，**代码内默认 `7776000`（90 天）**。注意 `api/config.yml` 不在 git 里，所以代码默认值是唯一可靠的事实源；线上 config.yml 里的显式配置属于部署清单项，不是代码变更。

**新增 Device 列**：

```go
TunnelIssueAt int64 `gorm:"column:tunnel_issue_at;default:0"`
```

**这一列是必需的，不能复用 `TokenIssueAt`**：`api_refresh_token`（`api/api_auth.go:439-478`）在每次 web token 刷新时会把新的 `TokenIssueAt` 写回 Device 行。若 tunnel token 也绑 `TokenIssueAt`，则用户每 24 小时刷新一次 web token 就会把自己的隧道凭据作废——正好毁掉本 Phase 的目的。两个时钟必须独立。

**校验点改造**（`api/slave_api_device_auth.go`）：

1. `validateToken(c, req.Token, TokenTypeTunnel)`，并将 `TokenIssueAt` 比对改为 `TunnelIssueAt` 比对（在 `validateToken` 内按 token type 分派，或抽出 `validateTunnelToken`）。
2. **新增封禁检查**：`isUserBlocked(&user)` → `403`。当前 `/slave/device-check-auth` 走的是 `SlaveAuthRequired()`（节点 Basic auth）而非 `AuthRequired()`，**因此封禁检查从未在这条路径上执行过**（`isUserBlocked` 的调用点集中在登录、刷新、OTT 与 `AuthRequired` 中间件，不含本路径）。今天靠 access token 24h 过期兜底；换成 90 天凭据后，不加这个检查等于封号在隧道上 90 天不生效。
3. **新增 mode 校验**：见 §5.4。
4. 响应体扩展：见 §4.5。

**吊销手段**（三条，都已存在或成本极低）：

- 删除 Device 行 → `validateToken` 查不到设备 → 401。`checkDeviceLimitOrKick`（超设备数踢最旧设备）与用户主动删设备都走这条。
- 递增 `Device.TunnelIssueAt` → 旧 tunnel token 立即失效。用于"登出所有设备"与管理员强制下线。
- 封禁用户 → 上面第 2 条。

### 4.2 分发路径改造

| 路径 | 现状 | 改造 |
|---|---|---|
| `/api/subs`（`api/api_subs.go:129-141`） | `injectSubsCreds` 回填**调用方自己的** access token | 改为回填该设备的 tunnel token |
| `/api/v20260717/tunnels` | 返回裸 `serverUrl`，webapp 用 `storage[k2.auth.token]`（access token）拼接 | 响应新增 `tunnelToken` 字段；webapp `authService.getCredentials()` 改读它 |
| `POST /api/user/gateway-credential`（`api/api_gateway_credential.go:66-90`） | 只用 `AccessToken`，refresh 丢弃 | 改用 tunnel token |

### 4.3 续期

**滚动续期，由 Center 单向推动，客户端只负责持久化。**

**`/api/subs`（条件续期）**：请求本身就是用 tunnel token 认证的，所以服务端能测出它的剩余寿命。不足 50%（即已过 45 天）则签发新的并内嵌进响应的 tunnel URL。客户端持久化新值。

**`/api/tunnels`（无条件重签）**：这个端点用 access token 认证，**请求根本不携带 tunnel token**，服务端无从测量剩余寿命——所以 50% 规则在这里不可实现。改为每次成功响应都按当前锚点重签一个新的写进 `tunnelToken` 字段。这是 50% 规则的安全退化形：`TunnelIssueAt` 锚点不变，因此吊销语义完全不受影响，只是多签了几次。

**客户端必须先采纳、再注入。** `k2/config/subscription.go:302-309` 在每次 `Fetch` 后会用客户端自己的 `s.creds` **重写响应里所有 tunnel URL 的 userinfo**。若不先采纳 Center 回填的新凭据就走这一步，续期会被客户端原地抹掉——刷新看起来成功，凭据却一天没延长。这才是 `creds` 必须改为可变的真正机理，顺序颠倒等于整个 Phase 0 白做。

`Device.TunnelIssueAt` 在续期时**不变**——续期只延长 `exp`，不重置吊销锚点。这样"登出所有设备"这类操作仍然能一次性作废所有已续期的凭据。

由此得到的性质：**只要设备在 90 天内联网使用过一次，凭据就永不过期**。90 天不用的设备回来时需要用户重新登录一次——这是可接受且符合直觉的。

**客户端侧的持久化必须打通到六条路径的存储位置**：

- P1 k2r：`Subscription.creds` 从不可变字段改为受 `mu` 保护的可变字段，`Fetch` 成功且响应 URL 携带新凭据时更新并落盘（`gateway/api.go:138-150 applyCredential` 已有落盘路径可复用）。
- P2 桌面：`persistedState.Config` 在订阅刷新后回写。
- P3 iOS：**读取顺序是 options → `providerConfiguration` → App Group**（`PacketTunnelProvider.swift:178-195`），系统无参数拉起时 `providerConfiguration` 先命中。因此**只同步 App Group 不生效**——必须同时用 `saveToPreferences` 回写 `providerConfiguration`。
- P4 Android：同步进 `SharedPreferences("k2vpn")`。
- P3/P4 共同：这是移动端唯一的额外工作量。原生同步失败走 graceful catch，**不 bump `minNativeVersion`**——旧原生包缺 `updateConfig` 是无害的（存量凭据仍有 ≥45 天寿命，下次 App 更新自然收敛），为此强制升级不划算。这是对 `mobile/CLAUDE.md` 的 bump 规则的有意偏离，在此记录。
- P5：见 §4.4。
- P6：见 §4.4。

### 4.4 失效路径的收敛检查

Phase 0 完成后逐条复核（这是 Phase 0 的验收标准，不是事后总结）：

| # | 是否自愈 | 依据 |
|---|---|---|
| P1 k2r | ✅ | 凭据 90 天有效；每 30 分钟一次的 `/api/subs` 刷新会在第 45 天带回新凭据并落盘 |
| P2 桌面重连 | ✅ | 7 天窗口 ≪ 90 天，重放的凭据必然有效 |
| P3 iOS alwaysOn | ✅ | 同上，前提是 §4.3 的 App Group 同步已实现 |
| P4 Android alwaysOn | ✅ | 同上 |
| P5 subs 静默降级 | ✅（且**保留降级行为**） | 凭据不再 24h 过期，降级用旧缓存不再等于用死凭据。降级本身是正确的可用性设计（Center 不可达时仍能连），保留。但**必须补一条 `DIAG: subs-stale-cache` WARN 并带上缓存年龄**——今天它只报 fetch 失败，不报"我正在用多久以前的数据" |
| P6 冷启动竞态 | ✅ | 竞态仍在（连接不等 revalidate），但赛跑的两边都拿着 90 天凭据，谁先谁后都无所谓。**不为此新增 gate**——加同步等待会让冷启动连接变慢，而收益为零 |

### 4.5 Center 响应体扩展

`SlaveDeviceCheckAuthResult` 已有 `UserID` / `UDID` / `ServiceExpiredAt`。**`ServiceExpiredAt` 已经是正确的：共享节点取 `user.ExpiredAt`，专属节点取 `PrivateNodeSubscription.ExpiresAt`（两个独立时钟都对，`api/slave_api_device_auth.go:105-110`）。节点侧一直只解 `code` 把它丢了**（`wire/auth_remote.go` 只 decode `{code}`）。

本次不新增字段，只让节点真正读取它。`UserID` 同样纳入 cookie（用于 §6.6 的会话可观测与 §8 的归属交叉验证）。

---

## 5. Phase 1 — 统一 metadata 认证

### 5.1 线路格式

`MetadataFrame`（`wire/stream.go:239-334`）是 kv 编码，`ReadMetadataFrame` 的 `switch` 无 `default` 分支，未知 key 静默跳过。因此**新增字段天然前后兼容**，不需要版本协商。

请求方向新增一个 key：

| key | 必需 | 说明 |
|---|---|---|
| `cookie` | 否 | 上次该节点签发的 cookie。为空/缺失表示冷认证 |

响应方向：当前 `WriteStreamResponse` 只写 `[0x00]`（OK）或 `[0x01]`/`[0x02]`（错误）。`0x02` 已被 coded error 占用，**不能新开状态码**。改为：

```
[0x00][MetadataFrame kv]      成功，kv 中可含 cookie=<new cookie>
```

老客户端读完 1 字节 `0x00` 即 `stream.Close()`，多余字节被丢弃，不产生错误——QUIC 上 `Close()` 只发 FIN，剩余数据留在接收缓冲直至流被回收；smux 上 `Close()` 直接丢弃。已验证两侧都安全。

### 5.2 客户端

**合并两份手抄实现。** 现在 `QUICClient.sendMetadata`（`wire/quic.go:635-676`）与 `TCPWSClient.sendMetadataBestEffort`（`wire/tcpws.go:258-283`）是两份独立编排，行为已经不一致（前者返回错误、后者返回 void 并丢弃 `ok`/`errCode`）。抽出单一实现，两个传输只提供各自的 stream 打开方式与是否写 H3 frame type。

**错误不再吞掉。** 当前两边都是 best-effort：认证被拒时 `connect()` 照常返回，客户端只在后续 `DialTCP` 失败时才间接察觉。改为：

- 传输层错误（开流失败、写失败、读超时）→ 保持 best-effort，不失败 `connect()`。这类错误说明连接本身有问题，会由既有机制处理。
- **服务端明确拒绝（`0x01`/`0x02` 响应）→ 失败 `connect()`，并携带结构化错误码**。

  注意这在 permissive 期间不会误伤：permissive 模式下服务端**永远回 OK**（分支的 `handleMetadataStream` 以 `authed := !s.adm.enforce` 起手），因此 Phase 1/2 铺开期间没有任何客户端会因为凭据问题连不上。拒绝只在 enforce 打开后出现——那时它正是我们想要的行为。

**结构化错误码替代字符串匹配。** `engine/error.go:133-135` 现在靠 `strings.Contains(msg, "stream rejected")` 判定 401，这在认证被拒时根本不会命中（错误已被吞掉），命中时也脆弱。服务端改为用 `WriteStreamResponse` 的 coded error 形式（`0x02` + 数字码），码值直接复用 `engine.EngineError` 的区间约定：

| 码 | 语义 | 客户端呈现 |
|---|---|---|
| 401 | 凭据无效/设备未授权 | 需重新登录 |
| 402 | 订阅已过期 | 引导续费 |
| 403 | 设备类别不符（见 §5.4）/ 用户被封禁 | 联系客服 |

这三个码要落在 `engine/error.go` 的 `CategoryClient`，这样 `ClearWireError` 不会清除它们、`status-transform.ts:33-38` 会正确判定 `retrying=false`——既有机制无需改动，只是终于拿到了正确的输入。

**但不能按码分类。** `StreamError{403}` 已经被代理层用来表示"目标地址被 block"（`server/handler.go:243`），而 `engine/contract_test.go:124 TestContract_StreamError_IsTarget` 把裸 `StreamError`（含 403）钉死为 `CategoryTarget`。若直接按码判分类，**"用户访问了一个被封的 IP"会被误报成"你的账号认证失败"**——一个每天都会发生的正常情形被翻译成让用户去重新登录。

正确做法是让认证拒绝**在类型上可区分**，而不是靠码值猜：`sendMetadata` 仍返回 `*StreamError{Code}`（Phase 2 靠 `errors.As` 取码，不受影响），但 `connect()` 传播时用一个哨兵错误 `ErrMetadataRejected` 包裹，engine 以 `errors.Is` 判定这是认证拒绝、再取码分 401/402/403。这与 `k2/CLAUDE.md` 的错误归属规则一致——消费方查询一个明确的标记，而不是枚举码值。

**race 出口会把这个结构压平。** `wire/race.go:180` 用 `%v` 把三个候选的错误拼成字符串，结构化的码到不了 `ClassifyError`。而 enforce 打开后，认证拒绝**恰恰只会经 race 路径**到达 `engine.Start`——不修这一处，上面整套结构化错误码在真实路径上完全落空，测试却会全绿（各层单测都过，只是它们之间的那一段把信息扔了）。修法：race 出口改用 `errors.Join` 保留错误链，并配一条端到端的保真测试。

（顺带：该函数里的局部变量 `var errors [3]error` 遮蔽了 stdlib 的 `errors` 包，改这里时必须先更名。）

**cookie 存储。** 新增 `wire.CookieJar`，按服务端身份（pin 指纹，而非 host:port——同一节点可能多 IP/多端口）索引。**仅存内存，不落盘**：15 分钟的凭据不值得增加磁盘暴露面，进程重启后一次冷认证即可恢复。

Jar 必须位于**单个 dialer 之上**：`RaceTransport` 为每个候选创建独立的 `QUICClient`/`TCPWSClient`（`wire/race.go:202,214,246`），三者必须共享同一个 jar，否则三路竞速会产生三次冷认证。由 engine 按 outbound 创建 jar 并注入构造函数——`NewQUICClient(cfg, store *TicketStore)` 这个参数位在 master 上本来就存在（`quic_p2p.go`、`k2p/home.go`、`k2p/away.go` 都在传），分支把它删了，本次以 `*CookieJar` 复用该位置。

### 5.3 服务端

**冷认证路径**：validator 链不变（`users_file` → `remote_url` → accept-all fallback），但：

- `NewRemoteValidator` 从"返回 bool"改为返回结构化结果 `{ok, code, userID, serviceExpiredAt}`，使 §4.5 的信息能进入 cookie，且使 §6.4 能区分"被拒绝"与"够不着"。
- `CachingValidator` 加 **singleflight**。当前实现（`wire/auth_cache.go:35-57`）是"miss → 调 inner → 存"，并发 miss 会各打一次 Center。三路竞速的 300ms/800ms 错开只是让这件事**大部分时候**不发生，不是保证。
- `CachingValidator` TTL 从 5 分钟改为 15 分钟，与 cookie 寿命对齐（§6.3）。

  **归属**（两处别重复改）：Go 代码里的默认值（`config/config.go` 的 `SetDefaults`，当前 `5 * time.Minute`）归 **Phase 1**——它本来就在动这个 validator；`docker/sidecar/main.go` 模板里硬编码的 `cache_ttl: 5m` 归 **Phase 3**，它属于部署配置。

**验证结论是四态，不是三态。** 这一点漏掉会在链的这一层就把 §6.4 压掉：`NewUsersFileValidator` 以 udid 为键，文件缺失或查无此人时返回 false（`wire/auth_users.go:63-85`），而**共享节点的 users 文件本来就是空的**。若把这个 false 当成"明确拒绝"，它会抢在 remote validator 之前给出 401，Center 的真实回答——包括"不可达"——永远到不了调用方。

| `OK` | `Code` | 含义 | 链的行为 |
|---|---|---|---|
| `true` | `0` | 通过 | 立即返回 |
| `false` | `0` | **无意见**（本 validator 不认识这个 udid） | 跳过，继续 |
| `false` | `401`/`402`/`403` | 明确拒绝（只有 Center 能给出） | 立即返回 |
| `false` | `-1` | 无法判定 | 记下，继续 |

收尾：见过明确拒绝则返回它；否则见过 `-1` 则返回 `-1`；否则（全体无意见）返回 `{OK: false, Code: 401}`。

**Center 还会返回 401/402/403 之外的码**（422 `ErrorInvalidArgument`、500 `ErrorSystemError` 等）。这些一律映射为 `-1`（无法判定）——它们表示 Center 自己出了问题或收到了畸形请求，**不是对这个用户的授权裁决**，按拒绝处理会把一次服务端故障变成用户被踢。只有明确表达"这个用户不该被服务"的三个码才是拒绝。

`CachingValidator` 只缓存 `OK == true`。`-1` **绝不缓存**——缓存"够不着"会把一次瞬时抖动固化成一个 TTL 长的故障。`401`/`402`/`403` 也不缓存，否则刚续费的用户会被锁在 TTL 里（这是分支现有实现已经做对的性质，保留）。

下游消费方（尤其是 §6.5 的周期重校）**必须 switch on `Code`，不能写成 `if !verdict.OK`**。后者会把 `-1` 当成拒绝，于是一次 Center 抖动就把全车队连接在 15 分钟后断光——正是 §6.4 要防的那个故障。

**cookie 路径**：metadata 携带有效 cookie 时，直接本地验签放行，**完全不进入 validator 链**。

**udid 取值收口**：服务端记录的 `udid` 一律取自服务端判定结果（cookie 内的，或冷认证时 Center 确认的），**永不取 `meta.UDID`**。当前 `dev.set(meta.UDID)` 在 QUIC（`quic.go:1317`）与 TCP-WS（`tcpws.go:~830`）两处都是无条件执行的自报值。

需要注意：冷认证时 Center 本来就会校验 `device.UDID != udid` → 401（`api/slave_api_device_auth.go:56-60`），所以通过校验的 udid 已经可信。真正的漏洞在 permissive 模式下——未通过校验的连接**仍然会 `dev.set`**。改为：未通过校验的连接归入空 udid 桶（admin 侧已有"未识别"列，`per-user-traffic-accounting-design.md:172`），不冒充任何设备。

### 5.4 mode 收口

**现状是个真空洞。** 客户端在 metadata 里发的 `mode`（`gateway` = 企业路由器）经 `wire/auth_remote.go:17-21` 序列化后发给 Center，但 `SlaveDeviceCheckAuthRequest`（`api/slave_api_device_auth.go:13-17`）**没有 mode 字段**，`ShouldBindJSON` 直接丢弃。任何普通用户自报 `mode=gateway` 都会通过，Center 侧的 `Device.IsGateway` 从未被用来把关。

修法照抄 `/api/subs` 已有的设备类别交叉校验（`api/api_subs.go:199-212`），含其兼容规则：

```
mode == "gateway"  且 Device.IsGateway == false  →  403（设备类别不符）
mode == ""         →  放行（普通客户端，也是老客户端的默认）
```

只在危险方向（App 设备自称路由器）拦截。反方向（路由器自称普通客户端）是降级，无害。存量 k2r 恒发 `mode=gateway`（`gateway/config.go:104`），其 Device 行 `IsGateway=true`（`api_gateway_credential.go:73`），不受影响。

校验通过的 mode 签进 cookie，此后全程使用 cookie 内的值。注意 `connContext.mode` 当前是**只写字段**（`quic.go` 里只有 `Store`，没有任何 `Load` 消费者）——Phase 1 把它 gate 在 `verdict.OK` 之后，但它的第一个真正消费者是 Phase 2 签发 cookie 的那一步。

**归属**：`SlaveDeviceCheckAuthRequest.Mode` 这个字段在接口契约里划给 Phase 0（它要动这个 struct），而**校验逻辑**归 Phase 1。实施时按"字段已存在则复用，不存在则按契约逐字补上"处理，两个 Phase 都不会冲突。补字段会牵动 8 个既有测试的调用点（需补一个 `""` 参数）。

---

## 6. Phase 2 — 节点签发 cookie

### 6.1 它到底买到了什么

必须诚实回答：`CachingValidator`（15 分钟成功缓存）已经能让窗口内的重连免于 Center 往返。cookie 相比它多买到三件事，都是结构性的：

1. **节点重启不再制造惊群。** 服务端缓存随进程死。一次部署 = 全部在线设备同时重连 = 一波 Center 请求。无状态 cookie 在节点重启后照样本地验签。
2. **传输切换不会踩到缓存 miss。** 缓存 miss 会在切换中途插入一次同步 Center 调用（5 秒超时）——这正是"不无缝"的症状本身。cookie 让它结构上不可能发生。
3. **与 token 轮换解耦。** 缓存键含 token（`sha256(udid‖token‖mode)`）。token 一换全车队缓存全 miss。cookie 不受影响。

一句话概括：**cookie 就是把缓存从服务端内存搬到客户端，并让它变成无状态且签名的。** `CachingValidator` 保留，退为冷认证的第二道去重。

### 6.2 内容与签名

```
payload = protobuf-free 紧凑编码 {
    ver       uint8      协议版本，当前 1
    key_id    uint8      签名密钥编号
    udid      string     服务端判定的设备标识
    user_id   uint64     Center 返回的用户 ID
    mode      uint8      0 = 普通，1 = gateway（已校验）
    session_id [16]byte  随机，冷认证时生成，用于 §6.6
    auth_at   int64      上次 Center 校验的 Unix 时刻
    svc_exp   int64      Center 返回的 ServiceExpiredAt
}
cookie = base64url( payload || HMAC-SHA256(cookieKey[key_id], payload)[:16] )
```

截断到 128 bit 的 MAC 对这个用途足够，且让 cookie 保持在 metadata value 的 4096 字节限制内绰绰有余。

**cookie 是"已签名"而非"已加密"。** payload 对持有者可读——但持有者就是设备自己，`udid` / `user_id` / `svc_exp` 全是它已经知道的信息，因此不构成泄漏。**不要在将来假设它有机密性**（例如往里塞节点内部标识或策略参数）。

**cookie 永不进日志。** 桌面端有 S3 日志上传通道（见 `desktop/CLAUDE.md`），任何被记录的 cookie 都会离开设备并进入可被检索的存储。所有涉及 cookie 的日志一律只记 `key_id`、`session_id` 前 8 字节与验签结果，**绝不记 cookie 本体或 MAC**。同一约束适用于 `token`——`NodeSecret` 已有 `NEVER log` 的先例（`server/usage_reporter.go:87`），本次按同样标准处理。

### 6.3 生命周期：不做本地续期

```
exp = min(auth_at + 15min, svc_exp)
```

**cookie 永远不在本地被延长。** 节点在响应里回新 cookie 的唯一时机是**刚刚完成了一次 Center 冷认证**——那时 `auth_at` 才前移。

由此得到的性质：无论客户端重连多少次、无论 cookie 被出示多少次，**距离上次 Center 背书超过 15 分钟就必然回到 Center**。滞后窗口是硬的，不可能被续期机制无限延长。这是对"吊销时间关键是是否会刷新"的直接回答。

客户端永远同时携带 `cookie` 和 `token`。cookie 过期时服务端静默回落到冷认证，客户端无感知、无额外往返（凭据本来就在同一帧里）。

`svc_exp` 的额外价值：订阅到期这个最常见的情形**完全不需要 Center**。一个 3 分钟后订阅到期的用户拿到的 cookie 也在 3 分钟后过期，精确到秒。

**users_file 分支**（自建/独立节点，无 Center）：`svc_exp` 置为 0 表示"无订阅时钟"，`exp = auth_at + 15min`，行为与共享节点一致，只是 15 分钟后重新查 users 文件（本地读，零成本）。

### 6.4 Center 不可达 ≠ 认证失败

**这是本设计里最容易造成全车队故障的一处，必须显式规定。**

当前 `NewRemoteValidator`（`wire/auth_remote.go:29-77`）把**所有**失败——网络错误、超时、JSON 解码失败、非零 code——统统 `return false`。照这个语义加上 §6.5 的周期重校，一次 Center 抖动超过 15 分钟就会让全车队连接在 15 分钟后集体断开。

规定如下：

| Center 的回答 | 判定 | 行为 |
|---|---|---|
| `code == 0` | 通过 | 签发 cookie，`auth_at = now` |
| `code == 401 / 402 / 403` | **明确拒绝** | 冷认证：拒绝连接并回结构化错误码。周期重校：断开连接 |
| 网络错误 / 超时 / 5xx / 解码失败 | **无法判定** | 冷认证：按 `enforce` 决定（permissive 放行、enforce 拒绝，与今天一致）。**周期重校：不断连，把注册表里该会话的 `authSession.authAt` 前移一个窗口**，并打 `DIAG: auth-center-unreachable` WARN |
| `nodeSecret == ""` / `nodeIPv4` 解析不出 | 节点配置问题 | fail closed（分支已有行为，保留）——这是节点自己的错，不是 Center 的抖动 |

即：**fail-closed 只对"Center 说不"，不对"Center 没说话"。**

`authAt` 的前移只发生在服务端注册表里，**不产生新 cookie**——客户端手里那份仍按 §6.3 的原定时刻过期。这样 Center 恢复后，客户端的下一次新建连仍会如期回到 Center，自治状态不会被带出这条连接的生命周期。

前移有上限：同一会话连续无法判定累计超过 `cookie_grace_max`（默认 6 小时）后转为断连，避免 Center 永久失联时节点无限期自治。

节点侧还需要区分"Center 拒绝"与"Center 不可达"，才能把 Center 的 `code` 映射成 §5.2 的结构化错误码回给客户端。这正是 §5.3 要把 `NewRemoteValidator` 从返回 `bool` 改为返回 `{ok, code, userID, serviceExpiredAt}` 的原因——今天它把这两类失败压成了同一个 `false`，信息在那一步就丢了。

### 6.5 长连接周期重校

**没有它，"15 分钟"这个承诺不成立。** 今天（以及分支上）一条已建立的连接从不被重新校验——挂着 6 小时 QUIC 连接的用户退订后，这 6 小时内不会被踢。

`QUICServer`/`TCPWSServer` 当前**没有连接注册表**（连接在 `acceptLoop` 的 per-goroutine 里处理完就没了）。新增：

```go
// wire/session.go
type authSession struct {
    auth   *connAuth
    udid   string
    token  string   // 冷认证时的凭据，供重校使用
    mode   string
    authAt atomic.Int64
    svcExp int64
    kill   func()   // 断开该连接
}
```

注册于连接接受时，注销于连接关闭时。清扫 goroutine 每 60 秒执行一次：

1. 快照（持 state lock）→ **释放锁**。
2. 对每个会话：`svc_exp` 已过 → `kill()`，零 Center 往返。
3. `now - auth_at > 15min` → 调用 validator（**锁外**，命中 `CachingValidator` 则不触 Center）→ 按 §6.4 处置。

严格遵守 `k2/CLAUDE.md` 的并发宪法：临界区只做字段读写，Center 调用与 `kill()` 都在锁外，新互斥量登记进 Lock Ordering Graph（`wire.authSessions.mu`，standalone，不与任何 engine 锁嵌套）。

Center 请求量封顶：每设备每节点每 15 分钟 1 次，与新建连共用同一个缓存条目，不叠加。

### 6.6 会话可观测

`session_id` 随 `deviceTrafficReporter` 的批次上报：

```
POST /slave/device-traffic
{boot_id, batch_seq, ts, devices:[{udid, session_id, rx, tx}]}
```

Center 侧只记录不判断。目的是拿到"同一 udid 同时活跃在几个节点、几个会话"的真实分布，为将来的并发策略提供依据。**本次不设任何阈值、不做任何拦截。**

### 6.7 签名密钥管理

- 独立生成的 32 字节随机密钥，持久化到 `<cert_dir>/cookie.key`（权限 0600），与证书、ECH 私钥同一目录、同一生命周期。
- **不从 `NodeSecret` 派生**：`NodeSecret` 是 Center 下发的**认证凭据**，拿它当签名密钥会把两个爆炸半径绑在一起，而且它一轮换所有 cookie 会静默失效；独立节点（`users_file` 路径）根本没有 `NodeSecret`。
- 轮换：`key_id` 单调递增，保留前一把密钥用于验签，重叠期 = cookie TTL 的两倍（30 分钟）。**自动轮换周期 30 天**——不设自动轮换等于"泄漏后到人工发现为止"，而 `key_id` 字段的存在本来就是为了这个。

---

## 7. Phase 3 — 强制认证灰度

### 7.1 开关的前置条件（顺序是反的）

不是"等客户端都升级了就开 enforce"，而是：

```
Phase 0 铺满车队（新凭据）  ──►  Phase 1+2 铺满车队（发 metadata + 用 cookie）  ──►  才谈 enforce
```

Phase 0 必须先行且铺满，否则 enforce 打开的那一天，P1 的路由器和 P3/P4 的 always-on 用户会成片掉线且无法自愈。按用户确认的节奏，enforce 目标在**约两个月后**。

### 7.2 可测量的判据

`enforce=false` 期间，节点持续输出（每 5 分钟一条 INFO 汇总，可被 `kaitu-node-ops` 脚本采集）：

```
DIAG: auth-rollout  total=N  authed=N  cookie_hit=N  cold=N  unauthed=N  legacy_no_metadata=N
```

翻开关的判据（全部满足，且持续 7 天）：

- `unauthed / total < 0.5%`
- `legacy_no_metadata`（完全不发 metadata 的连接）绝对数低于人工可处置的量级
- `device_traffic` 的空 udid 桶占比 < 1%
- Phase 0 的凭据在 `/api/subs` 侧的采纳率 > 99%（Center 侧统计仍在用 access token 的请求数）

### 7.3 灰度顺序与回滚

1. 一台手工 canary 节点，`K2_ENFORCE_AUTH=1`，观察 24 小时（连接成功率、工单量、空 udid 桶）。
2. 按 `kaitu-node-ops` 的批量脚本铺开（先小批、再全量），不手工逐台。
3. 回滚：单个环境变量 + 重启容器。回滚条件写死在部署清单里：连接成功率跌幅 > 1%，或 24 小时内出现 ≥ 3 起同型工单。

"连接成功率"在服务端没有直接的观测量，所以它在计划里落地为 **`authed/total` 相对翻开关前 7 天基线的跌幅**——这是 §7.2 的埋点已经在产出的数字，不需要新增机制。写清楚这个替代口径，免得回滚时对着一个测不出来的指标争论。

### 7.4 配置改动

`docker/sidecar/main.go` 的 `k2v5ConfigTemplate`（当前不生成 `enforce_auth`，`cache_ttl` 硬编码 5m）：

```yaml
auth:
  users_file: "{{.UsersFile}}"
  remote_url: "{{.CenterURL}}/slave/device-check-auth"
  cache_ttl: 15m
  enforce_auth: {{.EnforceAuth}}
```

`docker/docker-compose.yml` **必须给两个容器都注入** `K2_ENFORCE_AUTH=${K2_ENFORCE_AUTH:-0}`：

- **k2s 容器**——`config/config.go:409` 读它，这是实际生效的那一处。
- **k2-sidecar 容器**——上面那段 yaml 的模板渲染发生在 sidecar 里（`generateK2V5Config` 直接 `os.Getenv`）。只注入 k2s 会让生成的配置文件恒写 `enforce_auth: false`，而实际行为被环境变量覆盖成 true。**配置文件撒谎比配置错误更难排查**：下一个人 `cat` 那个 yaml 会得到与线上行为相反的结论。

**部署前必须核实的一处风险**：sidecar 生成的 validator 链是 `users_file` → `remote_url`，`users_file` 优先。若共享节点的 users 文件里有任何条目，其中的 token 会**绕过 Center 直接通过**。宿主上的实际路径是 `/apps/k2s/users`（compose 里 bind-mount 的源头，容器内挂到 `/etc/k2v5/users`）——核实要查宿主路径，查容器内路径也行但别只查模板里写的那个字符串。分支已有的"enforce 打开且无真实 validator 则拒绝启动"（`server/server.go:163-166`）保留。

### 7.5 判据 4 目前没有数据源

§7.2 的前三条判据都有现成的数据来源，**第四条没有**：`/api/subs` 不记录请求用的是哪一类凭据，DB 里也没有请求级记录。因此 Phase 3 的第一批工作里必须包含"造出这个数据源"——在 `/api/subs` 加一行记录凭据类型的 INFO 日志作为权威口径，另用 `devices.tunnel_issue_at`（配合 `token_last_used_at` 近 7 天）做设备级的下界交叉验证。

这一条单独列出来，是因为它很容易被当成"到时候查一下就行"而拖到翻开关的前一天才发现无从查起。

---

## 8. 跨层契约变更清单

| 层 | 变更 | 兼容性 |
|---|---|---|
| `api/` | `TokenTypeTunnel` 常量；`tunnel_token_expiry` 配置；`Device.TunnelIssueAt` 列（需手动 migrate） | 新增，无破坏 |
| `api/` | `/slave/device-check-auth` 请求体加 `mode`；加封禁检查；token type 改 tunnel | **破坏性**：需与节点侧同步发布，或在过渡期同时接受 access 与 tunnel 两种 type |
| `api/` | `/api/subs`、`/api/tunnels`、`gateway-credential` 改发 tunnel token；滚动续期 | 老客户端仍能用 access token 连（因为过渡期双接受），无破坏 |
| `api/` | `/slave/device-traffic` 的 `devices[]` 加 `session_id` | 新增可选字段，无破坏 |
| `k2/wire/` | `MetadataFrame` 加 `cookie` key；metadata 响应改 `[0x00][kv]` | kv 编码天然兼容，已验证老客户端安全 |
| `k2/wire/` | `NewQUICClient`/`NewTCPWSClient` 参数位复用为 `*CookieJar` | 内部 API，调用点已存在 |
| `k2/config/` | `auth.cache_ttl` 默认 5m → 15m；`Subscription.creds` 改为可变 | 配置默认值变更需在部署清单标注 |
| `webapp/` | `authService.getCredentials()` 改读 tunnel token；移动端同步进 App Group / SharedPreferences | 需与 Phase 0 的 API 同步发布 |
| `docker/` | sidecar 模板 + compose 环境变量 | 部署时生效 |

**契约门**：本次不触及 `contracts/api-contract.json` 覆盖的品牌注册表，无需重生成。若实施中发现触及，按 `CLAUDE.md` 的要求执行 `cd api && UPDATE_CONTRACT=1 go test -count=1 -run TestExportContract ./...`。

---

## 9. 测试策略

**优先级最高的一条：每一个新写的门都必须做变异验证。** 多层校验下"测试绿"极易是因为输入被前置检查抢先拒绝，后置机制根本没被触达。每个门的测试写完后，故意把门改坏（删掉检查、恒返回 true），确认对应测试**确实变红**；不变红的测试作废重写。

具体：

- **准入门**：保留并扩展 `TestAdmissionGate_EgressSinksAreGated`（枚举所有 egress 入口，新增入口未接门则失败）。这是防止同型缺陷第四次出现的守卫。
- **cookie**：签发/验签/篡改拒绝/过期拒绝/密钥轮换重叠期/`svc_exp` 截断/`users_file` 分支。
- **`§6.4` 的三态语义**：三张表分别构造（Center 明确拒绝、Center 不可达、节点配置缺失），断言各自的处置。**这是最容易写成假绿的一组**——必须验证"不可达"路径真的走到了延长分支，而不是被上游某个检查提前挡掉。
- **周期重校**：注入可控时钟，验证 `svc_exp` 过期零 Center 断连、`auth_at` 超窗触发重校、Center 不可达时不断连、累计超 `cookie_grace_max` 后断连。
- **两传输一致性**：同一组表驱动用例跑 QUIC 与 TCP-WS 两遍，断言行为一致。这是防止 §5.2 的两份手抄实现再次分叉的结构性守卫。
- **凭据不入日志**：一条静态守卫测试，扫描 `wire/` 与 `server/` 的所有 `slog.*` 调用点，断言没有任何一处把 `cookie`、`token`、`NodeSecret` 作为值传入。这类约束靠人工 review 一定会漏——`§6.2` 的理由（S3 日志上传）意味着漏一次就是真实的凭据外泄。
- **api handler**：按 `CLAUDE.md` 的要求，新写的 handler 测试**必须 `go test -run X` 单跑与全量跑各一次**（`center` 包共享 viper/redis 全局状态，两种跑法结果可能不同），断言只锚定自己那道门。
- **DB 集成测试**：注意 `skipIfNoConfig` 会静默跳过 170 个测试，判据是 `-v` 下 **0 SKIP**。新 worktree 需 `mkdir -p center && cp <主仓>/center/config.yml center/`。
- **并发**：`go test -race -timeout 300s ./wire/...`；新增互斥量必须通过既有的 `deadlock_test.go` 与静态审计（`go-deadlock` 只在 runtime 生效，不能替代对所有 `Lock()` 点的静态检查）。

---

## 10. 风险与已知局限

### 10.1 Phase 0 是一次凭据体系的换血

`/slave/device-check-auth` 的 token type 变更是破坏性的。过渡期必须**同时接受** access 与 tunnel 两种 type（按 `claims.Type` 分派到对应的 IssueAt 比对），待车队铺满后再移除 access 分支。移除的时机与 enforce 翻开关是同一个判据集（§7.2），不设第二套。

### 10.2 节点本地 cookie ⇒ 换节点必然回 Center

15 分钟 TTL 下量级很小，但它意味着**节点切换与故障转移依赖 Center 可达**。而 Center 可达性本身就是审查的靶子（antiblock 控制面中继正是为此存在）。GFW 事件中用户在节点间失败转移时最需要它、也最可能够不着。

这是本设计明确接受的天花板，不是缺陷。缓解路径已存在（`wire/relay.go` 的控制面中继）。若将来它成为真实痛点，正确的演进方向是 Center 签发的跨节点凭据（§2 非目标里排除的那一项），而不是加长 cookie TTL——加长 TTL 会直接牺牲 §2 目标 5 的吊销承诺。

### 10.3 cookie 泄漏的窗口

cookie 在 TLS + ECH 内传输，泄漏需要先攻破传输层。一旦泄漏，攻击者可在**该节点**冒充该设备**至多 15 分钟**，且冒充的是一个已实名归属的 udid——流量会记在受害者账上，因此这是一个可被计量发现的攻击。相比现状（每条连接重传 24 小时有效的账户级 access token），这是净改善。

不做 cookie 与连接/IP 的绑定：绑 IP 会直接破坏移动网络切换，而那正是本设计要解决的问题。

### 10.4 本次不解决的

- 并发使用限制（只做可观测，§2）。
- 跨节点凭据（§10.2）。
- 会话级无缝切换（§2）。
- `users_file` 路径没有订阅时钟，自建节点走无限期分支——这是自建节点的预期语义，不是遗漏。

---

## 11. 实施顺序

四个 Phase 有严格的先后依赖，不可并行：

```
Phase 0  隧道凭据（api/ + webapp/ + mobile/ + k2 subscription）
   │      └─ 出口判据：/api/subs 侧 tunnel token 采纳率 > 99%，且 P1–P6 逐条复核通过
   ▼
Phase 1  统一 metadata 认证（k2/wire/ + k2/server/ + api/ 的 mode 校验 + k2/engine/ 错误码）
   │      └─ 出口判据：两传输行为一致性测试全绿且经变异验证
   ▼
Phase 2  cookie（k2/wire/ + k2/server/ + 周期重校 + 密钥管理）
   │      └─ 出口判据：§6.4 三态语义测试全绿且经变异验证；节点重启无惊群（压测）
   ▼
Phase 3  强制认证灰度（docker/ + 节点运维）
          └─ 出口判据：§7.2 全部满足且持续 7 天
```

Phase 1 与 Phase 2 在代码上高度耦合（cookie 走的就是 metadata 帧），但拆成两个 Phase 是为了让"认证真的生效了"与"认证变快了"分别可验证——合并会让任何一处失败都难以定位到底是哪一半的问题。

### 11.1 Phase 顺序是验证顺序，不是发布顺序

上面的箭头指的是**实现与验证**的先后，不是"要发三次客户端"。Phase 0/1/2 的客户端改动可以、也应该**装进同一个客户端版本**发出去；否则光等版本传播就要半年。

实际的发布编排是三步：

1. **先部署 api**（Phase 0 的服务端 + Phase 1 的 mode 校验）。此时 `/slave/device-check-auth` 已经进入 §10.1 的过渡期——同时接受 access 与 tunnel 两种 type，所以**存量客户端零感知**。
2. **再部署节点 k2s**（Phase 1 + Phase 2 的服务端）。`enforce_auth` 保持 `false`，节点对不发 metadata 的老客户端照常服务，只是开始记录 §7.2 的观测数据。顺序上必须在第 1 步之后（它依赖 api 已能接受 tunnel token）。
3. **然后发客户端**（Phase 0/1/2 的客户端改动一次发完），开始约两个月的传播等待。

这三步之间没有需要人工卡点的窗口，每一步都可独立回滚。**Phase 3 是唯一需要等的**——它等的是第 3 步的传播率，判据在 §7.2。

顺带说明为什么不需要老键→新键的兼容桥（项目约定禁止）：过渡期靠的是 `/slave/device-check-auth` 按 `claims.Type` **分派到两条完整路径**，不是在一条路径里做字段兼容。两条路径各自完整、各自可测，且移除 access 那条的时机与判据都写死在 §10.1。
