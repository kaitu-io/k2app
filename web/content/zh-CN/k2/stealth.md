---
title: 隐身伪装技术
date: 2026-02-22
summary: k2 的隐身机制：ECH 加密 SNI、TLS 指纹伪装、流量特征隐藏、主动探测对抗，以及如何在隐身传输中维持高吞吐量。
section: technical
order: 6
draft: false
---

# 隐身伪装技术

k2 的设计出发点是：即使在深度包检测（DPI）和主动探测普遍部署的网络环境中，连接流量也应与普通 HTTPS 浏览流量无法区分。同时，隐身不应以牺牲性能为代价——[k2arc](/k2/protocol) 确保在隐身传输中依然维持最优吞吐量。

本文从威胁模型出发，说明 k2 如何对抗各类检测手段。

## 威胁模型

k2 针对以下四类对手：

| 对手 | 能力 | k2 的对策 |
|------|------|-----------|
| **被动旁观者**（ISP、运营商）| 记录所有流量的 IP、端口、数据包大小 | ECH 隐藏 SNI；流量特征与 Cloudflare HTTPS 相同 |
| **主动探测者** | 向可疑 IP 发送 TLS 握手，检查响应 | 非 ECH 连接透明代理到真实网站 |
| **IP-SNI 交叉检查** | 将连接的 IP 与 SNI 进行关联分析 | 外层 SNI 为某主流 CDN 公共域名，IP 为普通 VPS |
| **CT 日志扫描** | 扫描 Certificate Transparency 日志寻找可疑证书 | 自签名证书，不向任何 CA 申请，不留 CT 记录 |

## ECH：加密 SNI

### 为什么 SNI 很关键

传统 TLS 握手中，ClientHello 消息以明文发送，其中包含 SNI（Server Name Indication）字段，明文指示客户端想要连接的域名。防火长城可以直接读取 SNI 进行过滤，不需要解密后续流量。

### ECH 的工作原理

ECH（Encrypted Client Hello）将 ClientHello 分为两层：

- **外层 ClientHello**（明文）：包含虚假的公共 SNI（某主流 CDN 域名），使用该 CDN 的真实 ECH 配置格式
- **内层 ClientHello**（ECH 加密）：包含真实的目标域名，只有持有 ECH 私钥的服务端才能解密

```
TLS 握手中 DPI 看到的内容：
  外层 SNI = [某主流 CDN 域名]   ← 公开可见，与真实 CDN 流量一致
  ECH 扩展 = [加密数据]          ← 真实 SNI 隐藏于此
```

k2s 生成的 ECH 配置的 `cipher_suites`、`kem_id` 和 `public_name` 字段均从某主流 CDN 的真实 ECH 记录派生，使流量在结构上与访问该 CDN 的真实 ECH 流量完全相同。

## TLS 指纹伪装——浏览器级隐身

TLS 指纹是当前最有效的代理识别技术之一。审查系统通过分析 TLS ClientHello 消息中数十个字段的精确组合来判断客户端身份——这比检查 SNI 更难规避。

### 为什么 TLS 指纹如此关键

每个 TLS 实现都有独特的"指纹"：ClientHello 中的 cipher suites 列表、扩展顺序、椭圆曲线参数、签名算法等字段组合在一起，形成了一个几乎唯一的标识。

| TLS 实现 | 指纹特征 | 识别难度 |
|----------|---------|---------|
| **Go crypto/tls** | 独特的 cipher suite 顺序和扩展组合 | 极易识别 |
| **OpenSSL** | 与系统版本关联的特征 | 可识别 |
| **真实 Chrome** | 数十亿用户的 ClientHello 模式 | 无法封锁 |
| **真实 Firefox** | 独特但用户基数大 | 无法封锁 |

绝大多数代理工具（包括 Shadowsocks、V2Ray、Trojan）使用 Go 标准库或 OpenSSL 建立 TLS 连接，其指纹与任何主流浏览器都不匹配。审查系统只需将"非浏览器指纹的 HTTPS 连接"标记为可疑，就能高效发现代理流量。

### k2 的浏览器指纹模拟

k2 通过 **uTLS** 库在字节级别精确复制真实浏览器的 ClientHello 消息，包括：

- **Cipher suites**：与目标浏览器完全一致的密码套件列表和顺序
- **扩展列表**：包括 ALPN、supported_versions、key_share 等扩展的精确排列
- **椭圆曲线与点格式**：匹配浏览器支持的曲线参数
- **签名算法**：复制浏览器的签名算法偏好顺序
- **GREASE 值**：模拟 Chrome 的 GREASE（Generate Random Extensions And Sustain Extensibility）随机填充

结果：k2 的 TLS 握手在 DPI 看来与真实浏览器访问 HTTPS 网站完全相同。JA3/JA4 指纹分析工具无法将 k2 流量与真实浏览器流量区分开。

### 指纹选择

```bash
# Chrome 指纹（默认，全球市场份额 65%+）
sudo k2 up k2v5://...?fp=chrome

# Firefox 指纹（适合需要差异化的场景）
sudo k2 up k2v5://...?fp=firefox

# Safari 指纹（适合 macOS/iOS 环境）
sudo k2 up k2v5://...?fp=safari

# 随机轮换（每次连接随机选择一种浏览器指纹）
sudo k2 up k2v5://...?fp=random
```

默认使用 Chrome 指纹，因为 Chrome 拥有全球最大的用户基数，其 TLS 指纹是互联网上最常见的 ClientHello 模式——审查系统无法在不影响正常网页浏览的情况下封锁这一指纹。

## 流量特征隐藏

即使 SNI 和指纹都匹配，流量的**数据包大小分布**也可以暴露代理软件。

k2s 定期从 ECH 伪装目标域名下载真实证书链，测量其 TLS Record 大小，并使用相同的填充长度发送 k2 握手记录。效果是：k2 的 TLS 握手阶段与真实 CDN 服务器的握手在流量特征上无法区分。

此外，k2s 使用 **RSA + EC 双证书**：
- 某些检测方案会对 EC-only 证书的 VPS 产生怀疑
- 双证书组合与真实 CDN 行为一致

## 主动探测对抗

主动探测是指审查者向可疑服务器主动发送连接请求，观察响应来判断是否为代理服务器。

k2s 的应对策略：

1. 检查每个传入 TLS 连接的 ClientHello
2. 如果 ClientHello **包含 ECH 扩展**：解密内层 ClientHello，进入 k2v5 隧道处理
3. 如果 ClientHello **不包含 ECH 扩展**：将原始 TCP 连接透明转发到 `public_name` 对应的真实 CDN 服务器

探测者向 k2s 发出的非 ECH 连接将收到来自真实 CDN 服务器的合法 HTTPS 响应，无法区分 k2s 与真实 CDN 服务器。

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

## 隐身与性能的统一

隐身伪装解决的是"能不能连上"的问题，而 k2arc 解决的是"连上后跑多快"的问题。两者协同工作：

- **隐身层**确保连接不被识别和封锁
- **k2arc**在隐身传输通道内自动最大化吞吐量，即使面对审查引发的高丢包也能维持高速

这意味着 k2 用户不需要在安全性和性能之间做取舍——ECH + TLS 伪装确保连接隐蔽，k2arc 确保连接高效。

---

如需了解 k2arc 的详细介绍，请参阅 [协议技术详解](/k2/protocol)。如需了解 k2arc 与其他拥塞控制策略的性能对比，请参阅 [k2 vs Hysteria2](/k2/vs-hysteria2)。
