# CLI Redesign: Machine-Friendly k2/k2s

## Problem

k2/k2s CLI 有三个结构性缺陷，导致 AI 和自动化工具无法使用：

1. 两套命令风格混在一起（`-s stop` 和 `ctl down` 都是断开），AI 不知道用哪个
2. 客户端必须先装系统服务再连接，没有一步到位的连接命令
3. 所有输出都是中英双语人类文本，AI 无法解析

## 设计原则

- 所有操作都是 `k2 <动词>` 或 `k2 <名词> <动词>`，没有例外
- 一条命令完成一件事，不需要多步操作
- `--json` 输出结构化数据，AI 直接用
- 不考虑向后兼容，干净重来

## k2 命令表

```
k2 connect <URL>                    前台直连，默认 proxy 模式，Ctrl+C 停止
k2 connect <URL> --mode tun         前台直连，TUN 模式（需要 root）
k2 connect <URL> --listen addr      自定义代理监听地址

k2 up [URL|config.yaml]             通过后台 daemon 连接
k2 down                             断开
k2 status                           连接状态

k2 setup <URL>                      生成配置 + 安装服务 + 启动
k2 run [-c config]                  启动 daemon（系统服务入口）

k2 config test [-c config]          测试配置
k2 config show                      打印当前配置

k2 service install                  安装系统服务              [需要 root]
k2 service uninstall                卸载系统服务              [需要 root]

k2 upgrade [--check]                自动更新
k2 version                          版本信息
k2 help [command]                   帮助
```

去掉的东西：
- `k2`（裸调用）→ 必须用 `k2 run`
- `k2 -s stop|reload` → 用 `k2 down` 或 kill
- `k2 -t` → 用 `k2 config test`
- `k2 -c` → 用 `k2 run -c`
- `k2 -v` → 用 `k2 version`
- `k2 ctl up|down|status` → 去掉 `ctl` 前缀，直接 `k2 up|down|status`
- `k2 demo-config` → 用 `k2 config show`

## k2s 命令表

```
k2s run                             部署服务器（自动配置+装服务+打印 URL+退出）
k2s run --foreground                前台运行（开发调试用）
k2s run --foreground -c config.yml  前台运行，指定配置

k2s setup                           交互式向导（人类用）
k2s setup --port 443 --username alice --no-firewall
                                    全自动部署（AI 用，不弹任何提示）

k2s user add <name>                 添加用户，打印连接 URL
k2s user list                       列出用户
k2s user del <name>                 删除用户
k2s user reset <name>               重置 token

k2s config test [-c config]         测试配置
k2s config show                     打印当前配置

k2s service install                 安装系统服务
k2s service uninstall               卸载系统服务

k2s upgrade [--check] [--channel stable|beta]
k2s version
k2s help [command]
```

去掉的东西：
- `k2s`（裸调用）→ 必须用 `k2s run`
- `k2s -s stop|reload` → kill 或 `k2s service stop`
- `k2s -t` → 用 `k2s config test`
- `k2s -c` → 用 `k2s run --foreground -c`
- `k2s -v` → 用 `k2s version`
- `k2s demo-config` → 用 `k2s config show`

## 全局 Flag

所有命令都支持：

```
--json      结构化 JSON 输出
--quiet     只输出关键结果
--help      帮助
```

## 输出协议

### 正常输出（人类）

```
$ k2 status
State:     connected
Server:    1.2.3.4:443
Uptime:    2h 15m
TX/RX:     1.2 GB / 3.4 GB
```

### --json 输出（AI）

```json
{"ok":true,"data":{"state":"connected","server":"1.2.3.4:443","uptime_sec":8100,"tx_bytes":1288490188,"rx_bytes":3650722201}}
```

### --json 错误

```json
{"ok":false,"error":"daemon not running","code":"DAEMON_NOT_RUNNING","hint":"run: k2 setup <URL>"}
```

### k2 connect --json（事件流）

每行一个 JSON 事件：

