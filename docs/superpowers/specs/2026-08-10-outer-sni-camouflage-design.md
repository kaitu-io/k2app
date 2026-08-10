# Outer SNI 伪装 —— 设计与订正

Date: 2026-08-10
Status: Design（讨论共识 + 事实订正，尚未落代码）
Scope: k2v5 wire 层 outer SNI / ECH 伪装策略；`wire/ech*.go`、`server/server.go`（`learnOuterSNI` / `handleRawTCP`）

本文有两个目的：(1) 固化一次"如何迷惑墙"的设计讨论共识；(2) 订正 spec / CLAUDE.md 里一批关于 outer SNI 的**错误断言**——它们会把下一个读者带进和我们第一轮相同的误判。

---

## 1. 订正的事实：outer SNI 并没有被"焊死"在 public_name 上

多处文档写着 **"uTLS forces outer SNI = ECH config's public_name — cannot be set independently"**。作为**能力断言，这是错的**。精确的事实分两半：

- **库层（metacubex/utls v1.8.4）不强制。** 服务端解密路径明确不校验 outer ClientHello 的 `server_name` 是否等于 ECH config 的 `PublicName`——trial decryption 只认 `config_id` + HPKE encap（`ech.go:610` 的 NOTE：*"we do not enforce that the sent server_name matches the ECH configs PublicName ... This is only a MAY in the spec"*）。所以协议/服务端对 outer SNI ≠ public_name 是**容忍**的。
- **当前客户端 emit 路径把它焊死了（行为，非协议要求）。** stock utls 客户端在 ECH 生效时无条件覆盖：`hello.serverName = string(ech.config.PublicName)`（`handshake_client.go:310`、`u_handshake_client.go:474`、`u_parrots.go:2856`）。k2 两条传输（`tcpws.go:424` 的 `UClient`、`quic.go:530` 交给 quic-go）都没有在握手前 `SetSNI` 覆盖，所以**出厂客户端线上发的 outer SNI 恒等于 public_name = `cloudflare-ech.com`**。

**结论**：解耦 outer SNI 与 public_name 在库层是**可行**的（服务端已容忍），只需覆盖客户端 emit（patch 或 ECH 应用后 `SetSNI`）。"不可能/改不了"是错误前提，`docs/knowledge/architecture-decisions.md:517` 基于该前提做的"配置化 SNI 不可能"决策记录需要重新审视（见 §7）。

---

## 2. 讨论共识（地基）

1. **ECH 保留。** 前提是"广封所有 ECH 误伤过大、不会发生"——双方同意。
2. **真正的残余威胁是窄封**（`ECH-presence AND IP∉CDN-whitelist`），其杀伤力**不由 IP 决定**：IP∉CDN 单独匹配大半个互联网（零精度，墙用不了），判别力全在"在无名 IP 上携带了罕见的第二特征（ECH）"这一点。
3. **outer SNI 选择判据 = 当下未被封 ∩ 真实 ECH 群体最大。** 最优解是 `cloudflare-ech.com` 这类大厂 public_name（Cloudflare 用单一 public_name 承载所有 ECH 站点，连带损伤最大）。**偏门小域名是反面**：零连带损伤 = 把干净判别式送给墙。
4. **先例存在**：GFW 2020 年已封过 ECH 前身 ESNI（丢弃带 `encrypted_server_name` 扩展的握手）。"墙不敢碰 ECH"只对**广封**成立，对**带 IP 条件的窄封**不成立——但当下大概率尚未上线，视作有限 runway。

---

## 3. 关键耦合（本轮新发现，已代码验证）

Decoy SNI 不是一个可以独立旋钮的东西。它和服务端的**反代伪装 / 开放中继防线 / 主动探测自洽性**是同一根绳上的三个结。验证链：

- `learnOuterSNI`（`server.go:726-767`）**只把 `s.camouflageName`（=public_name）** 加入 `trustedSNIs`，其它 outer SNI 一律 WARN 拒收（`server.go:730`）。这是**开放中继防线**：`trustedSNIs` 里的名字会让 `handleRawTCP` 把非 ECH 连接**裸 TCP 反代到 `<SNI>:443`**（`server.go:699`），这条路**不过设备认证、且不做 `isReservedIP` 检查**。若信任客户端自选 SNI，节点就成了打向任意主机（含内网）的开放中继。
- `handleRawTCP`（`server.go:673-706`）对非 ECH 连接：命中 `local_routes` → 反代；命中 `trustedSNIs` → 反代真站；**其它未知 SNI → `conn.Close()`**。
- 当前（焊死）设计因此**主动探测自洽**：被动侧只看到 `cloudflare-ech.com`；主动侧拿 `cloudflare-ech.com`+无 ECH 探测 → 命中 `trustedSNIs` → 反代到真 Cloudflare 站。观测与探测一致。

