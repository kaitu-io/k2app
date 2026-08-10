# kuic — QUIC 腿 Chrome 指纹伪装 + 去 port-hopping 设计

- 日期：2026-08-10
- 状态：设计（待 review → writing-plans）
- 范围：k2 客户端 QUIC 传输腿的抗指纹伪装；协议层移除 port-hopping；新建薄 fork `github.com/kaitu-io/kuic`
- 关联：`k2/wire/k2v5-protocol-spec.md`、`k2/wire/CLAUDE.md`（ECH gotchas）、`docs/superpowers/specs/2026-08-10-outer-sni-camouflage-design.md`（outer-SNI 伪装 + ECH 威胁模型）

---

## 1. 问题

k2v5 是复合隧道：**QUIC/H3 为主 + TCP-WS/smux 为备**。抗审查伪装的目标是：呈现给 GFW 的握手不带**罕见特征**，尽量像一次普通的、访问真实网站的 HTTPS/H3。

现状有一处实现不对称，使 QUIC 腿成为伪装最弱的一环：

- **TCP-WS 腿 / relay 腿**用 `metacubex/utls` 把 TLS ClientHello 逐字节塑形成 Chrome（`utls.UClient` + `HelloChrome_120_PQ`），见 `wire/tcpws.go:424`、`wire/relay.go:154`。
- **QUIC 腿**直接用 `apernet/quic-go` 的 `transport.Dial(ctx, addr, *crypto/tls.Config, qcfg)`（`wire/quic.go:564`），喂标准库 `crypto/tls`，**没有任何 ClientHello 指纹伪装**。其 ClientHello + QUIC transport parameters 带的是 apernet/quic-go + Go stdlib 的指纹，与真实 Chrome-H3 不同。

两个独立事实叠加，使 QUIC 腿的暴露面**大于** TCP 腿：

1. **QUIC Initial 包不是保密的**（RFC 9001 §5.2）：Initial 密钥由公开 salt + 明文 DCID 推出，任何在途观察者都能解开 Initial 包、读到其中 CRYPTO 帧里的 TLS ClientHello——**ECH 扩展、config_id、outer SNI 与 TCP-TLS 一样裸露**。换 HTTP/3 **不能**把 ECH 藏起来。
2. **QUIC 腿未做 Chrome 指纹**：于是"一个不像 Chrome 的 QUIC 握手却带了 ECH"，比 TCP 腿"逐字节像 Chrome 的 ECH"更罕见、更易被挑出。

### 两轴拆分（伪装的可解与不可解）

罕见特征**不是 ECH 本身**——真实 Chrome 默认就发 ECH（发给 Cloudflare）。若握手逐字节像 Chrome，被动过滤想按"带 ECH"过滤就会连真 Chrome 一起误伤。真正的区分器只剩两条轴：

- **轴 A — 指纹轴（本设计要修）**：ECH-携带的握手是否逐字节像真实流行客户端。TCP 腿：是。QUIC 腿：**否**。可修。
- **轴 B — IP 轴（本设计不修）**：`ECH-presence AND IP∉CDN` 被动窄封。QUIC/TCP 都躲不掉，不在传输层可解。对策是 §6 领先指标监测 + 无 ECH 真站腿（触发式），见 outer-SNI 设计 spec。**换 HTTP/3 或加指纹对轴 B 毫无帮助。**

本设计只解**轴 A**。

---

## 2. 目标 / 非目标

**目标**
1. QUIC 腿的 ClientHello + QUIC transport parameters 逐字节塑形成**当前稳定 Chrome 的 H3 指纹**（与 TCP 腿的 Chrome 伪装同一哲学）。
2. **保留 ECH**（真 ECH，非 GREASE 假 ECH）——ECH 是 k2v5 核心，不可丢。
3. **保留 K2CC**（GFW 模式可插拔拥塞控制，`wire/quic.go:614` `conn.SetCongestionControl`，"critical for download speed"）。
4. **协议层移除 port-hopping**：443 定死不 hop，与"最到位的 443 伪装"自洽。
5. 单一 quic-go、单一 utls fork（不引入第二套）。
6. 兼容：客户端单侧改动；新指纹客户端 ↔ 老/新服务端同 QUIC v1 互通；旧客户端继续可用。

