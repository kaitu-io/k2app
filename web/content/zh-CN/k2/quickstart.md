---
title: 1 分钟快速开始
date: 2026-02-21
summary: 一分钟内启动 k2s 服务端并通过 k2 客户端完成首次连接。服务端自动生成密钥，客户端一条命令接入。
section: getting-started
order: 2
draft: false
---

# 1 分钟快速开始

本文引导您以最快速度完成 k2 的部署。您需要一台可公网访问的 Linux 服务器（VPS）和一台客户端机器。

## 第一步：部署服务端（30 秒）

在您的服务器上执行：

```bash
curl -fsSL https://dl.k2.52j.me/install.sh | sudo sh -s k2s
sudo k2s run
```

**首次启动**会自动：

- 生成 TLS 自签名证书（RSA + EC 双证书）
- 生成 ECH 密钥
- 安装系统服务
- 打印即用的连接 URL，格式如下：

```
k2v5://abc123:tok456@203.0.113.5:443?ech=AEX0...&pin=sha256:...
```

> 如果需要再次查看连接 URL，再次运行 `sudo k2s run` 即可。

## 第二步：连接客户端（30 秒）

在您的客户端机器上执行（将 URL 替换为上一步输出的实际 URL）：

```bash
curl -fsSL https://dl.k2.52j.me/install.sh | sudo sh -s k2
sudo k2 up k2v5://abc123:tok456@203.0.113.5:443?ech=AEX0...&pin=sha256:...
```

连接成功后，所有流量将通过 k2 隧道加密传输。

## 常用命令

```bash
k2 status     # 查看连接状态
k2 down       # 断开连接
k2 up <url>   # 重新连接
```

## 无需 root 的代理模式

如果您不希望使用 root 权限创建 TUN 设备，可使用代理模式：

```bash
k2 up --mode proxy k2v5://abc123:tok456@203.0.113.5:443?ech=AEX0...&pin=sha256:...
```

代理模式在本地启动 SOCKS5 代理，监听 `socks5://127.0.0.1:1080`，无需修改系统路由。

---

接下来阅读：[k2s 服务端详细部署](/k2/server) 了解高级配置和 Docker 部署。
