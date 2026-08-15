# tessera — 验证场

Tessera 协议（设计稿见 `k2/docs/tessera/`）的**隔离验证场**，不是出厂地。

这里做一件事：把有风险的机制在**碰不到生产代码**的地方建出来、跑穿、证死。证好之后**移植**进 k2 的 `wire/`——那才是节点二进制里判别真正发生的地方。所以这里的代码刻意对 k2 **零依赖**、保持可移植。

## 为什么独立成模块

第一件要验证的事，是给 `apernet/quic-go` 打补丁。补丁经 go.mod 的 `replace` 生效，而 **`replace` 作用于主模块的整个构建图**：放进 k2，就等于把**正在服务用户的 k2v5 QUIC 路径**踩在一个未经验证的 fork 上。放在这里，`replace` 只作用于本模块，k2 构建完全不受影响。

## 上手

```bash
make fork    # 从 module cache 物化 apernet/quic-go 并应用补丁（首次必须）
make test    # 跑本模块测试
make test-fork  # 跑 quic-go 自己的测试套件——补丁的回归证据
```

`.fork/` **不进 git**（4.2 MB / 452 个文件的上游源码）。**`patches/` 才是交付物**：一个 203 行、8 个文件的补丁，将来整块挪进 k2。改完 fork 用 `make patch` 重新生成。

补丁做两件事，都是"把写死的东西变成可注入的"，没有行为改动：

| 接缝 | 为什么模块外做不到 |
|---|---|
| TLS 引擎接口化（`internal/handshake/crypto_setup.go`）+ `Config.ClientTLSConnFactory` | 字段类型是标准库具体类型 `*tls.QUICConn`，且在 `internal/` 包里 |
| `quic.NewClientToken(data, rtt)` 构造函数 | `TokenStore` 本来就是调用方提供的公开接口，但 `ClientToken` 的字段未导出、无构造函数 |

> `make patch` 会排除 `keylog.txt`（跑 fork 测试套件留下的产物，曾两次混进交付物），并把**任何只存在于 fork 侧的新文件**打印出来——静默排除和静默夹带一样糟。

> `make test-fork` 的 `integrationtests/self` 有一组会失败的用例（`TestStreamDataBlocked`、`TestHTTP3ListenerGracefulShutdown`、`TestHTTPServerIdleTimeout`、`TestHTTPClientRequestContextCancellation`、`TestGracefulShutdownLongLivedRequest`），它们都断言毫秒级时长或时序。**与补丁无关，已实测归因**：同一条命令在 `.fork/quic-go.orig`（未打补丁）与 `.fork/quic-go`（打了补丁）上各跑 3 次，失败集合相同且同样随机波动；其中 `TestStreamDataBlocked` 在**未打补丁**的副本上 3/3 全挂——那不是抖动，是上游在这台机器/这个 Go 版本上的既有失败。
>
> 脚本保留 `.fork/quic-go.orig` 正是为了做这种归因。**别靠"看起来像抖动"下结论**——上一轮我就是这么想的，而当时的真相恰好相反（是补丁引起的，见下方 `-checklinkname=0` 那条）。判据只有一个：在原始副本上跑同一条命令。

## `utlsquic` —— QUIC ClientHello 换 uTLS

**问题**：QUIC 的 ClientHello 装在 Initial 包里，而 Initial 的头部保护密钥由**明文的** Destination Connection ID 推导（RFC 9001 §5.2）——任何在途观察者都能解开来读。GFW 自 2024-04 起正在规模化做这件事。Go 标准库发出的 ClientHello 没有任何浏览器会产生。

**卡点**：quic-go 把 TLS 引擎写死成具体的 `*tls.QUICConn`，且在 `internal/` 包里，模块外无法替换。补丁把它命名为接口，并让调用方**按连接**通过 `quic.Config.ClientTLSConnFactory` 提供。

```go
&quic.Config{ClientTLSConnFactory: utlsquic.Factory(utls.HelloChrome_120)}
```

### 实测证据