**非目标**
- 轴 B（`ECH+IP` 窄封）——记为残留，属 outer-SNI 设计 spec 的触发式未来项。
- 服务端伪装——服务端不发 ClientHello，无需伪装（但需保留 K2CC）。
- 多指纹/指纹轮换——先落地单一 Chrome-H3，与 TCP 腿一致。

---

## 3. 关键决策与证据

设计建立在源码级核实上（应"10/10 置信度 + 先验证再设计"的要求）。附录 A 列全部证据。

### 决策 1：移除 port-hopping（协议层）
443 是最到位的伪装；往随机 UDP 口 hop 本身是异常信号，与伪装目标相悖。移除后 QUIC 恒定 443。爆炸半径（附录 A.4）中等且自洽。

### 决策 2：`kuic` = apernet/quic-go 的**薄 fork**，而非引入 uquic
先前设想引入 `refraction-networking/uquic` 会带来第二个 quic-go + 第二个 utls（refraction/utls，非我们 ECH 所在的 metacubex/utls），且 uquic 是上游 quic-go 的 fork、**没有 apernet 的可插拔拥塞控制**（K2CC 焊死在 `apernet/quic-go/congestion.CongestionControlEx`，附录 A.2）——换 uquic 会丢 K2CC。故否决。

**核心洞察**：我们**已经在用**的 `metacubex/utls@v1.8.4`（ECH 已跑在其上）本身就同时具备：
- **crypto/tls 兼容的 QUIC 事件 API**：`QUICClient()` / `type QUICConn` / `Start()` / `NextEvent()` / `HandleData()` / `QUICEvent` / `QUICEncryptionLevel`（`metacubex/utls/quic.go`）——与 apernet `internal/handshake/crypto_setup.go` 驱动的**同一套接口**，事件枚举 1:1 同名（附录 A.3）。
- **QUIC 版 uTLS 指纹注入**：`UQUICConn.ApplyPreset(*ClientHelloSpec)`（`metacubex/utls/u_quic.go:61`）。
- **ECH 走 QUIC 路径的管道**：`QUICConfig{ TLSConfig *Config }`，`EncryptedClientHelloConfigList` 设在该 `Config` 上（TCP 腿现状同源）。

因此 `kuic` 只需：fork `apernet/quic-go`，把**客户端** `crypto_setup.go` 的 TLS engine 从 `crypto/tls` 换成 `metacubex/utls`，并开一个吃 `ClientHelloSpec` 的拨号入口。K2CC、transport、server、relay、P2P **一行不动**。

### 决策 3：全 k2 依赖 `apernet/quic-go` → `kaitu-io/kuic`（超集）
kuic 是 apernet 的超集（apernet + 客户端 utls 拨号选项）。全 k2 统一换成 kuic：server/relay/P2P 走非 utls 老路径不变，只有客户端 QUIC 拨号 opt-in 走 utls+spec+ECH。→ **单一 quic-go，单一 utls，无双 fork**。

---

## 4. 架构

### 4.1 seam
apernet `internal/handshake/crypto_setup.go`：
- `cs.conn = tls.QUICClient(&tls.QUICConfig{...})`（:95）
- 事件循环 `conn.Start()` / `conn.NextEvent()` / `conn.HandleData()` + `switch ev.Kind`（QUICSetReadSecret / QUICWriteData / QUICTransportParameters / QUICTransportParametersRequired / QUICRejectedEarlyData / QUICHandshakeDone / QUICStoreSession / QUICResumeSession / QUICNoEvent）。

