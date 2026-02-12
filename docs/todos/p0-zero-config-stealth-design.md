# Zero-Config Stealth: 自动化证书、ECH 与协议伪装

Date: 2025-02-12
Updated: 2026-02-12 (brainstorm session 2)

## 目标

服务端 `k2s run` 一个命令启动，自动完成证书生成、ECH 配置、协议伪装。管理员从日志复制 URL 发给用户即可。客户端只需要一个 k2v5:// URL，无需理解底层细节。

## 设计决策记录

### 1. 自签名证书 + 公钥指纹验证

**选择**: 服务端两套自签名证书（RSA + EC），公钥 SHA-256 指纹嵌入 k2v5 URL。

**否决方案**:
- Let's Encrypt: Certificate Transparency 日志暴露域名与 IP 的关联。
- sslip.io + Let's Encrypt: 增加外部依赖，且 CT 暴露同样存在。
- 自签名 CA + CA 指纹: 多一层密钥管理，对 K2 的小规模分发场景是 YAGNI。
- 证书 DER 哈希 pin: 见决策 #3，证书大小因 padding 不同而变化，pin 必须基于公钥。

**理由**:
- TLS 1.3 中证书在加密通道内传输，被动观察者不可见。
- 主动探测者（无 ECH）被 ReverseProxy 引导到真实网站，不会看到 K2 的证书。
- 自签名证书可设 10 年有效期，无需续期。
- 私钥泄露时换服务器重新分发 URL 是必然的，证书轮换不是实际需求。

### 2. Outer SNI = ECH public_name（实际实现）

**选择**: outer SNI = ECH config 的 public_name（`cloudflare-ech.com`），由 uTLS 自动设置。不再有独立的 `sni` URL 参数。

**原始设计 vs 实际实现**:
- 原设计想通过 uTLS 独立控制 outer SNI，从 Cloudflare 域名池随机选择，但实际测试发现 uTLS 在 ECH 模式下强制将 outer SNI 设置为 ECH config 的 public_name。
- 这其实是正确行为: Cloudflare 的所有 ECH 站点都使用同一个 `public_name`（`cloudflare-ech.com`），所以合法 ECH 流量本身就共享同一个 outer SNI。SNI 集中度不是问题，因为审查者观测到的所有 Cloudflare ECH 流量都是同一个 outer SNI。

**结果**: `sni` URL 参数已完全移除。outer SNI 由 ECH config 的 public_name 决定，无需用户配置。

### 3. 双密钥对 + TLS Record Padding（做戏全套）

**选择**: 服务端持有两套固定密钥对（RSA-2048 + ECDSA P-256），根据 outer SNI 真实网站的证书算法选择对应密钥，通过 TLS record padding 匹配目标长度。

**核心洞察**: TLS 1.3 握手中 Certificate 和 CertificateVerify 是独立 TLS record，长度各自可观测：
- Certificate record: 自签名 ~500B vs 真实 CA 链 ~3.8KB → 需要 padding
- CertificateVerify record: ECDSA ~72B vs RSA ~256B → 必须匹配算法

**否决方案**:
- 单密钥对 + padding 吸收算法差异: CertificateVerify 签名长度由算法决定，无法通过 padding 伪造。
- 按 outer SNI 动态生成不同证书: 需要缓存管理，X.509 塞垃圾数据不够干净。pin 必须基于公钥。
- REALITY 中间人转发真实证书: CertificateVerify 需要证书私钥签名，标准 crypto/tls 不允许 key 与 cert 不匹配。需要完全自定义 TLS 实现，复杂度极高。

**实现路径**: TLS record padding（RFC 8446 §5.4）是协议标准能力，接收方天然支持。Go crypto/tls 未暴露 padding API，需要小幅 patch。详见任务 `p0-tls-record-padding.md`。

**理由**: 两张固定证书 + record padding = 最简架构。做戏全套：算法匹配 CertificateVerify 长度，padding 匹配 Certificate 长度。

### 4. 服务端 ECH 路由（在 TLS 握手前决策）

**流程**:
```
TCP 连接 → Peek ClientHello (原始字节)
  ├── 检测到 ECH 扩展 (0xfe0d)
  │     → TLS 握手 (含 ECH 解密) → k2v5 handler
  │        ├── HTTP → /k2v5/auth
  │        └── WebSocket → smux 隧道
  └── 未检测到 ECH
        → ReverseProxy 原始 TCP 到 outer SNI 真实站点
```

**关键**: ECH 解密失败（伪造 ECH）也必须走 ReverseProxy，不能暴露自签名证书。

**已实现**: wire-protocol-compliance feature (2026-02-12)。

### 5. 首次启动探测时机

**选择**: 仅首次启动时探测 outer SNI 目标站点，获取证书特征（算法、链长度）。

**否决方案**:
- 每次连接实时探测: 延迟太高，每个连接增加一次 TLS 握手。
- 周期性刷新: 增加复杂度，证书特征变化频率极低。

