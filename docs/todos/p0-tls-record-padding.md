# P0: TLS Record Padding — Go crypto/tls Patch 调研与实现

前置设计: `docs/todos/p0-zero-config-stealth-design.md` 决策 #3

## 背景

TLS 1.3 握手中，Certificate 和 CertificateVerify 是独立的 TLS record，加密后的长度可被审查者观测：

```
[Record: EncryptedExtensions]    ~50B   — 固定
[Record: Certificate]            ~500B  — 自签名 vs ~3.8KB 真实 CA 链 ⚠️
[Record: CertificateVerify]      ~72B   — ECDSA vs ~256B RSA ⚠️
[Record: Finished]               ~50B   — 固定
```

RFC 8446 Section 5.4 定义了标准的 record padding 机制：

```
TLSInnerPlaintext:
  [content][ContentType 1B][0x00 ... padding]
  └────────── AEAD 加密 ─────────────────────┘
```

接收方解密后从尾部向前跳过零字节找到 ContentType，自动剥离 padding。
这是协议标准行为，任何合规 TLS 1.3 实现（包括 uTLS）都能正确接收。

## 问题

Go crypto/tls 的 `halfConn.encrypt()` 不添加 padding：

```go
// crypto/tls/conn.go — encrypt() 简化：
record = append(record, payload...)
record = append(record, byte(typ))     // ContentType
// ← 这里没有 padding
encrypted = hc.aead.Seal(...)
```

需要在 ContentType 后、AEAD Seal 前插入零字节填充。

## 调研任务

### T1: Go crypto/tls 源码分析

- [ ] 定位 Go 1.25 中 `halfConn.encrypt()` 的精确代码路径
- [ ] 确认 TLS 1.3 handshake record 的 encrypt 调用链：
      `writeHandshakeRecord → writeRecordLocked → encrypt`
- [ ] 确认 `c.buffering = true` 阶段的完整 record 序列
- [ ] 确认是否有 unexported hook 点可以注入 padding（避免 fork 整个包）

### T2: Patch 方案设计

三个候选方案，需评估可行性和维护成本：

**方案 A: 最小 monkey-patch**
- 用 `//go:linkname` 或 build tag 覆盖 `halfConn.encrypt`
- 优点: 不 fork 整个 crypto/tls
- 风险: Go 版本升级可能 break linkname

**方案 B: Fork crypto/tls 的 conn.go**
- 复制 `conn.go` 到项目内，修改 `encrypt()` 添加 padding
- 优点: 完全控制
- 缺点: 维护成本，需跟踪 Go 版本

**方案 C: 在 tls.Conn 外层包装**
- 实现自定义 `net.Conn` wrapper，在 `Write()` 中拦截 TLS record
- 解密 record header → 判断是否为 handshake → 重新加密带 padding
- 优点: 不修改 crypto/tls
- 缺点: 无法实现（AEAD 密钥在 tls.Conn 内部，外部无法重新加密）

**方案 D: 利用 uTLS 服务端能力**
- metacubex/utls 已 fork crypto/tls，检查是否可以在服务端使用
- 如果可以，直接在 utls fork 中添加 padding 支持
- 优点: 已有 fork，维护成本共担
- 需要确认 utls 是否支持服务端 TLS（不仅仅是客户端）

### T3: Padding 量计算

- [ ] 实现 `ProbeTarget(host string) → (keyAlgo, certChainLen, certVerifyLen)`
- [ ] 连接目标站点 :443，完成 TLS 握手，记录：
  - 叶子证书密钥算法（RSA/ECDSA/Ed25519）
  - Certificate 消息总长度（整个证书链 DER 编码）
  - CertificateVerify 签名长度
- [ ] 缓存结果到 `map[string]TargetProfile`
- [ ] padding 量 = 目标 Certificate 长度 - 我方 Certificate 长度

### T4: 双密钥对集成

- [ ] 服务端生成并持久化 RSA-2048 + ECDSA P-256 两套密钥对
- [ ] 每个连接根据 outer SNI 对应的目标算法选择密钥对
- [ ] 需要 per-connection 动态选择 `tls.Certificate`（通过 `tls.Config.GetCertificate` 回调）
- [ ] 注意：GetCertificate 的 ClientHelloInfo 在 ECH 场景下提供的是 outer SNI 还是 inner SNI？需要验证。

### T5: 端到端验证

- [ ] 用 Wireshark 抓包对比：k2s 服务端 vs 真实 Cloudflare 站点
- [ ] 确认 Certificate record 长度匹配（误差 < 16 字节）
- [ ] 确认 CertificateVerify record 长度匹配
- [ ] 确认客户端（uTLS）能正确剥离 padding、完成握手
- [ ] 确认 pin 验证（双公钥哈希）正常工作

## 约束

- 不改变 k2v5 的 ECH + 自签名证书架构
- 客户端零改动（TLS record padding 是标准行为）
- 服务端 patch 尽可能小，易于跟踪 Go 版本升级
- 两套固定密钥对，不按 outer SNI 动态生成证书
