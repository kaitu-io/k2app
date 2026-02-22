# Feature: k2 CLI Redesign (nginx 模型)

## Meta

| Field     | Value                                    |
|-----------|------------------------------------------|
| Feature   | k2-cli-redesign                          |
| Version   | v2                                       |
| Status    | in-progress                              |
| Created   | 2026-02-21                               |
| Updated   | 2026-02-22                               |

## Version History

| Version | Date       | Summary                                              |
|---------|------------|------------------------------------------------------|
| v1      | 2026-02-21 | Initial: nginx 风格 CLI 重构 (k2 + k2s)              |
| v2      | 2026-02-22 | `k2 ctl` 平台感知错误提示（scrum 决议，否决 auto 行为）|

## Overview

参考 nginx 的设计模式，重构 k2（客户端）和 k2s（服务端）的 CLI 架构。核心变化：

1. **前台优先** — 二进制本身就是前台进程，daemon 化交给 OS 服务管理器
2. **配置文件驱动** — 标准默认路径搜索链，不再强制 `-c`/`-s` 指定
3. **信号控制** — `-s stop|reload` 通过 PID 文件控制运行中实例
4. **配置验证** — `-t` 验证配置不启动
5. **setup 引导** — `k2 setup` / `k2s setup` 一键首次部署
6. **职责分离** — 服务管理独立为 `service` 子命令，不再藏在 `run` flag 里

## Product Requirements

- PR1: CLI 用户能直观理解每个命令的含义，无隐式行为（smart mode、auto-install）
- PR2: 新用户通过 `k2 setup <url>` 一步完成首次部署，体验不退化
- PR3: 高级用户能手动控制每一步（配置、安装、启动）
- PR4: 配置变更可以热重载，不需要重启 daemon
- PR5: 配置文件错误能在启动前被发现
- PR6: 容器环境直接运行二进制，无需额外处理

## Technical Decisions

### TD1: 二进制即前台进程（nginx 模型）

`k2` 和 `k2s` 运行时永远是前台进程。后台运行由 systemd/launchd/SC 管理。

理由：
- 现代服务管理（systemd Type=simple、launchd KeepAlive、Docker）都期望前台进程
- 自己实现 daemon fork 是历史遗留模式，增加复杂性
- 日志天然输出到 stderr，服务管理器负责收集
- 容器场景零适配

删除项：
- `k2 run` 子命令 → `k2` 本身就是进程
- `k2 run --install` → 拆为 `k2 setup` / `k2 service install`
- `k2 run -c`（独立前台） → `k2 -c config` 就是前台 daemon
- `k2s run --foreground` → `k2s` 本身就是前台
- `k2s run`（smart mode） → `k2s setup`

### TD2: 标准配置文件搜索链

启动时按优先级搜索，找到第一个即用：

```
k2:   ./k2.yaml → ~/.config/k2/k2.yaml → /etc/k2/k2.yaml
k2s:  ./k2s.yaml → /etc/k2s/k2s.yaml
```

`-c` 显式指定时跳过搜索链。未找到任何配置文件时使用内置默认值启动（k2 daemon idle 等待 ctl up 指令，k2s 报错退出要求配置或 setup）。

### TD3: PID 文件

k2 写 PID 文件用于 `-s` 信号控制：

```
root 运行:  /var/run/k2.pid   |  /var/run/k2s.pid
用户运行:  ~/.config/k2/k2.pid
```

PID 文件路径可通过配置 `pid_file:` 覆盖。

`-s stop` → 读 PID → 发 SIGTERM
`-s reload` → 读 PID → 发 SIGHUP

### TD4: 热重载分层

配置项分为静态和动态两类：

**k2 daemon — 动态（reload 生效）：**
- `server` — 连接目标
- `dns` — DNS 配置
- `rule` — 路由规则
- `log.level` — 日志级别

**k2 daemon — 静态（需重启）：**
- `listen` — daemon API 地址
- `mode` — tun/proxy
- `tun` — TUN 设备配置
- `proxy.listen` — 代理监听地址
- `pid_file`

**k2s — 动态（reload 生效）：**
- `rate` — 限速
- `auth` — 认证配置
- `log.level`

**k2s — 静态（需重启）：**
- `listen` — 监听端口
- `tls` — 证书
- `ech` — ECH 配置
- `cert_dir`
- `pid_file`

daemon 收到 SIGHUP 后：重新读取配置文件 → 校验 → 仅应用动态段 → 若动态段包含 server 变更则触发重连。

