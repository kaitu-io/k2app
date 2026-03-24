---
title: "开途 k2 支持 MCP：让 AI 直接管理你的 VPN"
date: 2026-03-25
summary: "开途 k2 现已提供 MCP 服务器，让 Claude、Cursor 等 AI 工具直接连接、切换节点和管理订阅，无需手动打开 App。"
tags: ["产品更新", "MCP", "AI", "k2"]
draft: false
---

# 开途 k2 支持 MCP：让 AI 直接管理你的 VPN

**开途 k2 现在提供原生 MCP 服务器（k2-mcp），让 Claude、Cursor、Windsurf 等 AI 工具直接控制你的 VPN 连接、切换节点、查看订阅状态。你的 AI 助手可以在工作流中自动处理网络问题，不再需要你手动切换 App。**

## 为什么需要这个功能

越来越多的开发者、研究人员和内容创作者在使用 AI 助手工作——写代码、调 API、整理资料。这些工作场景有一个共同需求：**网络要通**。

以前的问题是：AI 助手发现网络有问题时，它能做的只是告诉你"请检查网络连接"，然后等你手动去处理。你得切出去，打开开途 App，切个节点，等重连，再回来继续。这个打断虽然短暂，但反复出现很烦。

有了 k2-mcp，AI 助手可以直接处理这层。

## k2-mcp 能做什么

k2-mcp 暴露 8 个 MCP 工具，覆盖完整的使用场景：

| 工具 | 功能 |
|------|------|
| `login` | 账号登录，获取会话 |
| `account_info` | 查看账号状态和订阅到期时间 |
| `list_servers` | 列出所有可用节点（含负载信息）|
| `connect` | 连接到指定节点 |
| `disconnect` | 断开 VPN |
| `status` | 查看当前连接状态 |
| `list_plans` | 查看订阅套餐 |
| `subscribe` | 创建续费订单，获取支付链接 |

## 实际工作流长什么样

**场景一：AI 助手自动处理网络问题**

你在用 Claude Code 或 OpenClaw 跑一个需要访问境外 API 的任务，请求超时了。AI 检查状态，发现当前节点负载过高，自动切换到延迟更低的节点，重试请求，继续工作。你什么都不用做。

**场景二：直接开口让 AI 处理**

> "帮我切到日本节点。"
> "现在连的哪里？延迟多少？"
> "我的订阅还有多久到期？"

**场景三：续费提醒**

AI 在调用 `account_info` 时发现订阅快到期：

> "你的订阅将在 3 天后到期。需要我帮你查看续费套餐吗？"

你说"查一下年付的"，AI 调用 `list_plans` 返回价格，你确认后 AI 调用 `subscribe` 生成支付链接，直接发给你。

## 如何接入

k2-mcp 是随开途桌面端一起分发的独立二进制文件，**不需要单独安装**。

在你的 AI 工具中添加以下配置：

**Claude Desktop：**

编辑 `~/Library/Application Support/Claude/claude_desktop_config.json`：

```json
{
  "mcpServers": {
    "k2": {
      "command": "/Applications/Kaitu.app/Contents/MacOS/k2-mcp"
    }
  }
}
```

**Claude Code：**

```bash
claude mcp add k2 /Applications/Kaitu.app/Contents/MacOS/k2-mcp
```

**Cursor / Windsurf：**

编辑对应的 `mcp.json`：

```json
{
  "mcpServers": {
    "k2": {
      "command": "/Applications/Kaitu.app/Contents/MacOS/k2-mcp"
    }
  }
}
```

接入后，第一次使用时 AI 会调用 `login` 工具完成账号验证，之后会话自动保持，无需每次重新登录。

## 安全说明

几个常见问题：

**AI 能看到我的密码吗？**

登录时密码通过 HTTPS 直接发送到开途服务器，k2-mcp 不存储密码。会话 token 保存在本地 `~/.kaitu/mcp-session.json`，文件权限为 `0600`（仅本机当前用户可读）。

**AI 能做什么，不能做什么？**

k2-mcp 的权限仅限于 VPN 连接管理和订阅查询。它无法访问你的完整账号密码、浏览记录、设备上的其他文件，也无法代替你完成支付（只能生成支付链接，需要你自己点击确认）。

**会话过期怎么办？**

token 过期时 k2-mcp 会自动用 refresh token 续签。refresh token 也失效时，AI 会提示你重新调用 `login`。

## 支持的平台

k2-mcp 作为桌面端 sidecar 分发，支持：

- macOS（Intel + Apple Silicon）
- Windows（x64 + ARM64）
- Linux（x64 + ARM64）

移动端（iOS、Android）暂不支持——MCP 主要用于桌面 AI 工具场景。

## 延伸阅读

- [k2cc (k2 congestion control)](/zh-CN/k2/k2cc) — 了解开途的自研拥塞控制协议
- [快速开始](/zh-CN/k2/quickstart) — 开途 k2 安装和基本使用
- [客户端文档](/zh-CN/k2/client) — 桌面端完整功能说明

## FAQ

**k2-mcp 和开途 App 是什么关系？**

k2-mcp 是随开途桌面端一起安装的独立工具，它通过本地 HTTP API 与开途后台守护进程通信，复用同一个 VPN 连接。你仍然可以正常使用开途 App，k2-mcp 和 App 不冲突。

**哪些 AI 工具支持 MCP？**

目前支持 MCP 的主流工具：Claude Desktop、Claude Code、Cursor、Windsurf、VS Code（通过 GitHub Copilot 扩展）。MCP 是 Anthropic 提出的开放协议，更多工具持续接入中。

**我用 Linux 没有图形界面，k2-mcp 能用吗？**

可以。k2-mcp 是纯命令行工具，支持 headless 运行。在服务器或无 GUI 环境下，用 `k2 up/down/status` 管理 VPN 进程，k2-mcp 连接本地守护进程即可工作。

**节点怎么选？k2-mcp 会自动选最快的吗？**

`list_servers` 返回每个节点的负载信息（`traffic_usage_percent`），AI 可以根据这个数据选择负载较低的节点。k2-mcp 本身不内置自动选节点逻辑——由 AI 根据你的需求决策，更灵活。
