# Feature: Kaitu Ops MCP Server

## Meta

| Field | Value |
|-------|-------|
| Feature | kaitu-ops-mcp |
| Version | v1 |
| Status | implemented |
| Created | 2026-02-20 |
| Updated | 2026-02-20 |

## Version History

| Version | Date | Summary |
|---------|------|---------|
| v1 | 2026-02-20 | Initial: MCP server for node ops (SSH direct + Center API discovery) + ops skill. Scrum-refined: removed sudo, added stdout redaction, dual-arch skill, stdin pipe for scripts. |

## Product Requirements

### 核心理念

- 用 MCP Server + Skill 替代 Web Dashboard 的批量脚本系统，实现 AI 驱动的服务器运维
- MCP Server 提供原子能力（SSH 直连 + API 查询），Skill 提供运维知识和安全护栏
- 节点操作通过 SSH 直连（不绕 Center API），消除中间人延迟
- Center API 仅用于节点发现（`X-Access-Key` 认证，复用已有 admin 机制）
- 现有 Node/Tunnel model 不做任何变动
- Cloud 相关工具延后实现（见 `docs/todos/kaitu-ops-mcp-cloud-tools.md`）

### MCP 工具集（2 个工具）

1. **`list_nodes`** — 从 Center API 获取所有节点列表，包含关联隧道信息
   - 可选过滤：country, name
   - 返回：`[{name, ipv4, ipv6, country, region, tunnels: [{domain, protocol, port, serverUrl}]}]`
   - 数据源：`GET /app/nodes/batch-matrix`（MCP 侧过滤，只提取必要字段，丢弃批量脚本矩阵数据）
   - 认证：`X-Access-Key` header → Center API `handleAccessKeyAuth()` → `AdminRequired()`

2. **`exec_on_node`** — SSH 直连节点执行任意命令
   - 输入：ip, command, timeout (default 30s)
   - 返回：`{stdout, stderr, exitCode, truncated}`
   - stdout 超过 10000 字符时截断，`truncated=true`
   - **stdout redaction**：返回前自动过滤敏感模式（`K2_NODE_SECRET=*`、`SECRET=`、64 位 hex 字符串等），替换为 `[REDACTED]`
   - **脚本传输**：通过 ssh2 stdin pipe 传输文件内容，无需 shell 转义。MCP 实现读取本地文件 → pipe 到远程 `bash -s`
   - 覆盖所有节点操作场景：健康检查、日志查看、服务重启、文件操作、配置更新
   - SSH 统一用 root 用户（sudo 无意义，已删除 sudo 参数）

### Skill 文件（运维安全护栏）

Skill 文件 `.claude/skills/kaitu-node-ops.md` 编码节点基础设施的领域知识，作为 Claude 使用 MCP 工具时的运维安全护栏。

**定位**：Skill 是防误操作的最佳实践指导，不是安全边界。admin 本身拥有完全的 SSH root 权限。Skill 防止 AI 在运维过程中无意执行危险操作（如意外打印密钥、`docker compose down` 导致中断）。真正的安全由 MCP 层的 stdout redaction 提供技术保证。

#### 架构识别（双版本兼容）

线上同时存在两种架构的节点，Skill 教 Claude 先识别再操作：

**识别方法**：`docker ps --format '{{.Names}}'`
- 看到 `k2v5` → **新版架构（k2v5 前门）**
- 看到 `k2-slave` → **旧版架构（k2-slave SNI 路由）**

**新版架构（主体描述）**：
- 部署路径 `/apps/kaitu-slave/`
- 4 个容器，依赖链严格：
  ```
  k2-sidecar (bridge) ──healthy──→ k2v5 (host network, :443 TCP+UDP)
                       ──healthy──→ k2v4-slave (bridge, :K2V4_PORT)
                       ──healthy──→ k2-oc (bridge, :K2OC_PORT)
  ```
- k2-sidecar：注册、配置生成、RADIUS 代理、健康上报。就绪标志 `/etc/kaitu/.ready`
- k2v5：ECH 前门，占用 443 端口。ECH 流量自己处理；非 ECH 按 SNI 转发到 k2v4/k2-oc
- k2v4-slave：旧版 TCP-WS 隧道，接收 k2v5 转发的非 ECH 流量
- k2-oc：OpenConnect 隧道，RADIUS 认证走 sidecar

