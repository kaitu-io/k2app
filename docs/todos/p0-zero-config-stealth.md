# P0: Zero-Config Stealth

Server auto-provisioning and protocol camouflage.

- Design: `docs/todos/p0-zero-config-stealth-design.md`
- Spec: `wire/k2v5-protocol-spec.md` Section 3 and 12

## 已完成 (wire-protocol-compliance feature)

- [x] ECH extension detection in ClientHello peek (`wire/ech.go` — `PeekClientHello`)
- [x] Server-side ECH decryption (`tls.Config.EncryptedClientHelloKeys`)
- [x] HPKE key pair generation + ECH config construction (`EnsureECHKey`)
- [x] ECH routing: ECH present → k2v5, no ECH → ReverseProxy (`server/server.go`)

## 待实现

### 证书与 TLS 伪装

- [ ] 双密钥对生成: RSA-2048 + ECDSA P-256 自签名证书，持久化到磁盘
- [ ] TLS record padding: Go crypto/tls patch（详见 `docs/todos/p0-tls-record-padding.md`）
- [ ] Outer SNI 探测: 连接目标站点 :443 获取证书算法和链长度
- [ ] Per-connection 证书选择: 根据 outer SNI 目标算法选 RSA/EC 密钥对
- [ ] 探测结果缓存 + 首次启动冷启动

### 客户端 Pin 验证

- [ ] `pin` URL 参数解析: `sha256:<rsa哈希>,sha256:<ec哈希>` 双 pin 格式
- [ ] `VerifyPeerCertificate` 回调: `InsecureSkipVerify=true` + 公钥哈希验证
- [ ] `BuildClientTLSConfig` 集成 pin 验证

### Outer SNI 分散

- [ ] 内置 Cloudflare 域名池（支持 ECH 的域名列表）
- [ ] 客户端每连接随机选择 outer SNI（uTLS SNIExtension 覆盖）
- [ ] `sni` URL 参数改为可选覆盖（当前语义: 默认 www.microsoft.com → 改为: 默认随机）

### 服务端一键启动

- [ ] 首次启动自动生成: 双密钥对 + ECH config + auth token
- [ ] 日志输出完整 k2v5:// URL（含双 pin、ECH config）
- [ ] Persist to disk, reload on restart