### TD5: `k2 ctl` 保留 IPC 控制

桌面端（Tauri）通过 HTTP API 控制 daemon，此机制不变。CLI 用户通过 `k2 ctl` 访问同一 HTTP API：

```
k2 ctl up [URL|config.yaml]   # 连接（覆盖配置文件中的 server）
k2 ctl down                   # 断开
k2 ctl status                 # 状态
```

`k2 ctl up <url>` = 向 daemon 发 IPC up 指令，daemon 临时使用该 URL 连接，不修改配置文件。这保持了 `k2 up <url>` 的便捷性。

HTTP API 接口 (`POST /api/core`) 不变。

### TD6: `setup` 引导命令

**`k2 setup [URL] [-c config]`：**
1. 生成配置文件到标准路径（URL → 写入 `server:` 字段，`-c` → 复制到标准路径）
2. 调用 `k2 -t` 验证
3. 安装系统服务（等价 `k2 service install`）
4. 启动服务
5. 打印后续操作提示

**`k2s setup [-c config]`：**
1. `cert_dir` 如为空则创建 `/etc/k2s/`
2. 自动生成证书、ECH 配置、auth token、connect URL
3. 写配置到标准路径
4. 安装系统服务
5. 启动服务
6. 打印 connect URL

setup 是 `配置生成 + service install + start` 的组合，每一步都可单独执行。

### TD7: Service 模板

系统服务模板随安装包提供，`k2 service install` 等价于复制模板到系统路径并启用：

**systemd (Linux):**
```ini
[Unit]
Description=k2 network tunnel
After=network.target

[Service]
Type=simple
ExecStart=/usr/bin/k2
ExecReload=/bin/kill -HUP $MAINPID
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
```

**launchd (macOS):**
```xml
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>io.kaitu.k2</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/k2</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/var/log/k2/k2.log</string>
    <key>StandardErrorPath</key>
    <string>/var/log/k2/k2.log</string>
</dict>
</plist>
```

注意：不再有 `run --foreground`，因为二进制本身就是前台。服务模板直接调 `k2` / `k2s`。

### TD8: `k2 ctl` 平台感知错误提示（v2 新增）

**问题**: v1 的 `k2 ctl up` 在 daemon 未运行时打印通用错误 `"Is the daemon running? Start it with: k2"`，用户无法区分是 service 没装还是没启动，也不知道平台对应的操作命令。

**Scrum 决议（2026-02-22）**: 对三个合并提议逐一辩论后，全部否决自动行为：

| 提议 | 裁定 | 理由 |
|------|------|------|
| `ctl up` 合并 `service install` | ❌ 否决 | 权限升级（需 root）、隐式副作用、`k2 setup` 已覆盖 |
| `ctl down` 合并 `service uninstall` | ❌ 否决 | 语义错误（断连≠卸载）、破坏开机自启、频繁 up/down 场景灾难 |
| `ctl up` 自动 `service start` | ❌ 否决（二次审议） | service start 某些平台也需 root；增加复杂度（IsInstalled/Start/重试）；清晰提示更符合 Unix 哲学 |

**最终方案**: 不加任何自动行为，改善错误信息——根据平台给出精确的 next step 命令。

**错误提示设计**:

```
k2 ctl up（daemon 未运行时）:

  macOS:
    Error: daemon not running

      First time?    k2 setup <URL>
      Already set up? sudo launchctl start kaitu

  Linux:
    Error: daemon not running

      First time?    k2 setup <URL>
      Already set up? sudo systemctl start k2

  Windows:
    Error: daemon not running

      First time?    k2 setup <URL>
      Already set up? sc start k2
```

`k2 ctl down` 和 `k2 ctl status` 同理，daemon 不可达时打印平台相关提示。

**设计原则**:
- `ctl` 命令永远只做一件事：IPC 通信。行为完全可预测
- 不需要 `--no-autostart` flag（没有自动行为）
- 不需要新增 `IsInstalled()` / `Start()` 方法
- 错误提示通过编译时 build tag 选择平台对应文案（`ctl_hint_darwin.go`、`ctl_hint_linux.go`、`ctl_hint_windows.go`）

**交互模型总结**:

