---
title: "Claude Code 安装教程（2026）：官方命令、npm 国内镜像与常见报错"
date: 2026-11-10
summary: "Claude Code 官方推荐用一行命令安装：Mac / Linux 用 curl，Windows 用 PowerShell。本文按官方文档整理四种安装方式、各自需要访问的域名、npm 国内镜像能否安装（实测可以），以及常见报错的含义和接入 DeepSeek 的方法。"
tags: ["claude code安装", "claude code安装教程", "claude code下载", "claude code使用教程", "claude code接入deepseek", "claude cli安装"]
brand: kaitu
draft: false
section: overseas-sites
---

<strong>Claude Code 官方推荐的安装方式是一行命令：Mac、Linux、WSL 用 `curl -fsSL https://claude.ai/install.sh | bash`，Windows PowerShell 用 `irm https://claude.ai/install.ps1 | iex`。</strong>装完运行 `claude --version` 能看到版本号就成功了。使用需要 Claude 的付费账号（Pro、Max、Ultra、Team、Enterprise）或 Console 账号，免费版不含 Claude Code。如果官方脚本下载不下来，npm 包在国内镜像 npmmirror 上同步完整，可以用镜像安装；也可以把 Claude Code 接到 DeepSeek 等国内模型上使用。

<em>本文命令和说明来自 Claude Code 官方文档（[安装](https://code.claude.com/docs/en/setup)、[安装排错](https://code.claude.com/docs/en/troubleshoot-install)、[网络配置](https://code.claude.com/docs/en/network-config)），2026 年 11 月 10 日查看；npm 镜像为同日实测。Claude Code 更新很快，以官方最新文档为准。</em>

## 安装条件与四种安装方式

### 安装前要确认的条件

| 项目 | 官方要求 |
|---|---|
| 系统 | macOS 13 以上；Windows 10 1809 以上或 Windows Server 2019 以上；Ubuntu 20.04+、Debian 10+、Alpine 3.19+ |
| 硬件 | 4 GB 以上内存，x64 或 ARM64 处理器 |
| 账号 | Pro、Max、Ultra、Team、Enterprise 或 Console 账号；也可以用 Amazon Bedrock、Google Cloud、Microsoft Foundry 等第三方接口 |
| 地区 | 官方系统要求里写明“Location: Anthropic supported countries”，即 [Anthropic 支持的国家和地区](https://www.anthropic.com/supported-countries)。这份名单里没有中国大陆、香港和澳门，台湾在列 |

Windows 上建议先装 [Git for Windows](https://git-scm.com/downloads/win)，这样 Claude Code 能使用 Bash；不装也能用，会改用 PowerShell。

### 四种安装方式怎么选

| 方式 | 命令 | 自动更新 | 安装时要访问的地址 |
|---|---|---|---|
| 原生安装（官方推荐） | Mac / Linux / WSL：`curl -fsSL https://claude.ai/install.sh \| bash`<br>PowerShell：`irm https://claude.ai/install.ps1 \| iex`<br>CMD：`curl -fsSL https://claude.ai/install.cmd -o install.cmd && install.cmd && del install.cmd` | 会，后台自动更新 | claude.ai、downloads.claude.ai |
| Homebrew（Mac） | `brew install --cask claude-code`（稳定版，通常晚一周）<br>`brew install --cask claude-code@latest`（最新版） | 默认不会，用 `brew upgrade` 更新 | Homebrew 的源（官方未单独列出） |
| WinGet（Windows） | `winget install Anthropic.ClaudeCode` | 默认不会，用 `winget upgrade Anthropic.ClaudeCode` | WinGet 的源（官方未单独列出） |
| npm | `npm install -g @anthropic-ai/claude-code` | 会（npm 全局目录可写时） | npm 仓库，<strong>可以换成国内镜像</strong> |

几点官方提醒：

- 原生安装<strong>不显示进度</strong>，命令跑完后要<strong>新开一个终端窗口</strong>再运行 `claude --version`。
- Windows 的两条命令不能混用：看到 `'irm' is not recognized` 说明你在 CMD 里，看到 `The token '&&' is not a valid statement separator` 说明你在 PowerShell 里。
- npm 安装<strong>不要加 sudo</strong>。官方解释：Claude Code 自己更新时不用 sudo，需要 sudo 才能装上的话，以后就没法自动更新。

## 用 npm 国内镜像安装（实测可用）

官方脚本要从 claude.ai 和 downloads.claude.ai 下载。下载不下来时，npm 是最省事的替代办法。

官方文档说明，<strong>npm 包装的是和原生安装完全相同的程序</strong>：npm 会按你的系统拉取对应的平台包（例如 `@anthropic-ai/claude-code-darwin-arm64`），再链接到位，运行时不依赖 Node.js。所以只要镜像同步了这些平台包，用镜像装出来的就是官方程序。

我们在 2026 年 11 月 10 日核对了 npmmirror（`registry.npmmirror.com`）：

| 包 | 官方 npm 最新版 | npmmirror 最新版 |
|---|---|---|
| `@anthropic-ai/claude-code`（主包） | 2.1.322 | 2.1.322 |
| 8 个平台包：darwin-arm64、darwin-x64、linux-x64、linux-arm64、linux-x64-musl、linux-arm64-musl、win32-x64、win32-arm64 | 均为 2.1.322 | <strong>均为 2.1.322</strong> |

安装命令：

```bash
npm install -g @anthropic-ai/claude-code --registry=https://registry.npmmirror.com
```

注意两点：

- <strong>Node.js 版本</strong>：npm 包要求 Node.js 22 或以上。官方说明，版本更低时 npm 只会打印 `EBADENGINE` 警告，安装仍会完成，`claude` 也能运行，因为它运行时不用你的 Node.js。
- <strong>不要禁用可选依赖</strong>（optional dependencies），否则平台包装不上，运行时会报 `Error: claude native binary not installed`。

装好以后能否正常使用，还取决于下一节的登录和接口地址。

## 网络：需要访问的地址与代理设置

### 需要访问哪些地址

官方[网络配置](https://code.claude.com/docs/en/network-config)文档列出了 Claude Code 需要访问的地址，整理如下：

| 地址 | 用途 | 什么时候需要 |
|---|---|---|
| `claude.ai` / `claude.com` | 安装脚本、claude.ai 账号登录 | 原生安装；用 claude.ai 账号登录 |
| `downloads.claude.ai` | 原生安装包、自动更新、插件可执行文件 | 原生安装和自动更新；npm 安装不需要它来装主程序 |
| `api.anthropic.com` | 调用模型，以及功能开关、遥测、WebFetch 的域名安全检查 | 使用 Anthropic 的模型时 |
| `platform.claude.com` | Console 账号登录；claude.ai 账号的令牌交换和刷新也走这里 | 用官方账号登录时 |
| `registry.npmjs.org` | npm 安装 Claude Code、安装插件、`npx` 启动的 MCP 服务 | npm 安装（可换成镜像）、插件 |
| `github.com`、`raw.githubusercontent.com` | 插件市场、更新日志 | 用插件时 |
| `storage.googleapis.com` | 插件统计；2.1.116 以前版本的安装器 | 用插件或安装旧版本时 |

官方给了一条检查能否连上下载服务器的命令：

```bash
curl -sI https://downloads.claude.ai/claude-code-releases/latest
```

Windows PowerShell 里要写 `curl.exe -sI …`。第一行出现 `200` 说明连得上。官方对其他结果的解释是：<strong>`403` 通常是代理或网络过滤拦住了，或者 Claude Code 在你所在的地区不提供</strong>；没有输出、`Could not resolve host` 或超时，说明网络拦住了连接，常见原因包括公司防火墙、<strong>“Regional network restrictions: try a VPN or alternative network”</strong>（地区网络限制，换用 VPN 或其他网络），以及证书问题。

### 设置代理时最容易踩的坑

官方支持用环境变量走代理：`HTTPS_PROXY`、`HTTP_PROXY`、`NO_PROXY`。三个容易出错的地方：

1. <strong>不支持 SOCKS 代理。</strong>官方原文：“Claude Code does not support SOCKS proxies.” 有的代理客户端给的是 SOCKS5 端口，填进去不会生效，要用 HTTP 代理端口。
2. <strong>地址要带 `http://` 前缀</strong>，例如 `export HTTPS_PROXY=http://127.0.0.1:端口`。
3. <strong>先设代理再安装</strong>：原生安装脚本本身也要下载文件，官方示例是先 `export` 两个变量，再运行安装命令。

如果你用的是系统级接管网络的工具（例如开途电脑版，它在系统里创建虚拟网卡，终端里的 curl、npm、claude 的流量都会经过它），就不需要单独设置这些环境变量。

## 常见报错对照

以下全部来自官方[安装排错页](https://code.claude.com/docs/en/troubleshoot-install)：

| 你看到的 | 原因 | 怎么办 |
|---|---|---|
| `syntax error near unexpected token '<'`（CMD 里是 `< was unexpected at this time`） | 安装地址返回的是网页而不是脚本 | 如果错误内容里有 “App unavailable in region”，说明 Claude Code 在你所在的地区不提供；否则检查网络后重试，或改用 npm 等其他方式 |
| `curl: (22) The requested URL returned error: 403` | 同上；也可能是公司代理拦截 | 同上 |
| `App unavailable in region` | 官方解释：“Claude Code is not available in your country” | 见[支持地区](https://www.anthropic.com/supported-countries) |
| `Failed to fetch version`、连不上下载服务器 | 网络或代理问题 | 用上面的 `curl -sI` 命令检查，设置 `HTTPS_PROXY` |
| `The connection dropped while downloading the update` | 下载中途断开 | 重新运行，或设置代理 |
| `command not found: claude`、`'claude' is not recognized` | 安装目录不在 PATH 里 | 新开终端；仍不行就把 `~/.local/bin`（Windows 是 `%USERPROFILE%\.local\bin`）加进 PATH |
| `EACCES: permission denied` | npm 全局目录没有写权限 | 修正目录权限，不要用 sudo |
| `TLS connect error`、`unable to get local issuer certificate` | 证书问题，常见于公司网络 | 更新系统证书，或配置公司的 CA 证书 |
| 登录后 `API Error: 403 Request not allowed` | 订阅未生效、Console 账号缺少权限，或代理干扰 | 在 claude.ai/settings 确认订阅状态 |

装好后可以运行 `claude doctor`，它会检查安装和配置，并给出修复建议。

## 不用 Anthropic 账号：接入 DeepSeek

Claude Code 支持把模型接口换成兼容 Anthropic 格式的第三方服务。DeepSeek 官方提供了专门的[接入说明](https://api-docs.deepseek.com/quick_start/agent_integrations/claude_code)，接口地址在国内。Mac / Linux 的设置（2026-11-10 DeepSeek 文档原文）：

```bash
export ANTHROPIC_BASE_URL=https://api.deepseek.com/anthropic
export ANTHROPIC_AUTH_TOKEN=<你的 DeepSeek API Key>
export ANTHROPIC_MODEL=deepseek-flash[1m]
export ANTHROPIC_DEFAULT_OPUS_MODEL=deepseek-flash[1m]
export ANTHROPIC_DEFAULT_SONNET_MODEL=deepseek-flash[1m]
export ANTHROPIC_DEFAULT_HAIKU_MODEL=deepseek-flash
export CLAUDE_CODE_SUBAGENT_MODEL=deepseek-flash
```

Windows PowerShell 把每行写成 `$env:变量名="值"`。设置后进入项目目录运行 `claude`。

两点说明：

- DeepSeek 文档写的是“Node.js 18+”，而 Claude Code 的 npm 包要求 Node.js 22+。按官方说明，低版本只会出现警告、不影响运行，但建议直接装 Node.js 22 以上，省掉排查。
- 这种用法调用的是 DeepSeek 的模型，按 DeepSeek 的价格计费，效果和 Claude 模型不同。DeepSeek 文档还说明，Claude Code 可能调用 DeepSeek 的联网搜索，会另外产生搜索费用。

## 开途能做什么

开途是网络工具，只负责“连得上、连得稳”。电脑版默认智能分流：国内网站（如 npmmirror、DeepSeek）按分流规则直连，境外网站走开途的线路；它在系统层面接管网络，终端里的命令不用单独设置代理变量。拥塞控制 [k2cc](/k2/k2cc) 针对跨境线路的高丢包设计，长时间保持连接的命令行工具更需要这一点。

账号、订阅、付款和使用地区由 Anthropic 按自己的规则判断，不在网络工具的范围内；这方面的官方说明见[国内能用 ChatGPT 和 Claude 吗](/guides/chatgpt-claude-china)和[Claude 账号为什么会被封](/guides/claude-account-suspended-reasons)。

开途要付费，App 版 1 年 $49 起，没有免费试用；首次购买 7 天内、流量不超过 1GB 可申请退款。电脑版在[下载页](/install?utm_source=guide&utm_medium=content&utm_campaign=claude-code-install-china)，价格见[定价页](/pricing?utm_source=guide&utm_medium=content&utm_campaign=claude-code-install-china)。

## 常见问题

<strong>Claude Code 怎么安装？</strong>

Mac、Linux、WSL 运行 `curl -fsSL https://claude.ai/install.sh | bash`；Windows PowerShell 运行 `irm https://claude.ai/install.ps1 | iex`。装完新开终端，运行 `claude --version` 检查。

<strong>Claude Code 免费吗？</strong>

不免费。官方要求 Pro、Max、Ultra、Team、Enterprise 或 Console 账号，免费版 claude.ai 不含 Claude Code。也可以接 DeepSeek 等第三方接口，按对方价格付费。

<strong>Claude Code 可以用 npm 国内镜像安装吗？</strong>

可以。2026 年 11 月 10 日核对，npmmirror 上主包和 8 个平台包都已同步到和官方相同的版本，运行 `npm install -g @anthropic-ai/claude-code --registry=https://registry.npmmirror.com` 即可。

<strong>安装时报 syntax error near unexpected token '<' 是什么意思？</strong>

安装地址返回了网页而不是脚本。如果内容里有 “App unavailable in region”，官方解释是 Claude Code 在你所在的地区不提供；否则通常是网络问题，可以检查网络后重试或改用 npm 安装。

<strong>Claude Code 怎么接入 DeepSeek？</strong>

把 `ANTHROPIC_BASE_URL` 设为 `https://api.deepseek.com/anthropic`，`ANTHROPIC_AUTH_TOKEN` 设为 DeepSeek 的 API Key，再按 DeepSeek 文档设置模型名，然后运行 `claude`。

<strong>Claude Code 支持 SOCKS5 代理吗？</strong>

不支持。官方原文 “Claude Code does not support SOCKS proxies”，要用 HTTP 代理地址，并带上 `http://` 前缀。

延伸阅读：[国内能用 ChatGPT 和 Claude 吗](/guides/chatgpt-claude-china) · [Claude 账号为什么会被封](/guides/claude-account-suspended-reasons) · [k2cc 拥塞控制原理](/k2/k2cc)
