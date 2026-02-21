---
title: k2 协议概述
date: 2026-02-21
summary: k2 是 Kaitu 自研的隐身网络隧道协议，专为高审查环境设计。QUIC+H3 主传输、TCP-WebSocket 回退、ECH 隐身、TLS 指纹伪装、证书固定。
section: getting-started
order: 1
draft: false
---

# k2 协议概述

k2 是 Kaitu 自研的隐身网络隧道协议，专为高审查和封锁网络环境设计。它以 **QUIC/HTTP3** 作为主传输协议，并在 QUIC 被封锁时自动切换到 **TCP-WebSocket** 回退。

## 核心特性

### 隐身传输

- **ECH（Encrypted Client Hello）**：真实的目标域名（SNI）被加密隐藏在 TLS 握手的 ECH 扩展中，DPI 设备只能看到公共域名 `cloudflare-ech.com`
- **TLS 指纹伪装**：通过 uTLS 库模拟真实 Chrome 浏览器的 TLS 握手特征，使流量与真实 HTTPS 浏览流量无法区分
- **证书自签名 + 固定**：服务端使用自签名证书，客户端通过 SHA-256 哈希固定证书，无需 CA 信任链，也不会在 Certificate Transparency 日志中留痕
- **主动探测对抗**：对不携带 ECH 的连接，服务端将其透明转发到真实网站，探测者只能看到合法 HTTPS 响应

### 传输层

- **QUIC/H3 主传输**：QUIC 原生多路复用，延迟低，弱网表现优秀
- **TCP-WebSocket 回退**：当 QUIC 被封锁时自动切换，通过 smux 实现连接复用
- **单端口 :443**：QUIC 和 TCP 共享同一端口，减少暴露面
- **UDP 端口跳跃**：支持 `hop=START-END` 参数在端口范围内随机跳跃，对抗 UDP QoS 限速
- **自研自适应拥塞控制算法**：针对高丢包率网络（如移动数据、跨国专线）优化吞吐量

### 身份与认证

- **k2v5 URL**：所有配置通过单个 URL 传递，格式为 `k2v5://UDID:TOKEN@HOST:PORT?ech=...&pin=...`
- **三层身份**：TCP 目标 IP（明文）→ 外层 SNI（明文，`cloudflare-ech.com`）→ 内层 SNI（ECH 加密）
- **服务端零配置**：首次启动自动生成所有密钥和证书，打印即用的连接 URL

## 快速导航

| 文档 | 说明 |
|------|------|
| [1 分钟快速开始](/k2/quickstart) | 一分钟内启动服务端并连接 |
| [k2s 服务端部署](/k2/server) | 详细的服务端安装与配置 |
| [k2 客户端使用](/k2/client) | 客户端安装与常用命令 |
| [协议技术详解](/k2/protocol) | 深入了解 k2v5 协议实现 |
| [隐身伪装技术](/k2/stealth) | ECH、TLS 指纹、主动探测对抗原理 |

## 支持平台

k2 命令行客户端支持 **Linux** 和 **macOS**。Kaitu 桌面客户端（macOS/Windows）和移动客户端（iOS/Android）已内置 k2 协议，无需单独安装。

前往[下载页面](/install)获取 Kaitu 客户端。
