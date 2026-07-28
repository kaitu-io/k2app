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

`.fork/` **不进 git**（4.2 MB / 452 个文件的上游源码）。**`patches/` 才是交付物**：一个 179 行、8 个文件的补丁，将来整块挪进 k2。改完 fork 用 `make patch` 重新生成。

> `make test-fork` 里 `TestGracefulShutdownLongLivedRequest` 会**间歇性**失败：它断言一个时长落在 12.5 ms 容差内，机器一忙就超。**与补丁无关**——已实测判定：同一条命令下未打补丁的原始副本失败、打了补丁的通过（各 3 次）。脚本会在 `.fork/quic-go.orig` 留一份原始副本，正是为了做这种归因，别靠"看起来像抖动"下结论。

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
