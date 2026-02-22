---
title: k2 协议概述
date: 2026-02-22
summary: k2 是 Kaitu 自研隐身隧道协议，搭载 k2arc 自适应速率控制算法，在高丢包、高审查网络中自动最大化吞吐量，无需手动配置带宽。
section: getting-started
order: 1
draft: false
---

# k2 协议概述

k2 是 Kaitu 自研的隐身网络隧道协议，专为高审查和封锁网络环境设计。它搭载自研的 **k2arc（Adaptive Rate Control）自适应速率控制算法**，能在高丢包、高延迟的网络环境中自动寻找最优发送速率——无需手动配置带宽参数。

k2 以 **QUIC/HTTP3** 作为主传输协议，并在 QUIC 被封锁时自动切换到 **TCP-WebSocket** 回退，配合 ECH 加密 SNI 和 TLS 指纹伪装，使隧道流量与真实 HTTPS 浏览流量无法区分。

## 三大核心能力

### k2arc 自适应速率控制

k2arc 是 k2 的核心竞争力。与传统拥塞控制算法不同，k2arc 通过**梯度上升效用函数**自动探测网络最优发送速率：

| 能力 | k2arc（k2） | 传统方案（如 Brutal） |
|------|----------|-------------------|
| 带宽配置 | 全自动探测，零配置 | 需手动指定最大带宽 |
| 丢包响应 | 区分拥塞丢包与审查丢包 | 忽略所有丢包信号 |
| 延迟控制 | RTT 感知，抑制 bufferbloat | 固定速率，易造成队列堆积 |
| 网络适应 | 实时跟踪带宽变化 | 无动态探测能力 |
| 共存公平性 | 与其他流量和平共存 | 挤占其他连接带宽 |

k2arc 的核心创新在于**自适应丢包惩罚系数**：在高审查网络中，大量丢包来自防火墙主动丢弃而非网络拥塞。k2arc 能自动识别这类非拥塞性丢包，避免错误地降低发送速率，从而在 GFW 等高丢包环境中维持远高于传统算法的吞吐量。

详细技术原理见 [k2arc 算法详解](/k2/protocol)。性能对比见 [k2 vs Hysteria2](/k2/vs-hysteria2)。

### 隐身传输

k2 通过四层防御实现流量隐身：

- **ECH（Encrypted Client Hello）**：将真实目标域名加密隐藏在 TLS 握手中，DPI 只能看到某个主流 CDN 的公共域名
- **TLS 指纹伪装**：通过 uTLS 模拟 Chrome/Firefox/Safari 的 TLS 握手特征
- **流量特征匹配**：TLS 记录填充长度与真实 Cloudflare 服务器完全一致
- **主动探测对抗**：非 ECH 连接被透明转发到真实网站，探测者无法识别

详细原理见 [隐身伪装技术](/k2/stealth)。

### 零配置部署

服务端一条命令启动，自动生成所有密钥和证书，打印即用的连接 URL。客户端一条命令连接，k2arc 自动探测最优速率——整个过程无需任何手动配置。

```bash
# 服务端（30 秒）
curl -fsSL https://kaitu.io/install.sh | sudo sh -s k2s
sudo k2s run

# 客户端（30 秒）
curl -fsSL https://kaitu.io/install.sh | sudo sh -s k2
sudo k2 up k2v5://abc123:tok456@203.0.113.5:443?ech=AEX0...&pin=sha256:...
```

## 传输层特性

- **QUIC/H3 主传输**：原生多路复用，无队头阻塞，搭载 k2arc 算法在高丢包网络中维持高吞吐
- **TCP-WebSocket 回退**：QUIC 被封锁时自动切换，通过 smux 实现连接复用
- **单端口 :443**：QUIC 和 TCP 共享同一端口，减少暴露面
- **UDP 端口跳跃**：支持 `hop=START-END` 参数，对抗 UDP QoS 限速

## 身份与认证

- **k2v5 URL**：所有配置通过单个 URL 传递，格式为 `k2v5://UDID:TOKEN@HOST:PORT?ech=...&pin=...`
- **三层身份**：TCP 目标 IP（明文）→ 外层 SNI（明文，CDN 公共域名）→ 内层 SNI（ECH 加密）
- **服务端零配置**：首次启动自动生成所有密钥和证书，打印即用的连接 URL

## 快速导航

| 文档 | 说明 |
|------|------|
| [1 分钟快速开始](/k2/quickstart) | 一分钟内启动服务端并连接 |
| [k2s 服务端部署](/k2/server) | 详细的服务端安装与配置 |
| [k2 客户端使用](/k2/client) | 客户端安装与常用命令 |
| [拥塞控制算法 k2arc](/k2/protocol) | k2arc 算法原理、效用函数、自适应丢包惩罚 |
| [隐身伪装技术](/k2/stealth) | ECH、TLS 指纹、主动探测对抗 |
| [k2 vs Hysteria2](/k2/vs-hysteria2) | k2arc 与 Brutal/BBR 拥塞控制对比与测评 |
| [k2 vs VLESS+Reality](/k2/vs-reality) | 隐身路线与抗封锁能力对比 |

## 支持平台

k2 命令行客户端支持 **Linux** 和 **macOS**。Kaitu 桌面客户端（macOS/Windows）和移动客户端（iOS/Android）已内置 k2 协议，无需单独安装。

前往[下载页面](/install)获取 Kaitu 客户端。