```
首次使用:
  k2 setup <URL>              → 生成配置 + install service + start + connect

日常使用:
  k2 ctl up [URL|config]      → IPC 连接（daemon 必须已运行）
  k2 ctl down                 → disconnect (service stays running)
  k2 ctl status               → show state

运维管理:
  k2 service install           → 注册系统服务
  k2 service uninstall         → 卸载系统服务

开发调试:
  k2 (no args)                 → 前台 daemon
  k2 ctl up --pid <PID>       → 跟随进程生命周期
```

## Design

### 完整 CLI 接口

**k2（客户端）：**
```
k2                             # 前台运行 daemon，读默认配置
k2 -c /path/k2.yaml           # 指定配置文件
k2 -t [-c config]             # 验证配置
k2 -s stop                    # 优雅停止（SIGTERM via PID）
k2 -s reload                  # 热重载配置（SIGHUP via PID）
k2 -v                         # 版本

k2 setup [URL|-c config]      # 首次引导
k2 ctl up [URL|config.yaml]   # 连接 VPN（IPC）
k2 ctl down                   # 断开（IPC）
k2 ctl status                 # 状态（IPC）
k2 service install             # 安装系统服务
k2 service uninstall           # 卸载系统服务
k2 upgrade [--check]           # 升级
k2 demo-config                 # 打印示例配置
```

**k2s（服务端）：**
```
k2s                            # 前台运行服务器，读默认配置
k2s -c /path/k2s.yaml         # 指定配置文件
k2s -t [-c config]            # 验证配置
k2s -s stop                   # 优雅停止
k2s -s reload                 # 热重载配置
k2s -v                        # 版本

k2s setup [-c config]         # 首次引导（auto-provision + 安装服务）
k2s service install            # 安装系统服务
k2s service uninstall          # 卸载系统服务
k2s demo-config                # 打印示例配置
```

### 配置文件结构

**k2.yaml（客户端）：**
```yaml
# === 静态配置（需重启） ===
listen: "127.0.0.1:1777"       # daemon API 地址
pid_file: /var/run/k2.pid
mode: tun                       # tun | proxy

tun:
  ipv4: "198.18.0.7/15"
  ipv6: "fdfe:dcba:9876::7/64"

proxy:
  listen: "127.0.0.1:1080"

# === 动态配置（reload 生效） ===
server: "k2v5://..."            # 可选，预设连接目标

dns:
  direct:
    - "114.114.114.114:53"
    - "223.5.5.5:53"
  proxy:
    - "8.8.8.8:53"
    - "1.1.1.1:53"

rule:
  global: false

log:
  level: info                   # debug | info | warn | error
  output: stderr                # stderr | stdout | file path

```

**k2s.yaml（服务端）：**
```yaml
# === 静态配置（需重启） ===
listen: ":443"
pid_file: /var/run/k2s.pid
cert_dir: /etc/k2s

tls:
  cert: /etc/k2s/cert.pem
  key: /etc/k2s/key.pem

ech:
  provider: local

pcc: ""

# === 动态配置（reload 生效） ===
rate: 0

auth:
  remote_url: ""
  cache_ttl: 5m

log:
  level: info
  output: stderr
```

### 进程启动流程

```
k2 [-c config] 启动流程：
  1. 解析 flag（-c, -t, -s, -v）
  2. -v → 打印版本，退出
  3. -t → 加载配置 + Validate() → 打印结果，退出
  4. -s stop|reload → 读 PID 文件 → 发信号，退出
  5. 默认：加载配置（搜索链或 -c 指定）
  6. Validate() 配置
  7. 写 PID 文件
  8. 注册信号处理：SIGTERM → graceful stop, SIGHUP → reload
  9. 如果配置有 server: → 自动连接
  10. 启动 HTTP API（listen 地址）
  11. 阻塞等待退出信号
```

### 代码文件规划

**k2/cmd/k2/（重构后）：**
```
main.go          # flag 解析 + 分发（-t, -s, -v, 子命令）
process.go       # 前台 daemon 启动流程（PID、信号、HTTP API）
config.go        # 配置搜索链、加载、Validate()
signal.go        # -s stop|reload 实现（读 PID、发信号）
ctl.go           # k2 ctl up|down|status（IPC 客户端）
setup.go         # k2 setup 引导逻辑
service.go       # k2 service install|uninstall（调 service_*.go）
service_darwin.go
service_linux.go
service_windows.go
upgrade.go       # k2 upgrade（原 cmd_upgrade.go）
```