**推论：贸然解耦成多样 decoy 会破坏这个自洽性。** 客户端发 `apple.com` → `learnOuterSNI` 拒收（不进白名单）→ 审查者被动看到 `apple.com`+ECH，主动拿 `apple.com`+无 ECH 探测撞上"未知 SNI → RST"→ **"用 apple.com 跑 ECH 却不提供 apple.com 站"** 成为干净的主动探测判别式。ECH 隧道本身不受影响（服务端不校验 outer SNI，§1），坏的是伪装一致性。

---

## 4. 两条策略与诚实的取舍

### 策略 A —— 单一高连带 in-distribution decoy（现状，焊死 `cloudflare-ech.com`）

- 主动探测自洽 ✓（trusted SNI = public_name → 反代真站）。
- In-distribution ✓（"cloudflare-ech.com + ECH" 正是数百万真 Cloudflare 连接的样子）。
- **弱点**：单一固定稀有值 → `SNI==cloudflare-ech.com AND IP∉Cloudflare段` 是**零连带损伤**的干净封锁签名。这是残余威胁。
- 多样性：无。

### 策略 B —— 服务端白名单化的轮换 decoy

要安全地拿到 decoy 多样性，**必须同时**改三处，缺一不可：

1. **服务端 decoy 白名单**（config，**非**客户端自选）：节点配置 `{cloudflare-ech.com, decoy2, ...}`。
2. `learnOuterSNI` 收整个白名单（而非仅 camouflageName）；`handleRawTCP` 对白名单内 decoy 反代到 `<decoy>:443`，**保留 `isReservedIP`/`checkAddr`**（decoy 都是公网真站，安全）。
3. 客户端从**同一份**白名单轮换 outer SNI（随 config 下发），覆盖 emit 路径（§1）。

- 收益：打掉"单一 SNI 值"过滤——没有唯一值可 key。
- **代价一（in-distribution）**：要让 "SNI + ECH" 不异常，decoy 最好**真在跑 ECH**；而真部署 ECH 的域名高度聚集在 `cloudflare-ech.com`，可选池极小。用不部署 ECH 的大厂（apple.com）会让 "apple.com + ECH 扩展" 变成**野外不存在的组合**——用一个 tell 换另一个 tell。
- **代价二（安全面）**：白名单必须严格、`isReservedIP` 不能丢；decoy 成为反代目标，是一条 security-review 必查项。

**诚实判断（2026-08-10 修正,承认前稿夸大）**：`cloudflare-ech.com` 的"最大人群"护盾**只对'单看 outer SNI 值'的过滤器有效**。它对 `SNI==cloudflare-ech.com AND IP∉Cloudflare段` 的窄封**无效**——因为那几百万条真 Cloudflare ECH 流量全落在 **Cloudflare 的 IP** 上,被 IP 条件排除在规则之外;我们在**裸 VPS IP** 上,被单独框住。**护盾人群和我们的 IP 段不相交,人群再大也不在我们这个 (SNI, IP) 格子里。** 加上 (代价一) 真 ECH 池太小、策略 B 拿不到实质 SNI-值多样性,结论是:**残余威胁本质不在 SNI 层可解**——选哪个 outer SNI(含 cloudflare-ech.com)都不是护盾,只是"最不坏的默认"。

> 注意 outer/inner 之别:`cloudflare-ech.com` 作为 **inner** 值几乎不出现(它不是内容站);作为 **outer** 值极常见——它是每条 Cloudflare ECH 连接的明文 SNI(客户端从 profile 的 public_name 照抄)。"profile 用得广"和"outer 值 = cloudflare-ech.com"是同一件事。

---

## 5. 残余威胁真正可解的地方

不是 SNI 层，而是这两处：

1. **连带损伤威慑（用户下的注）**：赌墙不会为封我们去吃"封 `cloudflare-ech.com` = 误伤 Cloudflare"的成本。当下成立，但是 runway，不是永久护城河。→ 需要**领先指标监测**（见 §6）。
2. **不携带 ECH tell 的"无聊腿"（暂搁置、未否决）**：在无名 VPS IP 上，ECH 是信标不是护盾（ECH 今天几乎只出现在 CDN 上）。一条 **无 ECH + 明文中性/自洽 SNI + pin 验证** 的降级路径，才是对冲"ECH 被点名"的真正一手。注意：**当前两条传输都强制带 ECH**（`BuildClientTLSConfig` 无 ECH 直接 error，`tcpws.go:402`），所以一条 `ECH-presence` 规则**同时杀死 QUIC 和 TCP-WS 两条腿**——composite transport 的韧性对这一手完全无效。这是把 ECH 设成"强制且唯一"的结构性风险。

---

## 6. 领先指标（把假设从信仰改成可观测）

判据很干净：**两条腿同时在 TLS 握手层失败，而对同一 IP 发一个不带 ECH 的普通 TLS 握手却能成** → 墙开始按 `ECH-presence` 切。可加一个探针作为"runway 到期"告警。这不改协议行为，纯观测。

---

## 7. 建议与待决