kuic 客户端改为：
- `cs.conn = utls.UQUICClient(...)`（或等价构造），`ApplyPreset(chromeH3Spec)`；
- 事件类型 `tls.QUICEvent` → `utls.QUICEvent` 等（同名枚举，直接替换）；
- ECH 经 `QUICConfig.TLSConfig.EncryptedClientHelloConfigList` 注入。

服务端 `tls.QUICServer`（:130）**不动**（服务端不发 ClientHello、无需伪装，标准库 ECH 终结照旧）。

### 4.2 新增 fork 入口
kuic 在包级新增（例）`u_transport.go`：
```
func (t *UTransport) Dial(ctx, addr, utlsConfig *utls.Config, spec *utls.ClientHelloSpec, qcfg *Config) (*Conn, error)
```
其余 API 与 apernet 完全一致，供 server/relay/P2P 原样调用。

### 4.3 k2 侧接线
`wire/quic.go` 客户端拨号（当前 `transport.Dial`）改走 kuic 的 utls 拨号：
- spec = Chrome-H3（对齐 TCP 腿 `HelloChrome_120_PQ` 的 Chrome 世代）；
- `utls.Config.EncryptedClientHelloConfigList` = 现有 `ParseECHConfig(cfg.ECH)` 产物（复用 `BuildClientTLSConfig` 的 ECH 装配）；
- 拨号成功后 `conn.SetCongestionControl(k2cc.NewCCSenderGFWMode(...))` 照旧（K2CC 不变）。

---

## 5. 分阶段（3 个，每个独立分支/PR）

**Phase 1 — 去 port-hopping**（独立先行，纯 k2）
- 删：客户端 `hopEnabled/hopPort/portHopAttempts/portHopExhaustedAt` + 拨号 hop 分支（`wire/quic.go:501-506`）、config hop 字段、服务端 `cmd/k2s` NAT redirect（`firewall.go:113`）+ wizard（`wizard.go:79/193`）、health 遥测 `PortHopAttempts`（`wire/wire.go:56`、`engine/health.go:45/262`）。
- 留：`quicUDPMux` 若服务于非 hop 用途，仅摘 hop 追踪（`wire/quic.go:1844-1919`，实现时确认）。
- 兼容：老配置 hop 参数变 no-op，直接删不留兼容桥（部署清单写清）。
- 独立性：不依赖 Phase 2/3，可先合先发。

**Phase 2 — 薄 fork `github.com/kaitu-io/kuic` + 验证门**（新 repo，去风险核心）
- 基座 = apernet/quic-go（pin 现用版）；仅改客户端 crypto_setup seam + 加 utls 拨号入口（§4.1/§4.2）。
- **验证门（命门）**：真 ECH + Chrome-H3 spec + K2CC **端到端对真 ECH 服务端全绿**，且抓 QUIC ClientHello **逐字节确认 Chrome**。此即 mihomo #2428（ECH + clientFingerprint 冲突，未解）的复现验证；因是我们自己的薄补丁 + 自己的 utls，可真正 debug。**门不过 → 钻 metacubex/utls 的 ECH-over-QUIC 代码，不得跳过、不得进 Phase 3。**

**Phase 3 — 集成进 k2 + 端到端验证**（纯 k2）
- 全 k2 依赖 `apernet/quic-go` → `kaitu-io/kuic`；`wire/quic.go` 客户端拨号切 kuic utls 入口（§4.3）；server/relay/P2P 不改逻辑（走 kuic 非 utls 路径）。
- 指纹：抓包逐字节比对真实 Chrome-H3（clienthellod 类工具）。
- 真机 smoke（无真机 = 信心封顶，遵 release 信心框架）。
- 兼容矩阵见 §6。

---

## 6. 兼容性矩阵

服务端接 QUIC 是 `server.go:467` `quicTransport.Listen(s.TLSConfig.Clone(), ...)`——标准 apernet Listen + 标准库 `EncryptedClientHelloKeys` 终结 ECH，**对 ClientHello 字节形状无所谓**，只要求：合法 TLS1.3 ClientHello + ECH 指向其持有的 config_id + 协商出支持的 QUIC 版本/ciphersuite。

