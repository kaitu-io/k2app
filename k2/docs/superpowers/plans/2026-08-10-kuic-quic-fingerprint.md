# kuic — QUIC 腿 Chrome 指纹伪装 + 去 port-hopping 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 给 k2 客户端 QUIC 腿加逐字节 Chrome-H3 指纹（保留真 ECH + K2CC），并在协议层移除 port-hopping。

**Architecture:** 三阶段。Phase 1 纯 k2，删 port-hopping。Phase 2 新建薄 fork `github.com/kaitu-io/kuic`（= apernet/quic-go + 客户端 crypto_setup 的 TLS engine 从 `crypto/tls` 换成 `metacubex/utls`，暴露吃 `ClientHelloSpec` 的拨号），单独通过"真 ECH + Chrome 指纹 + K2CC"验证门。Phase 3 把 k2 的 quic-go 依赖换成 kuic 并接线客户端拨号 + 端到端验证。

**Tech Stack:** Go 1.25；`github.com/apernet/quic-go`（现 pin `v0.57.2-0.20260111184307-eec823306178`）；`github.com/metacubex/utls v1.8.4`（已是 k2 依赖，含 ECH + QUIC 事件 API + `UQUICConn.ApplyPreset`）；`github.com/kaitu-io/k2/wire/k2cc`（K2CC 拥塞控制）。

## Global Constraints

- **设计权威**：`docs/superpowers/specs/2026-08-10-kuic-quic-fingerprint-design.md`（下称 spec）。每个 task 的需求隐含包含 spec 全文。
- **单一 quic-go / 单一 utls**：不引入 refraction/uquic、不引入第二个 utls fork。utls 恒为 `metacubex/utls`。
- **保留 K2CC**：客户端拨号成功后 `conn.SetCongestionControl(k2cc.NewCCSenderGFWMode(k2cc.DefaultMaxRate))` 必须照旧生效（`wire/quic.go`）。
- **保留 ECH**：真 ECH（`EncryptedClientHelloConfigList`），非 GREASE 假 ECH。
- **不留兼容桥**：删 port-hopping 直接删，不加 old→new 兼容层（见 memory `feedback_no_defensive_migration_bridges`）；部署影响写进 PR。
- **worktree 隔离**：非只读改动在独立 worktree + `feat/*` 分支；commit 用 `git commit --only <paths>`，commit 前单独查一次 staging。
- **指纹世代**：Chrome-H3 spec 对齐 TCP 腿现用的 `HelloChrome_120_PQ` 世代（`wire/tcpws.go`）。
- **验证门不可跳**：Phase 2 的"真 ECH + 指纹 + K2CC 端到端全绿 + ClientHello 逐字节 Chrome"门不过，禁止进 Phase 3。
- **api 测试假绿陷阱**（若碰到 go test）：判据是 `-v` 下 0 SKIP；wire race 需 300s+。

---

## Phase 1 — 去 port-hopping（纯 k2，独立分支 `feat/remove-port-hopping`，可先合先发）

**Files（全部在 k2 submodule）:**
- Modify: `wire/quic.go`（hop 字段/函数/拨号分支/mux）
- Modify: `wire/quic_p2p.go:28`（newQuicUDPMux 调用）
- Modify: `wire/wire.go`（Config 的 HopStart/HopEnd 字段 + WireStats 的 PortHopAttempts）
- Modify: `engine/health.go:45,262`（PortHopAttempts 遥测字段 + 赋值）
- Modify: `cmd/k2s/firewall.go:113`（hopNATRules + 调用点）
- Modify: `cmd/k2s/wizard.go:79,179,193`（port-hop prompt + printComplete 参数）
- Test: `wire/quic_test.go`、`wire/quic_race_test.go` 等含 hop 的测试

