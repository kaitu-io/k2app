---
title: k2s 服务端部署
date: 2026-02-22
summary: 在 Linux VPS 上一键安装并运行 k2s 服务端。零配置启动——自动生成密钥证书，k2arc 自动优化传输性能。
section: getting-started
order: 3
draft: false
---

# k2s 服务端部署

k2s 是 k2 协议的服务端程序，设计目标是**零配置启动**。您只需一条命令，服务端即可自动完成所有密钥生成和服务注册。k2arc 在服务端同步启用，自动优化传输性能。

## 系统要求

- **操作系统**：Linux（x86_64 或 arm64）
- **端口**：需要对外开放 **443 端口**（UDP + TCP）
- **权限**：需要 root 权限（或具备 `NET_BIND_SERVICE` 能力）

## 一键安装

```bash
curl -fsSL https://kaitu.io/install.sh | sudo sh -s k2s
```

安装完成后，验证安装：

```bash
k2s --version
```

## 启动服务

```bash
sudo k2s run
```

**首次启动**会在 `/etc/k2s/` 目录下自动创建：

- `server.crt` / `server.key`（EC 证书）
- `server-rsa.crt` / `server-rsa.key`（RSA 证书）
- `echkey.pem`（ECH HPKE 私钥）
- `config.yml`（服务端配置文件）

启动成功后，终端输出连接 URL：

```
k2s 已启动，监听 0.0.0.0:443
连接 URL：
k2v5://abc123:tok456@203.0.113.5:443?ech=AEX0...&pin=sha256:abc...
```

> k2s 同时自动启用 k2arc 拥塞控制。无需任何手动调优。

## 查看连接 URL

如果您关闭了终端，可以随时再次查看：

```bash
sudo k2s run
```

或者查看配置文件：

```bash
cat /etc/k2s/config.yml
```

## 系统服务（自动启动）

首次启动时，k2s 会自动注册 systemd 服务：

```bash
# 查看服务状态
sudo systemctl status k2s

# 设置开机自启（默认已设置）
sudo systemctl enable k2s

# 手动停止/启动
sudo systemctl stop k2s
sudo systemctl start k2s

# 查看日志
sudo journalctl -u k2s -f
```

## Docker 部署

项目内置 Docker Compose 配置，适合容器化环境：

```bash
git clone https://github.com/kaitu-io/k2.git
cd k2/docker/
docker compose up --build
```

默认端口映射：
- **443**：k2s 服务端（UDP + TCP）
- **1080**：k2 客户端 SOCKS5 代理
- **1777**：k2 daemon API（本地管理）

## 高级配置

生成默认配置文件后手动编辑：

```bash
k2s demo-config > server.yml
```

常用配置项：

```yaml
listen: 0.0.0.0:443        # 监听地址
public_name: example-cdn.com    # ECH 外层域名（用于主动探测伪装，默认为某主流 CDN 域名）
reverse_proxy: auto        # 非 ECH 连接反向代理目标（auto=自动从 DNS 解析）
cert_refresh_interval: 24h # TLS 记录填充模板刷新周期
```

## 防火墙配置

确保 443 端口的 UDP 和 TCP 流量都已放行：

```bash
# iptables（示例）
sudo iptables -A INPUT -p udp --dport 443 -j ACCEPT
sudo iptables -A INPUT -p tcp --dport 443 -j ACCEPT

# ufw（示例）
sudo ufw allow 443/udp
sudo ufw allow 443/tcp
```

## 常见问题

**端口被占用怎么办？**

检查是否有其他程序（如 Nginx）占用 443 端口：

```bash
sudo ss -tlunp | grep :443
```

可通过配置文件修改 k2s 监听端口（非标准端口会影响隐身效果）。

**如何更新 k2s？**

重新执行安装命令即可，新版本会覆盖旧版本但保留配置文件：

```bash
curl -fsSL https://kaitu.io/install.sh | sudo sh -s k2s
sudo systemctl restart k2s
```

---

接下来阅读：[k2 客户端使用](/k2/client) 了解如何连接到您部署的服务端。
