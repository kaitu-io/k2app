---
title: 隐身伪装技术
date: 2026-02-21
summary: k2 的隐身机制：ECH 加密 SNI、TLS 指纹伪装、流量特征隐藏、主动探测对抗，以及它们所针对的威胁模型。
section: technical
order: 6
draft: false
---

# 隐身伪装技术

k2 的设计出发点是：即使在深度包检测（DPI）和主动探测普遍部署的网络环境中，连接流量也应与普通 HTTPS 浏览流量无法区分。本文从威胁模型出发，说明 k2 如何对抗各类检测手段。

## 威胁模型

k2 针对以下四类对手：

| 对手 | 能力 | k2 的对策 |
|------|------|-----------|
| **被动旁观者**（ISP、运营商）| 记录所有流量的 IP、端口、数据包大小 | ECH 隐藏 SNI；流量特征与 Cloudflare HTTPS 相同 |
| **主动探测者** | 向可疑 IP 发送 TLS 握手，检查响应 | 非 ECH 连接透明代理到真实网站 |
| **IP-SNI 交叉检查** | 将连接的 IP 与 SNI 进行关联分析 | 外层 SNI 始终为 `cloudflare-ech.com`，IP 为普通 VPS |
| **CT 日志扫描** | 扫描 Certificate Transparency 日志寻找可疑证书 | 自签名证书，不向任何 CA 申请，不留 CT 记录 |

## ECH：加密 SNI

### 为什么 SNI 很关键

传统 TLS 握手中，ClientHello 消息以明文发送，其中包含 SNI（Server Name Indication）字段，明文指示客户端想要连接的域名。防火长城可以直接读取 SNI 进行过滤，不需要解密后续流量。

### ECH 的工作原理

ECH（Encrypted Client Hello）将 ClientHello 分为两层：

- **外层 ClientHello**（明文）：包含虚假的公共 SNI（`cloudflare-ech.com`），使用 Cloudflare 的 ECH 配置格式
- **内层 ClientHello**（ECH 加密）：包含真实的目标域名，只有持有 ECH 私钥的服务端才能解密

```
TLS 握手中 DPI 看到的内容：
  外层 SNI = cloudflare-ech.com  ← 公开可见
  ECH 扩展 = [加密数据]          ← 真实 SNI 隐藏于此
```

k2s 生成的 ECH 配置的 `cipher_suites`、`kem_id` 和 `public_name` 字段均从真实 Cloudflare ECH 记录派生，使流量在结构上与访问 Cloudflare 的真实 ECH 流量完全相同。

## TLS 指纹伪装

TLS 指纹是通过分析 ClientHello 中的字段组合（支持的 cipher suites、扩展列表、椭圆曲线等）来识别客户端软件的技术。许多代理工具因为使用 Go 标准库的 `crypto/tls` 而拥有独特的指纹，可被轻易识别。

k2 通过 **uTLS** 库模拟真实浏览器的 TLS 指纹：

```bash
# 使用 Chrome 指纹（默认）
sudo k2 up k2v5://...?fp=chrome

# 使用 Firefox 指纹
sudo k2 up k2v5://...?fp=firefox

# 使用 Safari 指纹
sudo k2 up k2v5://...?fp=safari

# 随机轮换指纹
sudo k2 up k2v5://...?fp=random
```

## 流量特征隐藏

即使 SNI 和指纹都匹配，流量的**数据包大小分布**也可以暴露代理软件。

k2s 定期从 `cloudflare-ech.com` 下载真实证书链，测量其 TLS Record 大小，并使用相同的填充长度发送 k2 握手记录。效果是：k2 的 TLS 握手阶段与真实 Cloudflare 服务器的握手在流量特征上无法区分。

此外，k2s 使用 **RSA + EC 双证书**：
- 某些检测方案会对 EC-only 证书的 VPS 产生怀疑
- 双证书组合与真实 CDN 行为一致

## 主动探测对抗

主动探测是指审查者向可疑服务器主动发送连接请求，观察响应来判断是否为代理服务器。

k2s 的应对策略：

1. 检查每个传入 TLS 连接的 ClientHello
2. 如果 ClientHello **包含 ECH 扩展**：解密内层 ClientHello，进入 k2v5 隧道处理
3. 如果 ClientHello **不包含 ECH 扩展**：将原始 TCP 连接透明转发到 `public_name` 对应的真实服务器（`cloudflare-ech.com` 实际 IP）

探测者向 k2s 发出的非 ECH 连接将收到来自真实 Cloudflare 服务器的合法 HTTPS 响应，无法区分 k2s 与 Cloudflare 服务器。

## 证书固定与 CT 日志规避

传统 VPN 和代理工具通常需要向 CA 申请 TLS 证书。CA 签发的证书会被记录在 Certificate Transparency（CT）日志中，这些公开日志可以被自动扫描，用于发现和封锁代理服务器。

k2 的做法：

- 使用**自签名证书**，完全不经过任何 CA
- 客户端通过 URL 中的 `pin=sha256:HASH` 直接信任特定证书，不依赖 CA 信任链
- 自签名证书不会提交到任何 CT 日志，不留下可被扫描的公开记录

## UDP 端口跳跃

部分网络环境会对特定 UDP 端口进行 QoS 限速或彻底封锁。k2 支持在指定端口范围内随机跳跃：

```
k2v5://...@server:443?hop=10000-20000&...
```

客户端定期更换 UDP 端口，使基于固定端口规则的 QoS 策略失效。

---

如需了解这些机制的底层实现细节，请参阅 [协议技术详解](protocol)。
