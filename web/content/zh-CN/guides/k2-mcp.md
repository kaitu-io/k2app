---
title: "在 AI 工具中配置 k2-mcp 接管 VPN 连接"
date: 2026-03-25
summary: "k2-mcp 随开途桌面端内置，无需单独安装。本指南介绍如何在 Claude、Cursor 等 AI 工具中完成配置，让 AI 直接管理 VPN 连接和订阅。"
tags: ["MCP", "Claude", "Cursor", "AI", "k2"]
draft: false
---

# 在 AI 工具中配置 k2-mcp 接管 VPN 连接

**k2-mcp 是随[开途 k2](/zh-CN/k2/quickstart) 桌面端一起安装的 MCP 服务器，无需单独下载。配置完成后，Claude、Cursor、Windsurf 等 AI 工具可以直接连接节点、查询状态、管理订阅，不再需要手动打开 App。**

## 前提条件

- 已安装开途 k2 桌面端（v0.4.2 或更高）
- 使用支持 MCP 的 AI 工具（见下表）

| AI 工具 | MCP 支持 | 配置文件位置 |
|---------|---------|------------|
| Claude Desktop | ✅ 原生支持 | `~/Library/Application Support/Claude/claude_desktop_config.json` |
| Claude Code | ✅ 原生支持 | CLI 命令 |
| Cursor | ✅ 原生支持 | `~/.cursor/mcp.json` |
| Windsurf | ✅ 原生支持 | `~/.codeium/windsurf/mcp_config.json` |
| VS Code (Copilot) | ✅ 插件支持 | `.vscode/mcp.json` |

## k2-mcp 二进制位置

k2-mcp 随桌面端安装，与 k2 二进制在同一目录：

| 平台 | 路径 |
|------|------|
| macOS | `/Applications/Kaitu.app/Contents/MacOS/k2-mcp` |
| Windows | `C:\Program Files\Kaitu\k2-mcp.exe` |
| Linux | `/usr/local/bin/k2-mcp` |

## 配置步骤

### Claude Desktop

编辑配置文件（不存在则新建）：

**macOS**: `~/Library/Application Support/Claude/claude_desktop_config.json`
**Windows**: `%APPDATA%\Claude\claude_desktop_config.json`

```json
{
  "mcpServers": {
    "k2": {
      "command": "/Applications/Kaitu.app/Contents/MacOS/k2-mcp"
    }
  }
}
```

保存后重启 Claude Desktop。

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

## 首次使用：登录账号

配置好后，第一次使用时 AI 会调用 `login` 工具验证账号：

> "帮我连接到 VPN"

AI 会自动请求你的邮箱和密码完成登录。登录后会话持久保存在 `~/.kaitu/mcp-session.json`，后续无需重复登录。

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

## 验证配置

配置完成后，在 AI 工具中输入：

> "帮我查看当前 VPN 状态"

AI 调用 `status` 工具并返回当前连接信息，说明配置成功。

## 故障排查

| 现象 | 可能原因 | 解决方法 |
|------|---------|---------|
| AI 找不到 k2 工具 | 配置未生效 | 重启 AI 工具 |
| `k2-mcp: command not found` | 路径错误 | 确认桌面端已安装，检查二进制路径 |
| 登录失败 | 账号或密码错误 | 检查账号，或前往[购买页面](/zh-CN/purchase)确认订阅状态 |
| 连接失败 | k2 守护进程未运行 | 打开开途 App 确认已启动 |
| 会话过期 | token 失效 | 重新调用 `login` |

## 常见用法示例

**切换节点：**
> "帮我切到日本节点"

**查询订阅：**
> "我的订阅还有多久到期？"

**续费：**
> "帮我查一下年付套餐，生成支付链接"

**网络问题时让 AI 自动处理：**
在 Claude Code 或 [OpenClaw](https://openclaw.ai) 等 AI 工具中，当访问境外服务超时，AI 可以直接调用 `list_servers` 选低负载节点，调用 `connect` 切换，无需手动介入。

## FAQ

**k2-mcp 和开途 App 冲突吗？**

不冲突。k2-mcp 通过本地 API 与 k2 守护进程通信，复用同一条 VPN 连接。App 和 k2-mcp 可以同时使用。

**AI 能看到我的密码吗？**

密码通过 HTTPS 直接发送到开途服务器，k2-mcp 不存储密码。会话 token 保存在 `~/.kaitu/mcp-session.json`，权限为 `0600`（仅当前用户可读）。

**Linux 无图形界面能用吗？**

可以。k2-mcp 是纯命令行工具。确保 k2 守护进程在后台运行（`k2 up`），k2-mcp 即可正常工作。

**节点怎么选最优？**

`list_servers` 返回每个节点的 `traffic_usage_percent`（负载百分比）。AI 会自动选择负载较低的节点，也可以直接告诉 AI 你偏好的地区。
