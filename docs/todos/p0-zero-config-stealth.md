# P0: Zero-Config Stealth

Server auto-provisioning and protocol camouflage.

- Design: `docs/todos/p0-zero-config-stealth-design.md`
- Spec: `wire/k2v5-protocol-spec.md` Section 3 and 12

## 已完成

- [x] ECH extension detection in ClientHello peek (`wire/ech.go`)
- [x] Server-side ECH decryption (`tls.Config.EncryptedClientHelloKeys`)
- [x] HPKE key pair generation + ECH config construction (`EnsureECHKey`)
- [x] ECH routing: ECH → k2v5, no ECH → ReverseProxy (`server/server.go`)
- [x] 双密钥对生成: RSA-2048 + ECDSA P-256 (`wire/certgen.go`)
- [x] TLS record padding: `wire/tlspad` package
- [x] Outer SNI 探测: `ProbeTarget` (`wire/certgen.go`)
- [x] Per-connection 证书选择: `GetCertificate` callback (`server/server.go`)
- [x] 探测结果缓存
- [x] `pin` URL 参数解析: `ParsePins` (`wire/certgen.go`)
- [x] `VerifyPeerCertificate` 回调: 公钥哈希验证 (`wire/ech.go`)
- [x] `BuildClientTLSConfig` 集成 pin 验证

## 待实现

### Outer SNI 分散

- [ ] 内置 Cloudflare 域名池（支持 ECH 的域名列表）
- [ ] 客户端每连接随机选择 outer SNI（uTLS SNIExtension 覆盖）
- [ ] `sni` URL 参数改为可选覆盖（当前语义: 默认 www.microsoft.com → 改为: 默认随机）

### 服务端一键启动

- [ ] 首次启动自动生成: 双密钥对 + ECH config + auth token
- [ ] 日志输出完整 k2v5:// URL（含双 pin、ECH config）
- [ ] Persist to disk, reload on restart