```
{"event":"connecting","server":"1.2.3.4:443","transport":"quic"}
{"event":"connected","listen":"127.0.0.1:1080","mode":"proxy"}
{"event":"disconnected","reason":"signal","duration_sec":120}
```

## 退出码

| 码 | 含义 |
|----|------|
| 0 | 成功 |
| 1 | 用法错误（参数不对） |
| 2 | 运行时错误（连接失败、超时） |
| 3 | 权限不足（需要 root） |

## `k2 connect` 设计

AI 用的核心命令。直接启动 engine，不经过 daemon。

```go
func cmdConnect(args []string) int {
    fs := flag.NewFlagSet("k2 connect", flag.ExitOnError)
    mode := fs.String("mode", "proxy", "proxy or tun")
    listen := fs.String("listen", "127.0.0.1:1080", "proxy listen address")
    fs.Parse(args)

    url := fs.Arg(0)
    cfg := config.ClientFromURL(url)
    cfg.Mode = *mode
    cfg.Proxy.Listen = *listen

    // 直接用 engine，不用 daemon
    eng := engine.New(engineConfig(cfg))
    ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
    defer cancel()

    eng.Start(ctx)
    <-ctx.Done()
    eng.Stop()
}
```

默认 proxy 模式 → 不需要 root，不需要 TUN，不需要系统服务。
AI 只要有 URL，一条命令就能连上。

TUN 模式需要 root 和 DirectDialer（防止路由环路）。
proxy 模式不需要 DirectDialer 也不需要 NetworkMonitor。

## `k2s setup` 非交互模式

| 向导步骤 | Flag | 默认值 |
|---------|------|--------|
| 端口 | `--port N` | 443 |
| 端口跳跃 | `--hop START-END` | 不启用 |
| 用户名 | `--username NAME` | prince |
| 防火墙 | `--no-firewall` | 自动检测 |

规则：`--port` + `--username` + `--no-firewall` 三个都给了 → 完全自动，不弹任何提示。
缺任何一个 → 只提示缺的那个。
什么都不给 → 完整向导。

```bash
# AI 用法：一条命令，零交互
k2s setup --port 443 --username alice --no-firewall --json
# → {"ok":true,"data":{"url":"k2v5://alice:token@host:443?ech=...","config":"/etc/k2s/config.yml"}}
```

## 双语输出

去掉。CLI 工具统一英文。中文界面属于桌面/手机 App。

## 系统服务入口

service 定义全部改为用 `run` 子命令：

### Linux systemd

```ini
[Service]
ExecStart=/usr/local/bin/k2 run
```

```ini
[Service]
ExecStart=/usr/local/bin/k2s run --foreground
```

带配置文件时：

```ini
ExecStart=/usr/local/bin/k2s run --foreground -c /etc/k2s/config.yml
```

### macOS launchd

```xml
<key>ProgramArguments</key>
<array>
    <string>/usr/local/bin/k2</string>
    <string>run</string>
</array>
```

### Windows SCM

```
binPath= "C:\Program Files\k2\k2.exe" run
```

Windows `k2 run` 内部仍然用 `svc.IsWindowsService()` 判断 SCM vs 前台。

### OpenWRT procd

```sh
procd_set_param command /usr/local/bin/k2 run
```

## 帮助文本

```
$ k2 --help
k2 — Kaitu network tunnel

Quick start:
  k2 connect <URL>          Connect (foreground, no setup needed)
  k2 setup <URL>            Install as system service + connect

Commands:
  connect   Connect to server (foreground)
  up        Connect via background daemon
  down      Disconnect
  status    Connection status

  setup     First-time setup                          [root]
  run       Start daemon (service entry point)        [root for TUN]

  config    Configuration (test, show)
  service   System service (install, uninstall)       [root]
  upgrade   Self-update
  version   Version info

Flags:
  --json    Machine-readable JSON output
  --quiet   Minimal output
  --help    Help for any command
```