**旧版架构（兼容说明）**：
- 部署路径同 `/apps/kaitu-slave/`
- 容器名不同：`k2-slave-sidecar`, `k2-slave`, `k2-oc`
- 镜像名不同：`k2-slave-sidecar:latest`, `k2-slave:latest`
- k2-slave 使用 host network + SNI 路由（无 ECH）
- 操作命令相同，只是容器名替换

#### 环境配置

- `.env` 文件在 `/apps/kaitu-slave/.env`，核心变量：
  - `K2_NODE_SECRET` — 节点认证密钥（**不可读取、显示、修改**）
  - `K2_DOMAIN` — 隧道域名（通配符格式 `*.example.com`）
  - `K2V4_PORT` — k2v4 容器端口（默认 8443）
  - `K2OC_ENABLED` / `K2OC_DOMAIN` / `K2OC_PORT` — OpenConnect 配置
  - `K2_HOP_PORT_MIN` / `K2_HOP_PORT_MAX` — 跳端口范围（iptables DNAT 到 443）
  - `K2_CENTER_URL` — Center API 地址（默认 `https://k2.52j.me`）
  - `K2_LOG_LEVEL` — 日志级别
  - `K2_NODE_BILLING_START_DATE` / `K2_NODE_TRAFFIC_LIMIT_GB` — 流量监控

#### 标准运维操作

Skill 定义以下标准操作模式：

| 操作 | 命令（容器名按识别结果替换） |
|------|------|
| 识别架构版本 | `docker ps --format '{{.Names}}'` |
| 查看所有容器状态 | `cd /apps/kaitu-slave && docker compose ps` |
| 查看特定容器日志 | `docker logs --tail 100 {container_name}` |
| 拉取最新镜像并重启 | `cd /apps/kaitu-slave && docker compose pull && docker compose up -d` |
| 仅重启某个容器 | `docker restart {container_name}` |
| 查看 .env 配置 | `cat /apps/kaitu-slave/.env` |
| 健康检查 | `docker inspect --format='{{.State.Health.Status}}' {sidecar_name}` |
| 查看磁盘/内存/CPU | `df -h && free -h && top -bn1 \| head -5` |
| 查看网络连接数 | `ss -s` |
| 查看 IPv6 状态 | `ip -6 addr show scope global` |
| 检查 iptables hop 规则 | `iptables -t nat -L PREROUTING -n \| grep -E "10020\|REDIRECT"` |

#### 运维安全护栏

Skill 编码以下最佳实践规则。这些是防误操作的护栏，不是技术强制的安全边界：