| 客户端 | 服务端 | 结果 | 依据 |
|---|---|---|---|
| kuic（Chrome 指纹 + ECH） | apernet 或 kuic（未升级/已升级） | ✅ 通 | 服务端指纹无关；同 QUIC v1；ECH config_id 一致 |
| 老客户端（apernet 原生形状） | 同上 | ✅ 通 | 不受影响，纯客户端单侧改动 |
| kuic | 老服务端（Phase 0 前，仍有 port-hop NAT） | ✅ 通 | 443 固定端口本就在 hop range 内；去 hop 只是客户端不再跳 |

**结论**：给 QUIC 腿加指纹 + 去 hop = 纯客户端单侧改动，服务端零改动，新老共存。唯一硬前提=指纹库能携带我们的 ECH（Phase 2 验证门）。

---

## 7. 残留与未来工作

- **轴 B（`ECH+IP` 被动窄封）**：不在本设计范围；outer-SNI 设计 spec 的触发式未来项（§6 领先指标 + 无 ECH 真站腿）。
- **kuic 上游维护**：需跟 apernet 上游 rebase + 跟 Chrome 世代更新指纹 spec；作为 fork 长期成本，纳入 kuic repo 的维护文档。
- **多指纹/轮换**：暂不做，先单一 Chrome-H3。

---

## 附录 A：验证证据（源码级）

**A.1 QUIC 腿未伪装**：`wire/quic.go:564` `transport.Dial(..., tlsCfg, qcfg)` 喂标准 `crypto/tls.Config`；uTLS `HelloChrome_120_PQ` 仅现于 `wire/tcpws.go:424`、`wire/relay.go:154`；`apernet/quic-go` 源码零 `ClientHelloSpec`/utls 引用。

**A.2 QUIC 腿依赖 apernet 专有 K2CC**：`wire/quic.go:614` `conn.SetCongestionControl(k2cc.NewCCSenderGFWMode(...))`（注释 "critical for download speed"）；`SetCongestionControl` 是 apernet 扩展（`apernet/quic-go/connection.go:3122`），上游无；K2CC 实现 `apernet/quic-go/congestion.CongestionControlEx`（`wire/k2cc/k2cc.go:1400`）。→ 换 uquic（上游 fork）丢 K2CC。

**A.3 metacubex/utls 具备 crypto/tls 兼容 QUIC API + 指纹注入 + ECH 管道**：
- `metacubex/utls/quic.go`：`QUICClient()`(:175)、`type QUICConn`(:43)、`Start()`(:203)、`NextEvent()`(:220)、`HandleData()`(:257)、`type QUICEvent`(:126)、`QUICEventKind`(:62)。
- 事件枚举与 apernet switch **1:1 同名**：QUICNoEvent/SetReadSecret/SetWriteSecret/WriteData/TransportParameters/TransportParametersRequired/RejectedEarlyData/HandshakeDone/Resume/StoreSession。
- `u_quic.go:61` `UQUICConn.ApplyPreset(*ClientHelloSpec)`。
- `QUICConfig{ TLSConfig *Config }`（:50）——ECH 经 `TLSConfig.EncryptedClientHelloConfigList`。

**A.4 port-hopping 爆炸半径**：客户端 `wire/quic.go`（354-355 atomics、387-393 hopEnabled/hopPort、501-506 拨号分支、666/1144-45 mux、1844-1919 quicUDPMux hop 追踪）；`wire/quic_p2p.go:28`；config hop 字段；服务端 `cmd/k2s/firewall.go:113`、`cmd/k2s/wizard.go:79/179/193`；遥测 `wire/wire.go:56`、`engine/health.go:45/262`。relay 本就不 hop（`wire/relay.go:23`）。

**A.5 ECH-over-QUIC 脆弱性（外部）**：mihomo #2428（ECH + clientFingerprint 冲突，2025-12 开未解）；quic-go #4980（ECH-over-HTTP/3 被 reject）。→ Phase 2 验证门存在的理由。