**k2/cmd/k2s/（重构后）：**
```
main.go          # flag 解析 + 分发
process.go       # 前台服务器启动流程
config.go        # 配置搜索链、加载、Validate()
signal.go        # -s stop|reload
setup.go         # k2s setup（auto-provision + 服务安装）
service.go       # k2s service install|uninstall
service_darwin.go
service_linux.go
service_windows.go
```

**k2/config/（新增）：**
```
search.go        # 配置文件搜索链逻辑
validate.go      # Validate() — 结构校验 + 语义校验
pid.go           # PID 文件读写
```

**k2/daemon/（修改）：**
```
reload.go        # SIGHUP handler：重新读取配置 → 应用动态段
```

### 对现有系统的影响

| 系统 | 影响 |
|------|------|
| **desktop (Tauri)** | 详见下方 Tauri 桌面端适配 |
| **webapp** | `vpn.store.ts` 注释引用了 `svc up`，需更新注释 |
| **mobile** | 无影响（不走 daemon） |
| **k2/daemon HTTP API** | 接口不变（POST /api/core），新增 reload 能力 |
| **k2/engine** | 无影响 |
| **CI/CD** | release 脚本更新二进制名不变，调整安装后脚本 |
| **Docker 部署** | `ENTRYPOINT ["k2s"]` 或 `ENTRYPOINT ["k2s", "-c", "/etc/k2s/k2s.yaml"]`，比原来更自然 |

### Tauri 桌面端适配

Tauri 通过 Rust `std::process::Command` 调用 k2 二进制（非 Tauri shell/sidecar API），因此不需要修改 Tauri capability 权限配置。改动集中在三处：

#### 1. `service.rs` — admin_reinstall_service()

Tauri 启动时通过 `ensure_service_running()` 检查 daemon 状态，版本不匹配或未运行时调用 `admin_reinstall_service()` 以管理员权限安装服务。

**macOS（osascript 提权）：**
```rust
// 现状
let script = format!(
    r#"do shell script "{} run --install" with administrator privileges"#,
    service_path
);

// 改为
let script = format!(
    r#"do shell script "{} service install" with administrator privileges"#,
    service_path
);
```

**Windows（PowerShell UAC 提权）：**
```rust
// 现状
let ps_script = format!(
    r#"Start-Process -FilePath '{}' -ArgumentList 'run','--install' -Verb RunAs -Wait -WindowStyle Hidden"#,
    service_path.display()
);

// 改为
let ps_script = format!(
    r#"Start-Process -FilePath '{}' -ArgumentList 'service','install' -Verb RunAs -Wait -WindowStyle Hidden"#,
    service_path.display()
);
```

其余逻辑（ping、版本检查、等待服务启动、旧服务清理）不变。

#### 2. `installer-hooks.nsh` — NSIS Windows 安装器

当前 NSIS hooks 使用 `k2.exe svc up/down`，但 k2 二进制实际不存在 `svc` 子命令（历史遗留，参见 `k2app-rewrite.md` 第 185 行说明）。本次重构一并修正：

```nsis
; PREINSTALL — 卸载旧服务
; 现状（命令不存在，静默失败）:
nsExec::ExecToStack '"$INSTDIR\${SERVICE_EXE}" svc down'
; 改为:
nsExec::ExecToStack '"$INSTDIR\${SERVICE_EXE}" service uninstall'

; POSTINSTALL — 安装新服务
; 现状:
nsExec::ExecToStack '"$INSTDIR\${SERVICE_EXE}" svc up'
; 改为:
nsExec::ExecToStack '"$INSTDIR\${SERVICE_EXE}" service install'

; PREUNINSTALL — 卸载前清理
; 现状:
nsExec::ExecToStack '"$INSTDIR\${SERVICE_EXE}" svc down'
; 改为:
nsExec::ExecToStack '"$INSTDIR\${SERVICE_EXE}" service uninstall'
```

`k2 service uninstall` 应保证：先停止 VPN 连接 → 停止系统服务 → 删除服务定义。即完整替代原 `svc down` 的预期语义。

NSIS hooks 中的注释也需同步更新（文件头 `svc up/down` 说明 → `service install/uninstall`）。

#### 3. 不涉及的部分