**Interfaces:**
- Produces（供 Phase 3 与其余 k2 消费）：`newQuicUDPMux(conn *quic.Conn) *quicUDPMux`（去掉 hopAttempts/hopExhausted 两参）；`QUICClient` 不再有 `hopStart/hopEnd/portHopAttempts/portHopExhaustedAt`；`Config` 不再有 `HopStart/HopEnd`；`WireStats`/`HealthStatus` 不再有 `PortHopAttempts`。

### Task 1.1：删客户端 hop 拨号逻辑 + hopEnabled/hopPort + atomics

**Files:** Modify `wire/quic.go`

- [ ] **Step 1: 定位并阅读** hop 相关点：结构体字段（`quic.go:354-355` `portHopAttempts`/`portHopExhaustedAt`，以及 `hopStart`/`hopEnd` 字段——grep `hopStart` 找结构体定义）、`hopEnabled()`（:387-390）、`hopPort()`（:392-395）、`NewQUICClient` 里 `hopStart: cfg.HopStart, hopEnd: cfg.HopEnd`（:372-373）、拨号分支（:501-507）。

- [ ] **Step 2: 删拨号分支**——把 `wire/quic.go:500-507`
```go
	addr := c.serverAddr // immutable after construction
	if c.hopEnabled() {  // reads atomics, no lock needed
		dialHost := cfg.Host
		if cfg.DialAddr != "" {
			dialHost = cfg.DialAddr
		}
		addr = net.JoinHostPort(dialHost, strconv.Itoa(c.hopPort()))
	}
```
改为：
```go
	addr := c.serverAddr // immutable after construction; 443 固定，不再 port-hop
```

- [ ] **Step 3: 删** `hopEnabled()`（:387-390）与 `hopPort()`（:392-395）两个方法；删结构体字段 `portHopAttempts`、`portHopExhaustedAt`（:354-355）与 `hopStart`、`hopEnd`（grep 定位）；删 `NewQUICClient` 里 `hopStart: cfg.HopStart, hopEnd: cfg.HopEnd`（:372-373）。保留 `handshakeFailStreak`（QUIC→TCP fallback 仍需），把其注释 "across different hop ports" 改为 "across reconnect attempts"。

- [ ] **Step 4: 删 hop 计数的读写**——`s.PortHopAttempts = c.portHopAttempts.Load()`（:978，随 Task 1.2 的字段一起）、reset 块 `c.portHopAttempts.Store(0)` / `c.portHopExhaustedAt.Store(0)`（:1144-1145）。

- [ ] **Step 5: 编译**
Run: `cd wire && go build ./...`
Expected: 报错仅剩 `newQuicUDPMux` 参数不匹配（Task 1.3 修）与 `cfg.HopStart/HopEnd`/`PortHopAttempts` 未定义（Task 1.2 修）——即预期的下游断裂。

### Task 1.2：删 hop 遥测字段（Config / WireStats / HealthStatus）

**Files:** Modify `wire/wire.go`、`engine/health.go`

- [ ] **Step 1: 删 Config hop 字段**——`wire/wire.go` 的 `Config` 结构体里 `HopStart`、`HopEnd` 字段（grep `HopStart` 定位）；同文件 `WireStats`（或对应 stats 结构）的 `PortHopAttempts int32`（:56）。

- [ ] **Step 2: 追溯 HopStart/HopEnd 的赋值源**——grep 全 k2 `HopStart`、`HopEnd`（config 解析、k2subs/k2v5 URL 解析、daemon 配置）。全部删除赋值点（不留兼容读取）。

- [ ] **Step 3: 删 health 遥测**——`engine/health.go:45` 的 `PortHopAttempts int32 \`json:"port_hop_attempts"\`` 字段；`:262` 的 `PortHopAttempts: ws.PortHopAttempts` 赋值。

- [ ] **Step 4: 编译**
Run: `cd wire && go build ./... && cd ../engine && go build ./...`
Expected: 仅剩 `newQuicUDPMux` 参数错（Task 1.3）。

### Task 1.3：删 quicUDPMux 的 hop 追踪

**Files:** Modify `wire/quic.go`、`wire/quic_p2p.go`

