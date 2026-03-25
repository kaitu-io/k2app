---
title: "在 AI 工具中配置 k2-mcp 接管 VPN 连接"
date: 2026-03-25
summary: "k2-mcp 随开途桌面端内置，无需单独安装。本指南介绍如何在 Claude、Cursor、OpenClaw 等 AI 工具中完成配置，让 AI 直接管理 VPN 连接和订阅。"
tags: ["MCP", "Claude", "Cursor", "OpenClaw", "AI", "k2"]
draft: false
---

# 在 AI 工具中配置 k2-mcp 接管 VPN 连接

**k2-mcp 是随[开途 k2](/zh-CN/k2/quickstart) 桌面端一起安装的 MCP 服务器，无需单独下载。配置完成后，Claude、Cursor、OpenClaw 等 AI 工具可以直接连接节点、查询状态、管理订阅，不再需要手动打开 App。**

## 前提条件

- 已安装开途 k2 桌面端（v0.4.2 或更高）
- 使用以下支持的 AI 工具之一

| AI 工具 | 集成方式 | 适合场景 |
|---------|---------|---------|
| Claude Desktop | MCP（`mcpServers`） | 日常对话、个人助理 |
| Claude Code | MCP（CLI 命令） | 终端开发工作流 |
| Cursor | MCP（`mcp.json`） | IDE 内编码 |
| Windsurf | MCP（`mcp_config.json`） | IDE 内编码 |
| VS Code (Copilot) | MCP（`.vscode/mcp.json`） | IDE 内编码 |
| **OpenClaw** | **exec + k2 CLI（原生）** | **AI 工作自动化、自主 Agent** |

