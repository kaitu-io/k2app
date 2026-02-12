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

## 已完成 (T1-T5)

### Outer SNI — N/A (设计变更)

- [x] ~~内置 Cloudflare 域名池~~ → N/A: uTLS forces outer SNI = ECH config's public_name, no pool needed
- [x] ~~客户端每连接随机选择 outer SNI~~ → N/A: uTLS binds outer SNI to ECH public_name (`cloudflare-ech.com`)
- [x] ~~`sni` URL 参数改为可选覆盖~~ → ✅ Removed entirely: outer SNI = ECH config's public_name, forced by uTLS

### 服务端一键启动

- [x] 首次启动自动生成: 双密钥对 + ECH config + auth token
- [x] 日志输出完整 k2v5:// URL（含双 pin、ECH config）
- [x] Persist to disk, reload on restart