- [ ] **Step 1: 改 newQuicUDPMux 签名**——`wire/quic.go:1912`：
```go
func newQuicUDPMux(conn *quic.Conn, hopAttempts *atomic.Int32, hopExhausted *atomic.Int64) *quicUDPMux {
```
改为 `func newQuicUDPMux(conn *quic.Conn) *quicUDPMux {`，函数体删 `clientHopAttempts: hopAttempts, clientHopExhausted: hopExhausted,` 两行。

- [ ] **Step 2: 删 mux 结构字段**——`quic.go:1844-1845` 的 `clientHopAttempts`、`clientHopExhausted`。

- [ ] **Step 3: 删 recordRecv 的 hop 重置块**——`quic.go:1886-1896` 整个 `if m.clientHopAttempts != nil { ... }`。检查 `consecutiveRecvs`（:1849）与 `relayRecoveryRecvs`（:1878）：若删除 hop 重置块后 `consecutiveRecvs` 再无消费者，一并删（含 recordTimeout 里 `m.consecutiveRecvs.Store(0)`:1900 与常量）；grep `consecutiveRecvs` 确认。

- [ ] **Step 4: 改两处调用点**——`wire/quic.go:666`（客户端 `newQuicUDPMux(conn, &c.portHopAttempts, &c.portHopExhaustedAt)`）与 `wire/quic_p2p.go:28` 改为 `newQuicUDPMux(conn)`。

- [ ] **Step 5: 编译全 wire+engine**
Run: `cd wire && go build ./... && cd ../engine && go build ./...`
Expected: PASS（无报错）。

### Task 1.4：删/改 hop 测试 + 服务端 NAT/wizard + 全量绿

**Files:** Modify `wire/quic_test.go` 等、`cmd/k2s/firewall.go`、`cmd/k2s/wizard.go`

- [ ] **Step 1: 删服务端 hop NAT**——`cmd/k2s/firewall.go:113` `hopNATRules` 及其调用点（grep `hopNATRules`）；`cmd/k2s/wizard.go:79`（`promptYesNo("Enable port hopping?"...)`）、:179/:193（`printComplete` 的 `hopEnabled` 参数与 "Port hopping guide" 分支）。

- [ ] **Step 2: 删 hop 单测**——grep `wire/*_test.go`、`cmd/k2s/*_test.go` 里 `Hop`/`hop`，删专测 port-hopping 的用例，改动用到 `newQuicUDPMux`/`HopStart` 的测试到新签名。

- [ ] **Step 3: 全量编译 + vet**
Run: `go build ./... && go vet ./...`
Expected: PASS。

- [ ] **Step 4: 跑 wire + engine + k2s 测试**
Run: `go test ./wire/... ./engine/... ./cmd/k2s/... -short`
Expected: PASS（`TestLocalECHProviderFetchTemplate` 若红是 live-DNS flaky，单独跑确认；判据见 spec/memory）。

- [ ] **Step 5: race 抽验**（去 hop 不应引入竞态）
Run: `go test ./wire/ -run 'TestQUIC|UDPMux' -race -count=1`
Expected: PASS。