> **OpenClaw 用户**：OpenClaw 内置 `exec` 工具，可直接调用 `k2` CLI，无需配置 k2-mcp。[跳转至 OpenClaw 配置](#openclaw)。

---

## k2-mcp 二进制位置

k2-mcp 随桌面端安装，与 k2 二进制在同一目录：

| 平台 | 路径 |
|------|------|
| macOS | `/Applications/Kaitu.app/Contents/MacOS/k2-mcp` |
| Windows | `C:\Program Files\Kaitu\k2-mcp.exe` |
| Linux | `/usr/local/bin/k2-mcp` |

---

## 配置步骤

### Claude Desktop

编辑配置文件（不存在则新建）：

- macOS：`~/Library/Application Support/Claude/claude_desktop_config.json`
- Windows：`%APPDATA%\Claude\claude_desktop_config.json`

```json
{
  "mcpServers": {
    "k2": {
      "command": "/Applications/Kaitu.app/Contents/MacOS/k2-mcp"
    }
  }
}
```

保存后重启 Claude Desktop，对话框中输入"帮我查看 VPN 状态"验证。

### Claude Code

```bash
claude mcp add k2 /Applications/Kaitu.app/Contents/MacOS/k2-mcp
```

### Cursor

编辑 `~/.cursor/mcp.json`：

```json
{
  "mcpServers": {
    "k2": {
      "command": "/Applications/Kaitu.app/Contents/MacOS/k2-mcp"
    }
  }
}
```

### Windsurf

编辑 `~/.codeium/windsurf/mcp_config.json`，格式同上。

### VS Code (GitHub Copilot)

在工作区根目录创建 `.vscode/mcp.json`：

```json
{
  "servers": {
    "k2": {
      "type": "stdio",
      "command": "/Applications/Kaitu.app/Contents/MacOS/k2-mcp"
    }
  }
}
```

---

## OpenClaw 配置 {#openclaw}

OpenClaw 是具备自主执行能力的 AI Agent 平台，内置 `exec` 工具，可以直接调用 `k2` CLI——**无需配置 k2-mcp**，网络管理是 OpenClaw Agent 的原生能力。

### 工作原理

OpenClaw Agent 在执行任务时，如果需要管理 VPN 连接，会直接通过 `exec` 工具调用 k2 命令行：

```bash
k2 status      # 查看当前连接状态
k2 up          # 连接 VPN（使用已配置的默认节点）
k2 down        # 断开 VPN
```

这比 MCP 方式更直接——Agent 直接操作本机 k2 守护进程，无需中间层。

### 典型场景

**自动处理网络问题：**
当 Agent 正在执行需要访问境外服务的任务（调 API、拉代码、查资料），请求失败时，Agent 会：

1. 调用 `exec` 运行 `k2 status` 检查连接状态
2. 如果未连接，运行 `k2 up` 建立 VPN 连接
3. 重试原任务

全程无需用户介入。

**OpenClaw 配置 k2 工具允许列表：**

默认情况下 OpenClaw Agent 已有 `exec` 权限，无需额外配置。如果你的 OpenClaw 开启了工具白名单，确保 `exec` 在允许列表中：

```json
{
  "agents": {
    "defaults": {
      "tools": {
        "allow": ["group:runtime"]
      }
    }
  }
}
```

### 订阅和账号管理

需要查询订阅状态或生成续费链接时，OpenClaw 可以调用 k2-mcp（可选，按需配置）：

```bash
# 在 OpenClaw 所在机器上添加 k2-mcp 为可调用工具
# OpenClaw Agent 会通过 exec 运行 k2-mcp 工具
k2-mcp account_info
k2-mcp list_plans
```

或者直接告诉 OpenClaw Agent：

> "帮我查一下开途订阅还有多久到期"

Agent 会通过 k2-mcp 或直接访问[账号页面](/zh-CN/account)获取信息。

---

## 首次使用（Claude / Cursor / Windsurf）

配置 MCP 后，第一次使用时 AI 会调用 `login` 工具验证账号。登录后会话持久保存在 `~/.kaitu/mcp-session.json`，后续无需重复登录。

## k2-mcp 工具列表

| 工具 | 说明 |
|------|------|
| `login` | 账号登录 |
| `account_info` | 查看账号状态和订阅到期时间 |
| `list_servers` | 列出所有节点及负载 |
| `connect` | 连接指定节点 |
| `disconnect` | 断开 VPN |
| `status` | 查看当前连接状态 |
| `list_plans` | 查看订阅套餐 |
| `subscribe` | 生成续费支付链接 |

## 故障排查

| 现象 | 可能原因 | 解决方法 |
|------|---------|---------|
| AI 找不到 k2 工具 | 配置未生效 | 重启 AI 工具 |
| `k2-mcp: command not found` | 路径错误 | 确认桌面端已安装，检查二进制路径 |
| 登录失败 | 账号或密码错误 | 检查账号，或前往[购买页面](/zh-CN/purchase)确认订阅状态 |
| 连接失败 | k2 守护进程未运行 | 打开开途 App 确认已启动 |
| 会话过期 | token 失效 | 重新调用 `login` |
| OpenClaw exec 被拒绝 | 工具白名单限制 | 在 `openclaw.json` 中将 `group:runtime` 加入 `tools.allow` |

## FAQ

**k2-mcp 和开途 App 会冲突吗？**

不冲突。k2-mcp 通过本地 API 与 k2 守护进程通信，复用同一条 VPN 连接。App、k2-mcp、OpenClaw 可以同时使用。

**OpenClaw 为什么不需要配置 k2-mcp？**

OpenClaw 是 Agent 平台，内置 `exec` 工具可以直接运行 `k2 up/down/status` 等 CLI 命令。k2-mcp 是为不支持 shell 执行的 MCP 客户端（如 Claude Desktop）设计的桥接层，OpenClaw 不需要这个中间层。

**AI 能看到我的密码吗？**

密码通过 HTTPS 直接发送到开途服务器，k2-mcp 不存储密码。会话 token 保存在 `~/.kaitu/mcp-session.json`，权限为 `0600`（仅当前用户可读）。OpenClaw 通过 k2 CLI 操作时，无需任何账号凭证。

**Linux 无图形界面能用吗？**

可以。k2-mcp 是纯命令行工具。确保 k2 守护进程在后台运行（`k2 up`），k2-mcp 即可正常工作。OpenClaw 在 Linux 下同样通过 exec 调用 k2 CLI。

**节点怎么选最优？**

`list_servers` 返回每个节点的 `traffic_usage_percent`（负载百分比）。AI 会自动选择负载较低的节点，也可以直接告诉 AI 你偏好的地区。OpenClaw Agent 可以根据连接质量自动重选节点，无需用户指定。