- **建议**：`cloudflare-ech.com` 作为 outer SNI 默认**留着**——它是"未被封 ∩ in-distribution ∩ 主动探测自洽"的最不坏默认(但**不是护盾**,§4 诚实判断)。**不要**贸然做客户端单侧解耦成偏门/多样 decoy——那会破坏 §3 的主动探测自洽性、拿不到实质多样性、且对 SNI+IP 窄封无收益。真正的抗封投入在 §5.2 的无聊腿 + §6 的领先指标。
- **待用户拍板**：
  1. 是否投入策略 B（三处联改 + security review），接受其 in-distribution 上限？
  2. 是否解除对 §5.2 无聊腿的搁置——这是对"ECH 被窄封"唯一结构性对冲。
- **实现约束**：以上任何**协议/代码**改动（emit 覆盖、白名单反代、无 ECH 腿）都需独立 worktree + feature 分支 + 测试；本文仅设计与文档订正，不含代码改动。

---

## 8. 本次订正的文档（与本文一并提交）

| 文件 | 原断言（误） | 订正 |
|---|---|---|
| `wire/k2v5-protocol-spec.md:39,65` | outer SNI "forced by uTLS during the handshake" | 改为：当前客户端 emit 焊死，库层不强制（§1）；§3.1 补 decoy 选择判据 |
| `wire/k2v5-protocol-spec.md:579` | IP-SNI cross-checker 的缓解 = ReverseProxy | 标注：ReverseProxy 答的是**主动探测**，非 IP 归属；IP 归属不在 SNI 层可解（§5） |
| `CLAUDE.md:680`、`wire/CLAUDE.md:102` | "uTLS forces outer SNI = public_name — cannot be set independently" | 改为精确表述：库不强制 / 当前客户端焊死 / 可覆盖 |
| `server/CLAUDE.md:81` | 安全 pin "免费"因为"uTLS forces outer SNI = public_name" | 改为：pin 的安全**不依赖客户端行为**，而由 `learnOuterSNI` 服务端只收 camouflageName 强制（§3） |
| `docs/knowledge/architecture-decisions.md:517` | "Independent SNI control impossible" | 追加 dated 订正：库层可行，指向本文 §1/§4 |

---

## 9. 实现发现与修复：§3.7 "decrypt-fail → 反代" 从未落地(2026-08-10)

**发现(代码核准,10/10)**:spec §3.7 / §12.3 写着"ECH present but decryption fails (forged) → ReverseProxy",但代码没实现。`handleRawTCP` 按 ECH **存在性**(`PeekClientHello` 只认 0xfe0d 扩展存在)路由,任何 `HasECH` 连接进 `handleK2V5` → 无条件 `tls.Server(...).Handshake()`。ECH 解密失败时 utls 用 outer hello 继续(`ech.go:630` `return outer, nil, nil`),`getCertificate`(`server.go:430`)无条件返回自签证书 → **握手完成、暴露自签证书**。

**后果**:一个**不需要 config** 的主动探测去匿名——探测者(或 Chrome 默认发的 GREASE ECH)拼一个语法存在但解不开的 ECH 扩展,用自己的 key share 完成握手,读到自签证书,和真 cloudflare-ech.com 一比就露馅。这是维度①(自签证书暴露),最强信号。

**为什么不能"带外完整解密判定后反代"**:utls 的 HPKE 在 `internal/hpke`(Go internal,k2 不能 import);`ClientHelloInfo` 无 ECH-accepted 字段;回调也拿不到可靠的"是否已接受 ECH"信号。完整修法需 fork utls 或加新 HPKE 依赖 + 重实现 ECH AAD 构造(易错),对本项优先级不成比例。

**修复(config_id gate,proportionate)**:ECH 扩展里的 `config_id` 是**明文**(offset 5:`[type:1][kdf:2][aead:2][config_id:1]`,对着 utls `parseECHExt` 核准)。`echDisposition`/`ownsECHConfigID` 在**握手前**读它,不属于本节点活跃 config(读 `EncryptedClientHelloKeys[].Config` 的 offset 4)→ 反代 `camouflageName:443`。堵住 GREASE + 随机 config_id 探测(绝大多数),真客户端用本节点 config_id 不受影响,轮换期多 config 都在集合里。

**残留(记录,未修)**:config_id 明文在线上可被动观测,定向探测者抄一个真实 config_id 仍能进 handleK2V5、解密失败、暴露证书。完整堵 = 带外 HPKE 解密(需 fork/新依赖)。维度②(k2v5 行为 404/close)只在证书已暴露后够到,堵了①连带收窄②。

代码:`server/server.go`(`echDisposition`/`ownsECHConfigID`/`handleRawTCP`)、`wire/ech.go`(`PeekClientHello` 解析 config_id、`ECHConfigIDFromConfig`)。测试:`wire/ech_test.go`(`TestPeekClientHello_ECHConfigID`/`TestECHConfigIDFromConfig`)、`server/ech_config_id_gate_test.go`。分支:`feat/ech-camouflage-block-monitor`。
