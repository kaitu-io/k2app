---
title: 端口跳跃配置指南
date: 2026-03-06
summary: 通过 UDP 端口跳跃提升 QUIC 连接稳定性，防止单端口限速
section: getting-started
order: 4
draft: false
---

# 端口跳跃配置指南

QUIC 协议默认通过单一 UDP 端口通信。部分网络环境会对固定端口进行 QoS 限速或封锁。端口跳跃（Port Hopping）让客户端在一个端口范围内随机选择 UDP 端口，从而规避针对单端口的限制。

## 工作原理

```
客户端 --[UDP:50042]--> 服务器防火墙 --[REDIRECT 到 :443]--> k2s
```

客户端从配置的端口范围中随机选择一个 UDP 端口发送流量。服务器通过防火墙 NAT 规则将这些端口的流量重定向到 k2s 监听的 443 端口。

## 前置条件

- k2s 服务端已安装并运行（`sudo k2s setup`）
- 443/tcp 和 443/udp 端口已开放

## 第一步：配置端口重定向

在服务器上设置防火墙规则，将 hop 端口范围的 UDP 流量重定向到 443 端口。

### Ubuntu / Debian（nftables）

```bash
# 添加 NAT 重定向规则
sudo nft add table ip nat
sudo nft add chain ip nat prerouting { type nat hook prerouting priority 0 \; }
sudo nft add rule ip nat prerouting udp dport 50000-50100 redirect to :443

# 持久化
sudo nft list ruleset > /etc/nftables.conf
sudo systemctl enable nftables
```

### Ubuntu / Debian（iptables，旧版）

```bash
sudo iptables -t nat -A PREROUTING -p udp --dport 50000:50100 -j REDIRECT --to-port 443

# 持久化
sudo apt install -y iptables-persistent
sudo netfilter-persistent save
```

### CentOS / RHEL / Rocky / AlmaLinux（firewalld）

```bash
sudo firewall-cmd --permanent --add-forward-port=port=50000-50100:proto=udp:toport=443
sudo firewall-cmd --reload
```

### Alpine Linux（iptables）

```bash
sudo iptables -t nat -A PREROUTING -p udp --dport 50000:50100 -j REDIRECT --to-port 443

# 持久化
sudo rc-update add iptables
sudo /etc/init.d/iptables save
```

### Arch Linux（nftables）

```bash
sudo nft add table ip nat
sudo nft add chain ip nat prerouting { type nat hook prerouting priority 0 \; }
sudo nft add rule ip nat prerouting udp dport 50000-50100 redirect to :443

sudo nft list ruleset > /etc/nftables.conf
sudo systemctl enable nftables
```

## 第二步：开放防火墙端口

确保 hop 端口范围的 UDP 入站流量被允许通过。

### ufw

```bash
sudo ufw allow 50000:50100/udp
```

### firewalld

```bash
sudo firewall-cmd --permanent --add-port=50000-50100/udp
sudo firewall-cmd --reload
```

### iptables

```bash
sudo iptables -A INPUT -p udp --dport 50000:50100 -j ACCEPT
```

### 云平台安全组

在云平台控制台的安全组 / 防火墙规则中添加入站规则：

| 协议 | 端口范围 | 来源 |
|------|---------|------|
| UDP | 50000-50100 | 0.0.0.0/0 |

适用于 AWS、阿里云、腾讯云、GCP、Azure 等云平台。

## 第三步：更新客户端 URI

在连接 URI 中添加 `&hop=50000-50100` 参数：

```
k2v5://alice:token@1.2.3.4:443?ech=...&pin=...&hop=50000-50100&country=JP#tokyo
```

将更新后的 URI 粘贴到客户端的节点管理页面即可。

## 验证

### 服务器端

```bash
# nftables
sudo nft list ruleset | grep 50000

# iptables
sudo iptables -t nat -L -n | grep 50000
```

### 客户端

连接后查看日志，确认 hop 端口正在使用。

## 自定义端口范围

- 默认范围 50000-50100（101 个端口），建议最少 50 个端口
- 端口范围不能与服务器上的其他服务冲突
- 起始端口建议 ≥ 49152（动态/私有端口范围）