`TestUTLSFingerprintSurvivesTheWire` 断言的是**服务端解析出来的**字节（而非 uTLS 的孤立输出），这才叫端到端——它证明 hello 没有在 quic-go 某处被重建。stdlib 那条臂是对照组：没有它，"有 GREASE" 并不能证明 factory 起了作用。

| 引擎 | cipher suites | GREASE suite | GREASE group |
|---|---|---|---|
| crypto/tls（对照） | 3 | 0 | — |
| uTLS Chrome_120 | 16 | 1 | 1 |

### 跑通过程中撞出来的三件事

都不是推理能得到的，只有真跑才会暴露：

1. **uTLS 预设覆盖 ALPN**。预设是浏览器 **TCP** 握手的抓包，ALPN 写死 `h2, http/1.1`；只提供 h3 的 QUIC 服务端直接以 `no application protocol` 中止。
2. **预设提供 TLS 1.2**。TCP Chrome 确实提供，但 **RFC 9001 §4.2 明令 QUIC 客户端 MUST NOT 提供低于 1.3 的版本**——留着既违规，又是在审查者恰好会解密的那个包里露的破绽。
3. **uTLS 用预设建 hello 时不写传输参数**。`u_quic.go` 里正确的接线是**被注释掉的**，且它引用的字段在当前结构体上已不存在。没有 `quic_transport_parameters`，服务端回 `missing extension`。本包因此自己解析 quic-go 的参数块并逐项塞回扩展——**quic-go 始终是这些值的唯一作者**，在这里另造一份就等于给连接限额搞出两个事实源。

### 已知的保真度缺口（未解决）

预设依然是一次 **TCP** 抓包。真 Chrome 的 **QUIC** ClientHello 在扩展集合与顺序上另有不同，uTLS 目前没有任何预设建模这一点。补齐它需要一份 Chrome 自己的 QUIC 握手抓包来逐字节 diff —— 见 `k2/docs/tessera/spec.md` §10 第 7 项。**所以本模块证明的是"能把浏览器形状的 hello 送到对端"，不是"与真 Chrome 不可区分"。**

### 刻意的限制

- **不支持会话恢复**：`utls.UQUICConn` 没有 `StoreSession`，故 `EnableSessionEvents` 不开、0-RTT 不可用。
- **不支持客户端证书**。
- 会被 ClientHelloID 静默覆盖的 Config 字段（`CipherSuites`、`CurvePreferences` 等）**一律报错而非忽略**——静默忽略会让调用方以为某个设置生效了，而它没有。

---

## 服务端判别器

节点收到一条连接时判断"这是我们的客户端还是探测者"，**而且判断结果在线路上看不出来**：两条路径消耗的字节完全相同，差异只存在于节点私钥的运算里。

| 包 | 职责 |
|---|---|
| `credential` | §4.4 封装 / §4.5 判别：X25519 + AES-GCM，加上版本、时间窗、short_id、重放缓存 |
| `quicwire` | 从 Initial 数据报里取出 Token，只解析明文头部 |
| `demux` | 一个 UDP 端口两种命运：认证流交给本地 QUIC 栈，其余**逐数据报原样转发**给 front |
| `client` | 客户端拨号：uTLS 指纹 + 每次连接现铸的凭据 |

```go
// 节点
opener, _ := credential.NewOpener(credential.OpenerConfig{
    PrivateKey: nodePriv, Front: "www.example.com", ShortIDs: [][8]byte{sid},
})
dc, _ := demux.New(demux.Config{Conn: sock, Front: frontAddr, Classify: demux.TokenClassifier(opener)})
ln, _ := quic.Listen(dc, tlsConf, nil)   // 只会看到自己人

// 客户端
cfg, _ := client.Config(nodePub, "www.example.com", sid, utls.HelloChrome_120)
```

### 实测证据

`integration/` 里两个客户端拨同一个 UDP 端口，唯一差别是 Initial Token 里的 64 字节：