1. **K2_NODE_SECRET 不可触碰** — 绝不读取、显示、修改、传输节点密钥（MCP 层 stdout redaction 提供技术兜底）
2. **不删除 /apps/kaitu-slave/ 目录** — 这是节点的全部部署
3. **不修改 docker-compose.yml** — 配置变更只通过 `.env` 文件
4. **重启前确认** — 重启容器前先 `docker compose ps` 确认当前状态
5. **不直接操作 /etc/kaitu/** — 这是 sidecar 自动生成的配置，手动改会被覆盖
6. **不碰 iptables 规则** — hop port DNAT 由 k2v5/k2-slave entrypoint 自动管理
7. **更新 = pull + up** — 更新镜像只用 `docker compose pull && docker compose up -d`，不要 `docker compose down`

#### 脚本执行模式

Skill 记录两种脚本执行方式：

1. **小命令（几行）** — 直接通过 `exec_on_node` 的 command 参数执行
2. **大脚本（文件）** — 通过 `exec_on_node` 的 stdin pipe 传输。MCP 实现读取本地 `docker/scripts/` 下的脚本文件，通过 ssh2 stdin pipe 安全推送到节点执行（无需 shell 转义）

#### 附带脚本库

Skill 引用 `docker/scripts/` 下的运维脚本：

| 脚本 | 用途 | 注意 |
|------|------|------|
| `totally-reinstall-docker.sh` | 全量重装 Docker CE（清理旧版 + nftables + IPv6） | 破坏性操作，需要用户确认 |
| `enable-ipv6.sh` | 启用 IPv6 内核参数 + 测试连通性 | 需要 sudo |
| `simple-docker-pull-restart.sh` | 拉取最新镜像并重启 | 日常更新用 |

### 认证配置

- MCP 协议层（stdio）不需要认证 — 本地进程
- Center API 认证：`X-Access-Key` header（复用已有 `handleAccessKeyAuth()`，User 表 `AccessKey` 字段，需 `IsAdmin=true`）
- SSH 认证：使用当前用户默认密钥（`~/.ssh/id_rsa` 或 `~/.ssh/id_ed25519`），可通过环境变量 `KAITU_SSH_KEY` 或配置文件覆盖
- 配置文件路径：`~/.kaitu-ops/config.toml`（可选，环境变量优先）

```toml
# ~/.kaitu-ops/config.toml (可选，环境变量覆盖)
# 环境变量: KAITU_CENTER_URL, KAITU_ACCESS_KEY, KAITU_SSH_KEY, KAITU_SSH_USER, KAITU_SSH_PORT

[center]
url = "https://api.kaitu.io"
access_key = "admin-user-access-key"    # User.AccessKey (IsAdmin=true)

[ssh]
private_key_path = "~/.ssh/id_rsa"      # 默认用当前用户 SSH 密钥
user = "root"
port = 22
```

### 安装与集成

- MCP Server 代码位于项目内 `tools/kaitu-ops-mcp/`
- Skill 文件位于 `.claude/skills/kaitu-node-ops.md`
- 脚本文件位于 `docker/scripts/`
- Claude Code MCP 配置在项目级 `.claude/settings.json`

## Technical Decisions

### 1. 实现语言：TypeScript

**决策**: 使用 TypeScript + `@modelcontextprotocol/sdk`

**原因**:
- Claude Code 生态已有 Node.js 运行时，无需额外依赖
- MCP TypeScript SDK 最成熟
- SSH 使用 `ssh2` 库（成熟稳定）

### 2. MCP + Skill 分层

**决策**: MCP 提供原子工具 + stdout redaction（技术安全层），Skill 提供领域知识 + 运维护栏（行为指导层）

**原因**:
- MCP tool description 只能写一两句话，无法承载运维知识
- Skill 编码节点架构、容器依赖链、标准操作流程
- MCP = 手（能力）+ 安全手套（redaction），Skill = 经验（知识）
- Skill 是防误操作护栏，不是安全边界。admin 拥有完全 SSH root 权限

### 3. Center API 认证：X-Access-Key

**决策**: 复用已有 `X-Access-Key` 认证机制

**原因**:
- Center API `middleware.go` 已实现 `handleAccessKeyAuth()` — 通过 `X-Access-Key` header 查找 User 表
- `AdminRequired()` 随后检查 `IsAdmin=true`
- 无需改动任何后端代码
- AccessKey 是长期有效的字符串（不像 JWT 会过期）

### 4. SSH 直连 + 默认密钥

**决策**: 节点操作 SSH 直连，使用当前用户默认 SSH 密钥或环境变量指定

**原因**:
- 零延迟实时输出，不经过 Center → Asynq → Worker 的排队
- admin 本地已有可用 SSH 密钥（或通过环境变量配置）
- 无需密钥分发 bootstrap 步骤

### 5. 输出截断 + Redaction

**决策**: stdout 默认截断到 10000 字符 + 自动 redact 敏感模式

**原因**:
- MCP tool 返回值需要合理大小（Claude Code 上下文限制）
- 返回 `truncated: bool` 让 AI 知道输出被截断
- stdout redaction 过滤 `K2_NODE_SECRET=*` 等模式，防止密钥意外泄露到对话历史
- 这是技术层面的安全保证，比 Skill 提示词可靠

### 6. SSH 连接管理

**决策**: 每次 tool call 创建新连接，不做连接池

**原因**:
- tool call 频率低（人类交互节奏），连接池收益小
- 避免 stale connection 管理复杂度

### 7. 脚本传输：stdin pipe

**决策**: 大脚本通过 ssh2 stdin pipe 传输，不用 shell heredoc

**原因**:
- heredoc 需要处理 shell 转义（单引号、反引号、`$` 变量），AI 生成容易出 bug
- ssh2 的 stdin pipe 是 binary-safe 的，不经过 shell 解析
- 实现：MCP 读取本地文件 → pipe 到 `bash -s`

### 8. 项目结构

**决策**: MCP Server 在 `tools/kaitu-ops-mcp/`，Skill 在 `.claude/skills/`，脚本在 `docker/scripts/`

```
tools/kaitu-ops-mcp/
├── package.json
├── tsconfig.json
├── src/
│   ├── index.ts          # MCP Server entry point
│   ├── tools/
│   │   ├── list-nodes.ts
│   │   └── exec-on-node.ts
│   ├── ssh.ts            # SSH connection + stdin pipe helper
│   ├── redact.ts         # stdout redaction (SECRET patterns)
│   ├── center-api.ts     # Center API client (X-Access-Key auth)
│   └── config.ts         # Config: TOML file + env var fallback

.claude/
├── settings.json         # MCP server 注册
└── skills/
    └── kaitu-node-ops/
        ├── SKILL.md              # 运维安全护栏
        ├── deploy-compose.sh     # SCP 部署 docker-compose.yml
        └── update-compose.sh     # 批量 pull + restart

docker/scripts/           # 运维脚本
├── totally-reinstall-docker.sh
├── enable-ipv6.sh
└── simple-docker-pull-restart.sh
```

## Acceptance Criteria

### MCP Server

- AC1: `list_nodes` 通过 `X-Access-Key` 从 Center API 获取节点列表，MCP 侧过滤只返回必要字段（name, ip, country, region, tunnels），可按 country/name 过滤
- AC2: `exec_on_node` 通过 SSH 直连节点执行命令，返回 stdout/stderr/exitCode
- AC3: `exec_on_node` stdout 超过 10000 字符时截断并标记 `truncated=true`
- AC4: `exec_on_node` stdout 返回前自动 redact 敏感模式（K2_NODE_SECRET 等）
- AC5: `exec_on_node` 支持自定义 timeout
- AC6: SSH 默认使用当前用户密钥，支持 `KAITU_SSH_KEY` 环境变量和配置文件覆盖
- AC7: Center API 使用 config 或 `KAITU_ACCESS_KEY` 环境变量的 access_key 通过 `X-Access-Key` header 认证
- AC8: 配置缺失时（无 config 文件且无环境变量），MCP Server 启动报告清晰的配置缺失错误
- AC9: SSH 连接失败时返回明确的错误信息（IP 不可达、认证失败等）
- AC10: MCP Server 可被 Claude Code 通过 stdio 正常发现和调用

### Skill 文件

- AC11: Skill 包含架构识别流程（`docker ps` 判断新版 k2v5 / 旧版 k2-slave）
- AC12: Skill 主体描述新版 k2v5 架构（4 容器、依赖链、网络模式），附带旧版 k2-slave 兼容映射
- AC13: Skill 包含 `.env` 所有核心变量的说明
- AC14: Skill 包含标准运维操作命令表（容器名按架构版本替换）
- AC15: Skill 包含 7 条运维安全护栏，明确定位为防误操作最佳实践（非安全边界）
- AC16: Skill 记录两种脚本执行模式（小命令直接 exec / 大脚本 stdin pipe）
- AC17: Skill 引用 `docker/scripts/` 下的脚本及使用注意事项

### 脚本

- AC18: `~/Downloads/scripts/` 中的脚本整理到 `docker/scripts/`，修正文件名

## Testing Strategy

- 单元测试：config 解析（TOML + env fallback）、API response 字段过滤、stdout 截断、stdout redaction
- 集成测试：mock SSH server 验证 exec 行为和 stdin pipe 脚本传输
- 手动测试：安装到本地 Claude Code，验证 2 个 MCP 工具均可正常调用
- 不需要 CI — 这是私有运维工具

## Deployment & CI/CD

- 构建：`cd tools/kaitu-ops-mcp && npm install && npm run build`
- Claude Code 配置：项目级 `.claude/settings.json` 注册 MCP server
- 用户配置：设置 `KAITU_ACCESS_KEY` 和 `KAITU_SSH_KEY` 环境变量，或创建 `~/.kaitu-ops/config.toml`
- 无 CI/CD — 开发者手动构建