**理由**: 证书算法和链长度几乎不变（站点换 CA 或算法是罕见事件）。首次启动探测一次足够。如需刷新，重启服务即可。

## 协议伪装三层身份

```
层次          值                        可见性
──────────────────────────────────────────────────
TCP 目标 IP   1.2.3.4                  明文
Outer SNI     cloudflare-ech.com       明文（TLS ClientHello，= ECH public_name）
Inner SNI     服务端主机名               ECH 加密
TLS 证书      自签名（RSA 或 EC）        TLS 1.3 加密
CertVerify    匹配 public_name 算法     TLS 1.3 加密
证书验证      pin=sha256:<公钥哈希>      客户端本地（不上网）
```

## k2v5 URL 格式（修订 v2）

```
k2v5://UDID:TOKEN@HOST:PORT?ech=ECH_CONFIG&pin=PUBKEY_PINS&fp=FINGERPRINT&hop=START-END
```

| 参数 | 必需 | 默认 | 说明 |
|------|------|------|------|
| `ech` | 是 | - | URL-safe Base64 ECH config（含 public_name，决定 outer SNI） |
| `pin` | 是 | - | `sha256:<rsa公钥哈希>,sha256:<ec公钥哈希>` |
| `fp` | 否 | `chrome` | uTLS 指纹 |
| `hop` | 否 | 禁用 | UDP 端口跳跃范围 |

**变化**: `pin` 基于公钥哈希（两个值），`sni` 参数已移除（outer SNI = ECH config 的 public_name）。

## 服务端运行时流程

```
k2s run -s server.yaml

首次启动 (无证书文件):
  1. 生成两套密钥对: RSA-2048 + ECDSA P-256
     → server-rsa.pem/key + server-ec.pem/key
  2. 探测 ECH public_name:443
     → 证书链长度、密钥算法 → 计算 padding 量
  3. 生成 HPKE 密钥对 + ECH config
  4. 生成 auth token + UDID
  5. 日志输出完整 k2v5:// URL（含双 pin）
  6. 开始服务

后续启动 (证书已存在):
  1. 加载密钥对 + ECH config
  2. 重新探测 → 刷新 padding 量
  3. 开始服务

每个连接:
  1. Peek ClientHello → 提取 outer SNI
  2. 查缓存：该 outer SNI 的真实证书特征
     - 缓存未命中 → 后台探测 outer SNI:443，缓存结果
     - 缓存命中 → 继续
  3. 选择匹配算法的密钥对 + 对应 padding 量
  4. TLS 握手（带 record padding）
```

## 客户端验证流程

```go
tlsCfg := &tls.Config{
    ServerName:                     cfg.Host,       // inner SNI
    EncryptedClientHelloConfigList: echBytes,
    InsecureSkipVerify:             true,
    VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
        cert, err := x509.ParseCertificate(rawCerts[0])
        if err != nil { return err }
        pubKeyDER, err := x509.MarshalPKIXPublicKey(cert.PublicKey)
        if err != nil { return err }
        hash := sha256.Sum256(pubKeyDER)
        for _, pin := range cfg.Pins {
            if bytes.Equal(hash[:], pin) { return nil }
        }
        return errors.New("public key pin mismatch")
    },
}
```

客户端不依赖系统 CA、不依赖域名匹配，仅验证公钥 SHA-256 指纹。双 pin 允许服务端使用 RSA 或 EC 证书。

## 当前代码差距

| 组件 | 需要实现 | 当前状态 |
|------|---------|---------|
| ECH 扩展检测 | Peek ClientHello 时检测 0xfe0d | ✅ 已实现 (`PeekClientHello`) |
| 服务端 ECH 解密 | `tls.Config.EncryptedClientHelloKeys` | ✅ 已实现 |
| SNI 路由 | ECH → k2v5, 无 ECH → ReverseProxy | ✅ 已实现 |
| ECH config 生成 | 运行时自动生成 HPKE 密钥对 | ✅ 已实现 (`EnsureECHKey`) |
| 双密钥对生成 | RSA-2048 + ECDSA P-256 自签名证书 | ✅ 已实现 (`wire/certgen.go`) |
| TLS record padding | Go crypto/tls patch | ✅ 已实现 (`wire/tlspad`) |
| outer SNI 探测 | 连接目标站点获取证书特征 | ✅ 已实现 (`ProbeTarget`) |
| outer SNI 随机选择 | ~~内置 Cloudflare 域名池 + uTLS SNI 覆盖~~ | ❌ 不需要 (uTLS forces outer SNI = ECH public_name, matches Cloudflare behavior) |
| `pin` URL 参数 | 公钥哈希验证，双 pin 支持 | ✅ 已实现 (`ParsePins`) |
| `sni` URL 参数 | ~~改为可选覆盖~~ | ✅ 已移除 (outer SNI = ECH config's public_name) |