- [ ] **Step 6: Commit**
```bash
git add -A && git status   # 单独查 staging（commit 前必看）
git commit --only wire/quic.go wire/quic_p2p.go wire/wire.go engine/health.go cmd/k2s/firewall.go cmd/k2s/wizard.go wire/quic_test.go -m "refactor(quic): 协议层移除 port-hopping，443 固定端口

port-hopping 与 443 伪装目标相悖（跳随机 UDP 口本身是异常信号）。移除客户端
hop 拨号/atomics、quicUDPMux hop 追踪、Config.HopStart/HopEnd、health 遥测
PortHopAttempts、服务端 NAT redirect + wizard prompt。不留兼容桥：老配置 hop
参数变 no-op。

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Phase 2 — 薄 fork `github.com/kaitu-io/kuic` + 验证门（新 repo）

> **前置**：这是新 repo，不在 k2 内。先在本地 clone apernet/quic-go 到独立目录，改 module path 为 `github.com/kaitu-io/kuic`，push 到 `github.com/kaitu-io/kuic`（默认分支保留一个纯 apernet 镜像 tag，便于追溯 rebase 基线）。

**Files（在 kuic repo）:**
- Modify: `go.mod`（module path → `github.com/kaitu-io/kuic`；add `require github.com/metacubex/utls v1.8.4`）
- Modify: `internal/handshake/crypto_setup.go`（客户端 `NewCryptoSetupClient` 的 TLS engine + `cryptoSetup` 字段/事件循环类型）
- Modify: `config.go`（`Config` 加 `ClientHelloSpec *utls.ClientHelloSpec` 字段）
- Modify: `client.go` / `connection.go`（把 `Config.ClientHelloSpec` 透传到 `NewCryptoSetupClient`）
- Create: `u_fingerprint_test.go`（验证门测试：真 ECH + Chrome spec + 抓包比对）

**Interfaces:**
- Produces（供 Phase 3 消费）：`quic.Config` 新增字段 `ClientHelloSpec *utls.ClientHelloSpec`（非 nil ⇒ 客户端握手用 utls 按此 spec 塑形 ClientHello；nil ⇒ 走原 `crypto/tls` 路径，server/relay/P2P 不受影响）。`Transport.Dial(ctx, addr, tlsConf, qcfg)` 签名不变——指纹经 `qcfg.ClientHelloSpec` 传入。

### Task 2.1：建 kuic repo（改 module path + 加 utls 依赖 + 基线镜像）

**Files:** `go.mod`、全 repo import path

- [ ] **Step 1:** clone apernet/quic-go 现 pin 版到 `~/projects/kaitu-io/kuic`，`git remote set-url origin git@github.com:kaitu-io/kuic.git`，打 tag `apernet-base-<pin>` 作为 rebase 基线并 push。
- [ ] **Step 2:** `go.mod` module 改 `github.com/kaitu-io/kuic`；全仓 `grep -rl 'github.com/apernet/quic-go' --include=*.go | xargs sed -i '' 's#github.com/apernet/quic-go#github.com/kaitu-io/kuic#g'`（内部 import self-path）。
- [ ] **Step 3:** `go get github.com/metacubex/utls@v1.8.4 && go mod tidy`。
- [ ] **Step 4: 编译**  Run: `go build ./...`  Expected: PASS（纯 rename，无逻辑改）。
- [ ] **Step 5: Commit** `chore(kuic): fork apernet/quic-go, rename module, add metacubex/utls dep`。

### Task 2.2：Config 加 ClientHelloSpec 并透传到 crypto setup

**Files:** `config.go`、`client.go`/`connection.go`、`internal/handshake/crypto_setup.go`

- [ ] **Step 1:** `config.go` 的 `Config` struct 加：
```go
// ClientHelloSpec, when non-nil, makes the client TLS handshake use
// metacubex/utls to shape the ClientHello (and QUIC transport parameters
// extension) to this fingerprint. nil keeps the standard crypto/tls path.
ClientHelloSpec *utls.ClientHelloSpec
```
（import `utls "github.com/metacubex/utls"`）
- [ ] **Step 2:** 追踪 `NewCryptoSetupClient` 的调用链（grep `NewCryptoSetupClient`，通常在 `connection.go` 的 client 连接构造）。给 `NewCryptoSetupClient` 增参 `chSpec *utls.ClientHelloSpec`，从 `conn.config.ClientHelloSpec` 传入。
- [ ] **Step 3: 编译**  Run: `go build ./...`  Expected: PASS（字段暂未使用，nil 默认走原路径）。
- [ ] **Step 4: Commit** `feat(kuic): thread Config.ClientHelloSpec to client crypto setup`。

### Task 2.3：把客户端 TLS engine 从 crypto/tls 换成 metacubex/utls（核心 seam）

**Files:** `internal/handshake/crypto_setup.go`

**关键语义陷阱**（必须处理，极可能是 mihomo #2428 根因）：`metacubex/utls` 的 `UQUICConn.SetTransportParameters` 在**使用 preset/spec 时不会**把 transport params 写进 ClientHello（见 `metacubex/utls/u_quic.go:163-180` 注释）。因此 apernet 在 `crypto_setup.go:99` `cs.conn.SetTransportParameters(cs.ourParams.Marshal(...))` 提供的 QUIC transport parameters，必须落到 **spec 里的 `QUICTransportParametersExtension.TransportParametersExtData`**。spec 必须含一个 `*utls.QUICTransportParametersExtension` 占位。

- [ ] **Step 1:** `cryptoSetup` 结构（:28-30）：把 `conn *tls.QUICConn` 改为接口/双态。最简做法——保留 `conn *tls.QUICConn`（nil 分支）并加 `uconn *utls.UQUICConn`；或定义一个内部 `quicTLSConn` 接口覆盖用到的方法（`Start/NextEvent/HandleData/SetTransportParameters/ConnectionState/SendSessionTicket/Close/StoreSession/StoreSession`）。**推荐接口法**，避免全循环里 if/else：
```go
type quicTLSConn interface {
	Start(context.Context) error
	NextEvent() /* tls or utls */ QUICEvent   // 见 Step 3 类型统一
	HandleData(level, data) error
	SetTransportParameters([]byte)
	ConnectionState() /* ... */
	SendSessionTicket(/* ... */) error
	StoreSession(/* ... */) error
	Close() error
}
```
> 注意：`tls.QUICEvent` 与 `utls.QUICEvent` 是**不同类型**，接口无法直接抹平返回类型。故采用 **Step 3 的类型别名策略**，让整个 crypto_setup.go 只用 utls 的 QUIC 类型；client 走 utls，server 仍走 `tls.QUICServer` 但把其事件**适配**成 utls 类型（见 Step 4）。

- [ ] **Step 2: 客户端构造**——`NewCryptoSetupClient`（:95-99）改为：
```go
if chSpec != nil {
	spec := ensureQUICTransportParamsExt(chSpec) // 保证含 *utls.QUICTransportParametersExtension 占位
	uconn := utls.UQUICClient(&utls.QUICConfig{
		TLSConfig:           utlsConfigFromStd(tlsConf), // 见 Step 5：std tls.Config → utls.Config（含 ECH）
		EnableSessionEvents: true,
	}, utls.HelloCustom)
	if err := uconn.ApplyPreset(spec); err != nil { /* return via cs.events 错误路径 */ }
	uconn.SetTransportParameters(cs.ourParams.Marshal(protocol.PerspectiveClient))
	// SetTransportParameters 在 preset 下不进 ClientHello；ensureQUICTransportParamsExt
	// 已在 spec 放好扩展，ApplyPreset 后由 utls 用 SetTransportParameters 的值回填。
	cs.uconn = uconn
} else {
	cs.conn = tls.QUICClient(&tls.QUICConfig{TLSConfig: tlsConf, EnableSessionEvents: true})
	cs.conn.SetTransportParameters(cs.ourParams.Marshal(protocol.PerspectiveClient))
}
```
> `ensureQUICTransportParamsExt`、`utlsConfigFromStd` 为本 task 新增 helper（同文件或 `u_helpers.go`）。

- [ ] **Step 3: 类型统一**——在 kuic handshake 包加类型别名，让循环体只写一套：
```go
// u_quic_types.go
type qEvent = utls.QUICEvent
type qEventKind = utls.QUICEventKind
type qEncLevel = utls.QUICEncryptionLevel
const ( qNoEvent = utls.QUICNoEvent; qSetRead = utls.QUICSetReadSecret; /* ...全枚举... */ )
```
把 `handleEvent`（:246 起）的 `tls.QUIC*` 常量与 `ev tls.QUICEvent` 全部换成上面别名。

- [ ] **Step 4: server 分支适配**——server 仍用 `tls.QUICServer`（不需伪装）。写一个 `stdEventToUtls(tls.QUICEvent) utls.QUICEvent` 适配（字段同名同序：Kind/Level/Suite/Data），使 server 的事件也喂进统一循环；或保留 server 的独立小循环（二选一，推荐适配器，循环单一）。

- [ ] **Step 5: ECH 承接**——`utlsConfigFromStd(std *tls.Config) *utls.Config`：拷 `MinVersion`、`ServerName`、`RootCAs`、`InsecureSkipVerify`、`VerifyPeerCertificate`，并把 `std.EncryptedClientHelloConfigList` → `utls.Config.EncryptedClientHelloConfigList`（真 ECH）。确认 metacubex `utls.Config` 有该字段（grep）。

- [ ] **Step 6: 编译**  Run: `go build ./...`  Expected: PASS。
- [ ] **Step 7: Commit** `feat(kuic): swap client TLS engine to metacubex/utls with ClientHelloSpec + ECH`。

### Task 2.4：验证门（命门）— 真 ECH + Chrome spec + 握手全绿

**Files:** Create `u_fingerprint_test.go`

- [ ] **Step 1: 写测试**——起一个带真 ECH keys 的 `tls.QUICServer`（用现有 quic-go server test helper 或标准库），客户端用 kuic 以 `Config.ClientHelloSpec = <Chrome-H3 spec 含 QUICTransportParametersExtension>` + `EncryptedClientHelloConfigList = <server 对应 ECHConfigList>` 拨号，断言握手完成且 `ConnectionState().ECHAccepted == true`（或 utls 等价字段）。
```go
func TestKuic_ECH_ChromeSpec_Handshake(t *testing.T) {
	// 1. 生成 ECH keypair → server EncryptedClientHelloKeys + client ECHConfigList
	// 2. tls.QUICServer(Listen) with ECH keys（apernet 侧，不动）
	// 3. kuic client: qcfg.ClientHelloSpec = chromeH3Spec(); tlsConf.EncryptedClientHelloConfigList = echList
	// 4. Transport.Dial → 断言 err==nil && state.ECHAccepted
}
```
- [ ] **Step 2: 跑，预期先失败**（spec 未接好 transport params 时握手会 `CRYPTO_ERROR` / `ECH rejected` —— 正是 mihomo #2428 形态）
Run: `go test ./... -run TestKuic_ECH_ChromeSpec_Handshake -v -count=1`
- [ ] **Step 3: 修到全绿**——若失败，按下列顺序钻：① `ensureQUICTransportParamsExt` 是否真把 `cs.ourParams.Marshal()` 的值回填进扩展（打印 ClientHello 抓 QUIC TP 扩展字节）；② ECH 是否进了 outer ClientHello（`state.ECHAccepted`）；③ ciphersuite/版本协商。**不得通过关掉 ECH 或换 GREASE ECH 绕过**。
- [ ] **Step 4: 指纹逐字节比对**——抓 kuic 发出的 QUIC Initial ClientHello，与真实 Chrome-H3 的 ClientHello（clienthellod / 抓包样本）比对扩展集合、顺序、GREASE、ALPS、compress_certificate。断言一致（或差异仅在合法可变字段如 key_share 随机值）。
Run: `go test ./... -run TestKuic_Fingerprint_MatchesChrome -v -count=1`
Expected: PASS。
- [ ] **Step 5: Commit** `test(kuic): ECH+Chrome-spec handshake gate + fingerprint byte-match`。
- [ ] **验证门裁决**：Step 3 与 Step 4 全绿 → 准入 Phase 3；任一不过 → 停在此，回 Task 2.3 钻 utls ECH-over-QUIC，禁止进 Phase 3。

### Task 2.5：K2CC 兼容确认（kuic 不破坏可插拔拥塞控制）

**Files:** 复用 apernet 既有 `connection.go` `SetCongestionControl` + `congestion` 包（未改动，仅确认）

- [ ] **Step 1:** 写测试：kuic 拨号后 `conn.SetCongestionControl(<mock CongestionControlEx>)`，断言 mock 的回调被调用（发几个包看 `OnPacketSent`）。
- [ ] **Step 2:** Run: `go test ./... -run TestKuic_SetCongestionControl -v`  Expected: PASS（K2CC 路径在 utls 分支下仍生效——拥塞控制在传输层，与 TLS engine 正交）。
- [ ] **Step 3: Commit** `test(kuic): confirm SetCongestionControl works under utls dial path`。

### Task 2.6：kuic CI + 维护文档 + 发版

- [ ] **Step 1:** 加 `.github/workflows/test.yml`（go build + 上面 gate 测试）。
- [ ] **Step 2:** 写 `MAINTAINING.md`：rebase apernet 上游流程、Chrome 世代更新指纹 spec 的流程、`apernet-base-<pin>` 基线 tag 约定。
- [ ] **Step 3:** 打 tag `v0.1.0` push。

---

## Phase 3 — 集成进 k2 + 端到端验证（纯 k2，分支 `feat/kuic-quic-fingerprint`）

**Files（k2 submodule）:**
- Modify: `go.mod`（`apernet/quic-go` → `kaitu-io/kuic` via replace 或直接 require + 全 import rename）
- Modify: 全 k2 `import "github.com/apernet/quic-go"` → `"github.com/kaitu-io/kuic"`（server/relay/P2P/client 全部）
- Modify: `wire/quic.go`（客户端拨号传 `qcfg.ClientHelloSpec` + ECH）
- Modify: `wire/ech.go`（新增/复用 Chrome-H3 spec 构造）
- Test: `wire/quic_test.go`、真机 smoke

**Interfaces:**
- Consumes：kuic `quic.Config.ClientHelloSpec *utls.ClientHelloSpec`（Phase 2 Produces）。

### Task 3.1：k2 依赖 apernet/quic-go → kaitu-io/kuic

**Files:** `go.mod`、全 k2 import

- [ ] **Step 1:** `go.mod`：`require github.com/kaitu-io/kuic v0.1.0`，删 `github.com/apernet/quic-go`（若 kuic 内部 import self-path，k2 侧直接换）。开发期可先 `replace github.com/kaitu-io/kuic => ../kuic` 本地联调。
- [ ] **Step 2:** `grep -rl 'github.com/apernet/quic-go' --include=*.go . | xargs sed -i '' 's#github.com/apernet/quic-go#github.com/kaitu-io/kuic#g'`（含 `wire/quic.go`、`wire/k2cc/*`、`server/*`、`wire/quic_p2p.go`、`wire/relay.go` 等）。
- [ ] **Step 3:** `go mod tidy && go build ./...`  Expected: PASS（kuic 是 apernet 超集，非 utls 路径行为不变）。
- [ ] **Step 4:** 跑 server/relay/P2P 相关测试确认无回归 Run: `go test ./wire/... ./server/... -short`  Expected: PASS。
- [ ] **Step 5: Commit** `chore(k2): migrate quic-go dependency to kaitu-io/kuic`。

### Task 3.2：构造 Chrome-H3 ClientHelloSpec（对齐 TCP 腿世代）

**Files:** Modify `wire/ech.go`（或新建 `wire/quic_fingerprint.go`）

- [ ] **Step 1: 写 spec 构造**——从 `metacubex/utls` 取 Chrome-H3 的 ClientHelloSpec（`utls.UTLSIdToSpec(utls.HelloChrome_120_PQ)` 或 metacubex 的 QUIC 专用 helper——grep utls 里 QUIC parrot），确保含 `*utls.QUICTransportParametersExtension` 占位：
```go
// buildChromeH3Spec 返回对齐 TCP 腿 HelloChrome_120_PQ 世代的 QUIC ClientHelloSpec。
func buildChromeH3Spec() (*utls.ClientHelloSpec, error) { ... }
```
- [ ] **Step 2: 单测**——断言 spec 含 QUICTransportParametersExtension 且扩展集合非空。
Run: `cd wire && go test -run TestBuildChromeH3Spec -v`  Expected: PASS。
- [ ] **Step 3: Commit** `feat(wire): Chrome-H3 QUIC ClientHelloSpec builder`。

### Task 3.3：客户端 QUIC 拨号接入指纹 + ECH

**Files:** Modify `wire/quic.go`（`DefaultQUICConfig` 或拨号处）

- [ ] **Step 1:** `wire/quic.go` 拨号前把 spec + ECH 挂到 qcfg/tlsCfg：
```go
qcfg := DefaultQUICConfig()
spec, err := buildChromeH3Spec()
if err != nil { return nil, fmt.Errorf("wire: build chrome spec: %w", err) }
qcfg.ClientHelloSpec = spec
// tlsCfg 已含 EncryptedClientHelloConfigList（BuildClientTLSConfig），无需再动
```
（`BuildClientTLSConfig` 现已设 `EncryptedClientHelloConfigList = echBytes`，`wire/ech.go:74`——ECH 复用，不重复装配。）
- [ ] **Step 2: K2CC 不动**——确认拨号成功后 `conn.SetCongestionControl(...)`（原 :614）保持。
- [ ] **Step 3: 单测**——mock/loopback：k2 client（kuic + Chrome spec + ECH）↔ k2 server，断言 QUIC 连接建立 + 元数据鉴权通过 + K2CC 启用日志出现。
Run: `cd wire && go test -run 'TestQUIC.*ECH|TestQUICClient' -v -count=1`  Expected: PASS。
- [ ] **Step 4: Commit** `feat(wire): QUIC client dial uses Chrome-H3 fingerprint + ECH via kuic`。

### Task 3.4：端到端验证 + 兼容矩阵 + 真机 smoke

- [ ] **Step 1: 兼容矩阵**——对一台真 ECH 生产/canary 节点（未升级 = apernet 服务端）跑 kuic 客户端拨号，断言连通（服务端指纹无关，spec §6）。记录：kuic-client ↔ apernet-server ✅。
- [ ] **Step 2: 抓包核指纹**——真机/桌面 dev 构建发起 QUIC 连接，wireshark/抓包取 Initial ClientHello，比对真实 Chrome-H3。
- [ ] **Step 3: 真机 smoke**——按 release 信心框架，桌面 + 一个移动平台各连一次、跑通、看 K2CC 速度不回退（无真机 = 信心封顶，记入 PR）。
- [ ] **Step 4: 全量回归** Run: `go test ./... -short`（注意 api 假绿 0-SKIP 判据、wire race 300s+）。
- [ ] **Step 5: Commit + PR**——PR body 写：兼容矩阵结果、指纹比对证据、真机 smoke 范围与信心分（代码 vs 业务分开）、port-hopping 移除的部署影响、kuic 依赖引入。

---

## Self-Review 覆盖

- spec §1 问题（QUIC 未伪装 + ECH 裸露）→ Phase 2/3。
- spec §2 目标 1 指纹 → Task 2.3/3.2/3.3；目标 2 ECH → Task 2.3 Step 5 + 2.4；目标 3 K2CC → Task 2.5 + 3.3 Step 2；目标 4 去 hop → Phase 1；目标 5 单 fork/utls → Task 2.1/3.1；目标 6 兼容 → Task 3.4 Step 1。
- spec §3 决策 2/3（薄 fork + 超集迁移）→ Phase 2 + Task 3.1。
- spec §4 seam + transport-params 陷阱 → Task 2.3（含 mihomo #2428 处理）。
- spec §6 兼容矩阵 → Task 3.4 Step 1。
- spec §7 残留（轴 B）→ 不在本 plan（触发式未来项，outer-SNI spec）。
- 验证门 → Task 2.4，明确"不过则不进 Phase 3"。