```
$ k2s --help
k2s — Kaitu network tunnel server

Quick start:
  k2s run                   Deploy server (prints connect URL)

Commands:
  run       Start/deploy server
  setup     Setup wizard (or --port/--username for automation)
  user      User management (add, list, del, reset)

  config    Configuration (test, show)
  service   System service (install, uninstall)       [root]
  upgrade   Self-update
  version   Version info

Flags:
  --json    Machine-readable JSON output
  --quiet   Minimal output
  --help    Help for any command
```

## main() 结构

```go
func main() {
    if len(os.Args) < 2 {
        printUsage()
        os.Exit(1)
    }

    // 解析全局 flag（--json, --quiet）
    cmd, args := parseGlobal(os.Args[1:])

    switch cmd {
    case "connect":  os.Exit(cmdConnect(args))
    case "up":       os.Exit(cmdUp(args))
    case "down":     os.Exit(cmdDown(args))
    case "status":   os.Exit(cmdStatus(args))
    case "setup":    os.Exit(cmdSetup(args))
    case "run":      os.Exit(cmdRun(args))
    case "config":   os.Exit(cmdConfig(args))
    case "service":  os.Exit(cmdService(args))
    case "upgrade":  os.Exit(cmdUpgrade(args))
    case "version":  os.Exit(cmdVersion(args))
    case "help", "--help", "-h":
        printUsage()
    default:
        fmt.Fprintf(os.Stderr, "Unknown command: %s\nRun 'k2 help' for usage.\n", cmd)
        os.Exit(1)
    }
}
```

每个子命令内部用 `flag.NewFlagSet` → `--help` 自动生效。

## 实现步骤

### Step 1: 骨架重构

- 新建 `cmd/k2/commands.go`：全局 flag 解析 + 子命令分发
- 改 `main.go`：去掉 top-level flags，走纯子命令
- 每个子命令迁移到独立 `flag.NewFlagSet`
- `--help` 全部自动生效

### Step 2: `k2 connect` + `k2 up/down/status`

- 新建 `cmd/k2/connect.go`：直连模式（engine 直启，proxy 默认）
- `k2 up/down/status` 内部委托到现有 ctl 逻辑
- 删除 `ctl.go` 中的 `ctl` 前缀处理

### Step 3: `--json` 输出层

- 新建 `cmd/k2/output.go`：统一输出函数（human vs JSON）
- 所有命令的输出走这个层
- 错误输出也走这个层（带 error code）

### Step 4: k2s 非交互 setup

- `k2s setup` 增加 `--port/--username/--no-firewall` flag
- 三个都给了 → 跳过所有 prompt
- 去掉 `biPrint`，改英文输出

### Step 5: 系统服务模板更新

- `daemon/service_linux.go`：`ExecStart` 改为 `k2 run`
- `daemon/service_darwin.go`：`ProgramArguments` 加 `run`
- `daemon/service_windows.go`：`binPath` 加 `run`
- `cmd/k2s/service_*.go`：同上
- 同步更新 OpenWRT procd 模板

### Step 6: 清理

- 删除 `svc_other.go`（合并到 `cmdRun`）
- 删除 `ctl_hint_*.go`（错误信息统一到 output 层）
- 删除 `biPrint`/`biPrintf` 等双语函数
- 更新帮助文本、CLAUDE.md

## AI 完整使用流程

```bash
# 1. 部署服务器（一条命令，零交互）
k2s setup --port 443 --username agent --no-firewall --json
# → {"ok":true,"data":{"url":"k2v5://agent:token@1.2.3.4:443?ech=..."}}

# 2. 连接（一条命令，前台，可解析）
k2 connect 'k2v5://agent:token@1.2.3.4:443?ech=...' --json
# → {"event":"connected","listen":"127.0.0.1:1080","mode":"proxy"}

# 3. 检查状态
k2 status --json
# → {"ok":true,"data":{"state":"connected"}}

# 4. 添加用户
k2s user add bob --json
# → {"ok":true,"data":{"username":"bob","url":"k2v5://bob:token@..."}}

# 5. 停止
# Ctrl+C (connect 模式) 或 k2 down (daemon 模式)
```

零交互。零双语。零 daemon 前置。每个输出都是 JSON。每个错误都有码。
