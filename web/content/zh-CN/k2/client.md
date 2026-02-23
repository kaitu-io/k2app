---
title: k2 客户端使用
date: 2026-02-22
summary: 在 Linux 或 macOS 上安装 k2 命令行客户端，连接 k2v5 服务端。k2arc 自动探测最优速率，无需手动配置带宽。
section: getting-started
order: 4
draft: false
---

# k2 客户端使用

k2 命令行客户端支持 **Linux**（x86_64 / arm64）和 **macOS**（Intel / Apple Silicon）。
如果您希望使用图形界面，请参阅 [Kaitu 桌面客户端](/install)，它已内置 k2 协议，无需单独安装。

## 安装

```bash
curl -fsSL https://kaitu.io/install.sh | sudo sh -s k2
```

验证安装：

```bash
k2 --version
```

## 连接

将服务端输出的 k2v5 URL 传入 `k2 up` 命令：

```bash
sudo k2 up k2v5://abc123:tok456@203.0.113.5:443?ech=AEX0...&pin=sha256:...
```

连接成功后，系统路由表会自动更新，所有流量通过隧道传输。k2arc 会自动探测最优发送速率，无需像 Hysteria2 那样手动指定 `up_mbps` / `down_mbps`。

## 常用命令

```bash
# 连接（TUN 模式，需要 root）
sudo k2 up k2v5://...

# 代理模式（不需要 root，启动本地 SOCKS5 代理）
k2 up --mode proxy k2v5://...

# 查看当前连接状态
k2 status

# 断开连接
sudo k2 down

# 查看版本
k2 --version

# 查看帮助
k2 --help
```

## 连接模式

### TUN 模式（默认）

TUN 模式创建虚拟网络接口，拦截所有系统流量，效果等同于全局代理。需要 root 权限。

```bash
sudo k2 up k2v5://...
```

### 代理模式

代理模式在 `127.0.0.1:1080` 启动 SOCKS5 代理，无需修改系统路由。适合不需要全局代理或没有 root 权限的场景。

```bash
k2 up --mode proxy k2v5://...
```

配置系统或应用使用 `socks5://127.0.0.1:1080` 代理。

## 生成示例配置

如果您需要使用配置文件而非 URL，可以生成示例配置：

```bash
k2 demo-config > client.yml
```

编辑配置文件后，通过配置文件连接：

```bash
sudo k2 up --config client.yml
```

## TLS 指纹选择

k2 支持选择不同的 TLS 指纹进行伪装：

```bash
# 默认（Chrome）
sudo k2 up k2v5://...?fp=chrome

# Firefox 指纹
sudo k2 up k2v5://...?fp=firefox

# Safari 指纹
sudo k2 up k2v5://...?fp=safari

# 随机指纹
sudo k2 up k2v5://...?fp=random
```

## 状态说明

`k2 status` 输出示例：

```
状态：已连接
服务端：203.0.113.5:443
协议：k2v5 (QUIC/H3)
延迟：28ms
已上传：1.2 GB
已下载：8.4 GB
运行时间：2h 15m
```

当 QUIC 不可用时，协议自动切换为 `k2v5 (TCP-WS)`，无需手动干预。

## 卸载

```bash
sudo k2 down
sudo rm /usr/local/bin/k2
```

---

接下来阅读：[k2arc 自适应速率控制](/k2/protocol) 了解 k2arc 的核心能力。
