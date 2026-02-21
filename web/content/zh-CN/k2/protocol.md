---
title: 协议技术详解
date: 2026-02-21
summary: k2v5 协议的技术实现：三层身份体系、ECH 配置伪造、证书固定、QUIC 与 TCP 传输层、自研自适应拥塞控制。
section: technical
order: 5
draft: false
---

# 协议技术详解

本文面向希望深入理解 k2v5 协议工作原理的读者。如果您只需要快速使用，请参阅 [1 分钟快速开始](quickstart)。

## k2v5 URL 格式

k2v5 将所有连接参数编码到单个 URL 中：

```
k2v5://UDID:TOKEN@HOST:PORT?ech=ECH_CONFIG&pin=sha256:CERT_HASH&fp=FINGERPRINT&hop=PORT_RANGE
```

| 参数 | 说明 | 示例 |
|------|------|------|
| `UDID` | 设备标识符（服务端用于限制设备数量） | `abc123` |
| `TOKEN` | 认证令牌 | `tok456` |
| `HOST` | 服务端 IP 或域名 | `203.0.113.5` |
| `PORT` | 服务端端口（通常 443） | `443` |
| `ech` | Base64 编码的 ECH 配置 | `AEX0...` |
| `pin` | 服务端证书的 SHA-256 哈希 | `sha256:abc...` |
| `fp` | TLS 指纹类型（chrome/firefox/safari/random） | `chrome` |
| `hop` | UDP 端口跳跃范围（可选） | `10000-20000` |

## 三层身份体系

k2v5 连接过程中存在三层可观测的身份信息，每一层的可见性不同：

```
层级         明文可见  内容
─────────────────────────────────────────────────────────
1. TCP 目标  是        服务端真实 IP 地址
2. 外层 SNI  是        cloudflare-ech.com（ECH public_name）
3. 内层 SNI  否        k2 服务端域名（被 ECH 加密）
```

网络旁观者（ISP、GFW）只能看到第 1 层和第 2 层。第 3 层被 ECH 完整加密，无法在不持有 ECH 私钥的情况下解密。

## ECH 配置伪造

ECH（Encrypted Client Hello）是 k2v5 隐身的核心机制。k2s 生成的 ECH 配置并非随机创建，而是**从真实 Cloudflare ECH 配置派生**：

1. 查询 `cloudflare-ech.com` 的 DNS HTTPS 记录，获取当前 Cloudflare ECH 配置模板
2. 复制 `cipher_suites`、`kem_id`、`public_name` 等字段
3. 递增 `config_id`（避免与真实 Cloudflare 配置冲突）
4. 替换 HPKE 公钥为 k2s 自己的公钥

结果：k2 流量的 ECH 配置与真实 Cloudflare 流量在结构上无法区分。

## 证书与固定

k2s 使用**自签名证书**，不依赖任何 CA。

**双证书设计**：
- EC（椭圆曲线）证书：用于算法多样性伪装
- RSA 证书：兼容仅支持 RSA 的 TLS 客户端

**证书固定（Certificate Pinning）**：
- 连接 URL 中的 `pin=sha256:HASH` 是证书公钥的 SHA-256 哈希
- 客户端验证时跳过 CA 链检查，直接比对证书哈希
- 自签名证书不会出现在 Certificate Transparency（CT）日志中，避免被通过 CT 日志进行检测

## TLS 记录填充

k2s 定期（每 24 小时）从 `cloudflare-ech.com` 下载真实证书链，测量其 TLS Record 大小分布，并用相同的填充长度发送 TLS 握手记录。这使得 k2 握手的流量特征（数据包大小分布）与真实 Cloudflare HTTPS 流量匹配。

## 传输层

### QUIC/H3（主传输）

- 基于 QUIC 的 HTTP/3 传输
- 原生多路复用，单个 QUIC 连接上承载多个并发流
- 无队头阻塞，单个包丢失不影响其他流
- 使用自研自适应拥塞控制算法，在高丢包率网络（跨境链路、移动数据）下维持高吞吐

### TCP-WebSocket（回退传输）

- 当 QUIC 被 UDP 封锁时自动切换
- 使用 smux 在单个 WebSocket 连接上实现多路复用
- 切换过程对应用层透明，无需用户干预

### TransportManager

k2 内部的 `TransportManager` 组件封装了统一的 `Dialer` 接口，实现：

1. 优先使用 QUIC 建立连接
2. QUIC 失败后自动回退到 TCP-WebSocket
3. 连接状态监控与自动重连

## UDP 端口跳跃

当 URL 包含 `hop=START-END` 参数时，k2 客户端在 QUIC 传输中随机选择端口范围内的 UDP 端口，定期更换，对抗基于固定端口的 UDP QoS 限速或封锁。

```
# 示例：在 10000-20000 端口范围内随机跳跃
k2v5://...@203.0.113.5:443?hop=10000-20000&...
```

## 服务端 ECH 路由

k2s 服务端在接收 TLS 连接时会检查 ClientHello：

- **有 ECH 扩展**：解密 ECH，验证身份，进入 k2v5 隧道处理逻辑
- **无 ECH 扩展**：将原始 TCP 连接透明转发到 `public_name`（`cloudflare-ech.com`）对应的真实服务器

这意味着向 k2s 发起的非 ECH 连接会看到真实的 Cloudflare 响应，探测脚本无法区分 k2s 与真实 Cloudflare 服务器。

---

接下来阅读：[隐身伪装技术](stealth) 从威胁模型角度分析 k2 的对抗能力。