| 组件 | 说明 |
|------|------|
| **Tauri capabilities** | 不需要修改。`service.rs` 使用 `std::process::Command`，不经过 Tauri shell API |
| **Tauri sidecar** | k2 不是 sidecar，是独立安装的系统级二进制 |
| **HTTP IPC** | `daemon_exec` → `POST /api/core` 不变，`ensure_service_running` 的 ping/version 检查逻辑不变 |
| **macOS PKG 安装器** | `build-macos.sh` 不含 post-install 服务安装脚本，服务由 Tauri 启动时 `ensure_service_running()` 按需安装 |
| **tray.rs** | 托盘菜单通过 HTTP API 控制 VPN，不直接调用 k2 二进制 |

### 迁移兼容

过渡期可保留 `k2 run` 子命令作为 alias，打印 deprecation warning 后转发到主进程逻辑：

```
$ k2 run
⚠ 'k2 run' is deprecated, use 'k2' directly.
  Service install: k2 service install
  See: k2 --help
```

`k2 up/down/status` 同理，打印 warning 后转发到 `k2 ctl up/down/status`。

## Acceptance Criteria

### AC1: 前台运行
- `k2` 启动后进程保持前台运行
- Ctrl+C (SIGINT) 优雅退出
- SIGTERM 优雅退出
- 不存在任何自动 fork/daemonize 行为

### AC2: 配置文件搜索
- 无 `-c` 时按搜索链查找配置
- `-c` 指定时使用指定文件
- 未找到配置时使用内置默认值启动（k2 daemon idle，k2s 报错）
- 配置文件不存在时的错误信息清晰指向搜索路径

### AC3: 配置验证
- `k2 -t` 加载配置 + 校验 + 打印结果 + 不启动
- 校验覆盖：YAML 语法、必填字段、server URL 格式、listen 地址格式
- 校验通过打印 "configuration file /path/k2.yaml test is successful"
- 校验失败打印具体错误和行号

### AC4: 信号控制
- `k2 -s stop` 读 PID 文件发 SIGTERM，目标进程优雅退出
- `k2 -s reload` 读 PID 文件发 SIGHUP，目标进程重载动态配置
- PID 文件不存在或进程不存在时给出明确错误
- reload 后动态配置生效，静态配置不变

### AC5: PID 文件
- daemon 启动时写 PID 文件
- daemon 退出时删除 PID 文件
- 已有 PID 文件且进程存活时拒绝启动（防重复启动）

### AC6: setup 引导
- `k2 setup <url>` 生成配置 + 安装服务 + 启动，一步完成
- `k2s setup` 自动生成证书/ECH + 生成配置 + 安装服务 + 打印 connect URL
- setup 每一步失败时给出明确错误和手动操作指引
- setup 幂等：重复执行不破坏已有配置（提示已存在，询问覆盖）

### AC7: ctl 命令
- `k2 ctl up <url>` 通过 IPC 连接 VPN，行为等价原 `k2 up <url>`
- `k2 ctl down` 等价原 `k2 down`（仅断开隧道，daemon 保持运行）
- `k2 ctl status` 等价原 `k2 status`
- daemon 不可达时打印平台感知的错误提示（含 `k2 setup` 引导 + 平台对应的 service start 命令）
- 错误提示通过 build tag 区分平台（macOS: `launchctl start`、Linux: `systemctl start`、Windows: `sc start`）
- 不含任何自动 start/install 行为——ctl 只做 IPC 通信

### AC8: service 管理
- `k2 service install` 在当前平台安装系统服务（systemd/launchd/SC）
- `k2 service uninstall` 卸载
- 安装时检测已有服务并提示
- 卸载时停止运行中的服务

### AC9: 热重载
- SIGHUP 触发配置重载
- 动态配置段（server, dns, rule, rate, auth, log.level）即时生效
- 静态配置段变更时日志 warning 提示需重启
- reload 过程中 HTTP API 不中断

### AC10: 向后兼容
- `k2 run`、`k2 up`、`k2 down`、`k2 status` 保留为 deprecated alias
- deprecated 命令执行时打印 warning + 转发到新命令
- `k2s run` 保留为 deprecated alias
- deprecated alias 计划在 v2.0 移除

### AC11: Tauri 桌面端适配
- `service.rs` 中 `admin_reinstall_service()` 调用 `k2 service install`（非 `run --install`）
- macOS osascript 提权 + Windows PowerShell UAC 提权路径均正确执行
- NSIS installer-hooks 使用 `k2.exe service install/uninstall`（非 `svc up/down`）
- `ensure_service_running()` 流程不变：ping → version check → admin install if needed
- Tauri capabilities 无需修改（使用 std::process::Command，非 Tauri shell API）