| 拨号方 | 谁服务了它 | 拿到谁的证书 |
|---|---|---|
| Tessera 客户端 | 节点 | 节点的 |
| 普通 quic-go（探测者） | **front** | **front 的真实证书** |

并且断言**节点的 QUIC 栈一次都没有解析过探测者的 ClientHello**——数据报根本没到达它。同一套测试还覆盖：重放的凭据被转给 front；服务端 NEW_TOKEN 被丢弃（留着会在下次拨号顶掉凭据，把自己人转去 front）；uTLS 臂 16 个 cipher suite / 有 GREASE，stdlib 对照臂 3 个 / 无 GREASE。

### 比 spec 更严的三处，和为什么

1. **重放缓存按时间保留，不按容量淘汰**，且保留期绑定**凭据自己的失效时刻**而非"入库 + 固定 TTL"。spec 写的是"LRU，容量与 REPLAY_WINDOW 匹配"——容量淘汰会在凭据**仍然可接受**的时候把它忘掉，而那正是重放能得手的窗口。
2. **缓存满时拒绝（fail closed），不腾位置**。满了意味着节点不再走认证路径、把所有人转给 front：**没用了，但仍然完美伪装**。反过来（淘汰）是保住可用性、却让在途攻击者能靠重放捕获的凭据来确认这个节点。二者只能取一个时，该保的是不可观测性。
3. **未配置 short_id 的节点谁也不认证**。把空集合读成"没有限制"会让一次配置疏漏变成一个开放节点。

### 归因纪律：一次空测试

`TestReplayCacheOutlivesTheAcceptanceWindow` 第一版**是绿的，但什么也没测**——我把凭据铸在接受窗口的**最早**一端，它在时钟往前走一分钟后就因时间戳过期被拒了，根本没轮到缓存说话。是变异验证（把保留期从 `2*window` 改成 `window`，测试**仍然通过**）暴露的。真正需要两倍窗口的是另一端：客户端时钟快、凭据是未来时间戳，寿命才最长。修好后同一变异立刻失败。

同样方式验过：过期判定改回非严格（边界差一）→ 失败；条目立即过期 → 失败；去掉容量上限 → 失败；判别器恒真 / 恒假 → 失败。**一处等价变异**（去掉"是否为 Initial 包"的检查）不失败，因为 `ParseInitial` 失败时返回零值、`OpenToken(nil)` 必然因长度不符而拒绝——检查被长度检查覆盖了，行为确实不变。

`quicwire.ParseInitial` 另跑了 30 秒 fuzz（123 万次）：无 panic，且凡是声称解析出来的字段都确实落在输入数据报内。

### 已知限制（已被测试钉住，不是遗漏）

- **连接迁移会打断认证连接**。判别按 4 元组做、且需要一个 Initial 包才能做；NAT 重绑定后首包是短头包、无 token，会被判成陌生人转给 front。修法是让节点认出自己签发的连接 ID——需要给 CID 生成器加标记，是**另一件事**。`TestMigratedFlowIsRelayed` 钉住当前行为，将来修好时这个测试应当被**有意**改掉，而不是被意外发现。
- **`ProbeFront` 只验 front 会说 QUIC**，不验它待客如常（非 403 / 挑战页），spec §4.1 的其余硬门槛仍需外部普查。
- **token 存在性本身是否是特征，未普查**。真实客户端只在此前拿到过 NEW_TOKEN 时才带 token；首包即带 token 的比例没测量过。Token 字段就在 Initial 的**明文**部分，连解密都不需要——这是继 ECH 之后同一类"凭推理会栽"的问题（spec §6.4）。

### 一个不显眼的坑

quic-go 把 token 携带的 RTT **直接喂给平滑 RTT 估计**。传 0 不是"没有意见"，它会把 smoothed RTT 置零、导致首个 flight 被异常激进地重传——正好是审查者盯着的那一次交换里的特征。所以 `client.InitialRTT` 取的是 quic-go 自己的 `DefaultInitialRTT`（100ms），也就是**无 token 连接**用的值。
