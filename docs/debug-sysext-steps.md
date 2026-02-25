# macOS System Extension — 开发调试完整手册

可复用的 sysext 开发/调试流程。涵盖构建、签名、部署、测试、诊断全链路。

## 目录
1. [环境要求](#环境要求)
2. [架构概览](#架构概览)
3. [构建链路](#构建链路)
4. [快速迭代部署（Hot-swap）](#快速迭代部署hot-swap)
5. [VPN 连接测试](#vpn-连接测试)
6. [诊断与排错](#诊断与排错)
7. [已知坑与修复记录](#已知坑与修复记录)
8. [签名规则速查](#签名规则速查)

---

## 环境要求

| 项目 | 要求 |
|------|------|
| macOS | 14.x+, arm64 |
| SIP | **必须关闭** (`csrutil disable` in Recovery Mode) — 否则无法加载未公证的 sysext |
| 签名证书 | Developer ID Application (login keychain) |
| Xcode | 已安装 Command Line Tools (`xcode-select -p`) |
| gomobile | `go install golang.org/x/mobile/cmd/gomobile@latest` |

```bash
# 验证环境
csrutil status                    # 应显示 disabled
security find-identity -v | grep "Developer ID"  # 应显示签名证书
which gomobile                    # 应有 gomobile
```

---

## 架构概览

```
┌─────────────────────────────────────────────────┐
│  Kaitu.app (Tauri)                              │
│  ├── Contents/MacOS/k2app        (Rust主进程)    │
│  ├── Contents/MacOS/k2           (Go sidecar)   │
│  └── Contents/Library/SystemExtensions/          │
│      └── io.kaitu.desktop.tunnel.systemextension │
│          └── Contents/MacOS/KaituTunnel          │
│              = PacketTunnelProvider.swift         │
│              + K2MobileMacOS.xcframework (gomobile)│
└─────────────────────────────────────────────────┘

IPC 路径:
  WebView JS → invoke('daemon_exec') → Rust ne.rs (C FFI)
  → libk2_ne_helper.a (Swift) → NEVPNManager
  → nesessionmanager → KaituTunnel sysext 进程

数据路径:
  App → configJSON via NEVPNManager startOptions
  → PacketTunnelProvider.startTunnel()
  → MobileNewEngine() + engine.start(configJSON, fd, cfg)
  → sing-tun (utun fd) → QUIC/TCP-WS → k2v5 server
```

### 关键源文件

| 文件 | 作用 |
|------|------|
| `desktop/src-tauri/KaituTunnel/PacketTunnelProvider.swift` | NE sysext 主逻辑 |
| `desktop/src-tauri/KaituTunnel/Info.plist` | sysext bundle 元数据 (SYSX) |
| `desktop/src-tauri/KaituTunnel/KaituTunnel.entitlements` | sysext 权限 |
| `desktop/src-tauri/entitlements.plist` | 主 app 权限 |
| `desktop/src-tauri/ne_helper/K2NEHelper.swift` | Swift NE 控制库 (C FFI) |
| `desktop/src-tauri/src/ne.rs` | Rust NE 桥接 (macOS only) |
| `k2/engine/engine.go` | Go 隧道引擎 |
| `k2/provider/tun_desktop.go` | sing-tun TUN provider (desktop) |
| `k2/mobile/mobile.go` | gomobile 入口 |
| `scripts/build-macos.sh` | 完整构建脚本 |

---

## 构建链路

### 完整构建（CI/Release）

```bash
make build-macos          # 签名 + 公证
make build-macos-fast     # 签名，跳过公证（本地开发）
```

内部步骤:
```
1. make build-webapp                           → webapp/dist
2. make build-k2 (arm64 + x86_64 + lipo)      → k2 universal binary
3. make mobile-macos                           → K2MobileMacOS.xcframework
4. ne_helper/build.sh --arch universal         → libk2_ne_helper.a
5. yarn tauri build --target universal         → Kaitu.app (未签名)
6. swiftc PacketTunnelProvider.swift           → KaituTunnel executable
7. 组装 sysext bundle → 注入 Kaitu.app
8. 嵌入 provisioning profiles
9. 逐组件签名（k2 → sysext → main app）
10. pkgbuild + productsign → PKG
11. notarytool submit + staple
```

### 单独重编 sysext 二进制（开发迭代最常用）

#### 场景 A: 只改了 Swift 代码

```bash
ROOT_DIR="/Users/david/projects/kaitu-io/k2app"
XCFW_PATH="$ROOT_DIR/k2/build/K2MobileMacOS.xcframework"

# 找到 xcframework 的平台 slice 父目录
XCFW_SLICE_PARENT=$(dirname "$(find "$XCFW_PATH" -name "K2MobileMacOS.framework" -maxdepth 2 -type d | head -1)")

# 编译
swiftc \
  -emit-executable \
  -module-name KaituTunnel \
  -sdk "$(xcrun --sdk macosx --show-sdk-path)" \
  -target arm64-apple-macos12 \
  -F "$XCFW_SLICE_PARENT" \
  -framework K2MobileMacOS \
  "$ROOT_DIR/desktop/src-tauri/KaituTunnel/PacketTunnelProvider.swift" \
  -o /tmp/KaituTunnel
```

#### 场景 B: 改了 Go 代码（k2/ 子模块）

```bash
cd $ROOT_DIR && make mobile-macos
# 内部执行: gomobile bind -tags with_gvisor -target=macos ...
# 产出: k2/build/K2MobileMacOS.xcframework
# 然后执行场景 A 的 swiftc 编译
```

> **重要**: `-tags with_gvisor` 是必需的。gVisor stack 用于 NE 的 TCP/IP 处理（System stack 在 NE 沙箱中不工作）。

#### 场景 C: 改了 NE Helper (K2NEHelper.swift)

```bash
cd $ROOT_DIR/desktop/src-tauri/ne_helper
bash build.sh --arch arm64   # 或 --arch universal
# 产出: ne_helper/libk2_ne_helper.a
# 需要重新 cargo build (Tauri)
```

---

## 快速迭代部署（Hot-swap）

**核心循环**: 修改代码 → 编译 sysext → 替换 → 签名 → 部署 → 测试

### 环境变量（每个 shell 会话设一次）

```bash
export SIGN_IDENTITY="Developer ID Application: ALL NATION CONNECT TECHNOLOGY PTE. LTD. (NJT954Q3RH)"
export ROOT_DIR="/Users/david/projects/kaitu-io/k2app"
export SYSEXT_REL="Contents/Library/SystemExtensions/io.kaitu.desktop.tunnel.systemextension"
```

### 一键部署脚本（复制粘贴即用）

```bash
# === 参数 ===
BUNDLE_VER=10   # 每次递增！

# === 1. 杀进程 ===
pkill -f "k2app" 2>/dev/null; sleep 2

# === 2. 复制到 /tmp（用户上下文签名）===
rm -rf /tmp/Kaitu.app
cp -R /Applications/Kaitu.app /tmp/Kaitu.app

# === 3. 替换二进制 ===
cp /tmp/KaituTunnel "/tmp/Kaitu.app/$SYSEXT_REL/Contents/MacOS/KaituTunnel"

# === 4. 更新版本号 ===
/usr/libexec/PlistBuddy -c "Set CFBundleVersion $BUNDLE_VER" \
  "/tmp/Kaitu.app/$SYSEXT_REL/Contents/Info.plist"

# === 5. 清理 + 签名（顺序: k2 → sysext → app）===
find /tmp/Kaitu.app -name ".DS_Store" -delete

codesign --force --sign "$SIGN_IDENTITY" --options runtime \
  "/tmp/Kaitu.app/Contents/MacOS/k2"

codesign --force --sign "$SIGN_IDENTITY" \
  --entitlements "$ROOT_DIR/desktop/src-tauri/KaituTunnel/KaituTunnel.entitlements" \
  --options runtime \
  "/tmp/Kaitu.app/$SYSEXT_REL"

codesign --force --sign "$SIGN_IDENTITY" \
  --entitlements "$ROOT_DIR/desktop/src-tauri/entitlements.plist" \
  --options runtime \
  "/tmp/Kaitu.app"

# === 6. 验证 ===
codesign --verify --deep --strict "/tmp/Kaitu.app"

# === 7. 部署 ===
sudo rm -rf /Applications/Kaitu.app
sudo cp -R /tmp/Kaitu.app /Applications/Kaitu.app

# === 8. 启动 ===
open /Applications/Kaitu.app

# === 9. 验证 sysext 激活 ===
sleep 3
systemextensionsctl list 2>&1 | grep kaitu
# 应显示: * * NJT954Q3RH io.kaitu.desktop.tunnel (1.0.2/N) [activated enabled]
```

### 为什么需要这些步骤？

| 步骤 | 原因 |
|------|------|
| 复制到 /tmp | `sudo codesign` 无法访问 login keychain → `errSecInternalComponent` |
| 先 `rm -rf` 再 `cp -R` | 直接 cp 到已存在的目录会创建嵌套 `.app` |
| 递增 CFBundleVersion | 系统通过版本号检测 sysext 更新，不改不加载新二进制 |
| 签名顺序: 内→外 | 外层签名覆盖内层校验，必须从最内层开始 |
| k2 不带 NE entitlements | AMFI 要求 entitlements 与 provisioning profile 匹配，k2 没有 NE profile |
| 不用 `--deep` | `--deep` 会把 NE entitlements 应用到所有子二进制 (含 k2)，导致 AMFI 拒绝 |

---

## VPN 连接测试

### 方法 1: MCP Bridge（推荐，可自动化）

```bash
# 1. 连接 MCP Bridge
# driver_session start

# 2. 导航到 debug 页面
# webview_execute_js: window.location.href = '/debug.html'
```

```js
// 3. 连接 VPN
const { invoke } = window.__TAURI__.core;
await invoke('daemon_exec', {
  action: 'up',
  params: {
    server: 'k2v5://USER:PASS@HOST:443?ech=...&pin=...&insecure=1',
    dns: {
      proxy: ['198.18.0.7:53'],
      direct: ['223.5.5.5:53', '114.114.114.114:53']
    },
    rule: { global: true }
  }
});
// 期望: { code: 0, message: "ok", data: null }

// 4. 查询状态
const status = await invoke('daemon_exec', { action: 'status', params: {} });
// 期望: { code: 0, data: { state: "connected", connected_at: "...", uptime_seconds: N } }

// 5. 断开
await invoke('daemon_exec', { action: 'down', params: {} });
```

### 方法 2: 命令行

```bash
# NE 隧道状态
scutil --nc status "Kaitu VPN"
# Connected → SessionState: 4, NEStatus: 3
# Disconnected → SessionState: 10, NEStatus: 1

# TUN 接口
ifconfig utun19
# 应显示: inet 198.18.0.7, inet6 fdfe:dcba:9876::7, mtu 1400

# 外部 IP（验证流量经过隧道）
curl -s https://api.ipify.org

# DNS 验证
nslookup google.com
# Server 应为 198.18.0.7
```

### 验证清单

- [ ] `up` 返回 `{ code: 0 }`
- [ ] `status` 显示 `state: "connected"`
- [ ] `scutil --nc status` 显示 `Connected`
- [ ] `ifconfig` 显示 utun 接口，IP 正确
- [ ] `curl` 外部 IP 变化（或确认经过 VPN 出口）
- [ ] `down` 返回 `{ code: 0 }`
- [ ] `scutil --nc status` 显示 `Disconnected`
- [ ] 重连周期：up → down → up 均正常

---

## Split Routing 验证（smart 模式）

### 前置条件
- VPN 以 `rule: { global: false }` 连接（smart routing）
- k2rule 缓存存在于 App Group 容器的 `k2/` 目录
- 终端清除代理: `env -u HTTPS_PROXY env -u HTTP_PROXY`（避免 GOST 等本地代理干扰）

### DNS 验证

```bash
# Direct DNS（CN 域名走直连 DNS）
dig @198.18.0.7 myip.ipip.net
# 期望: 返回 A 记录

# Proxy DNS（海外域名走代理 DNS）
dig @198.18.0.7 api.ipify.org
# 期望: 返回 A 记录
```

### TCP 分流验证

```bash
# CN 站点 → 直连（应显示真实 IP）
env -u HTTPS_PROXY curl -s --max-time 15 https://myip.ipip.net
# 期望: 显示你的真实 IP（非 VPN 出口 IP）

# 海外站点 → 代理（应显示 VPN 出口 IP）
env -u HTTPS_PROXY curl -s --max-time 15 https://api.ipify.org
# 期望: 显示 VPN 服务器出口 IP
```

### TUN 流量检查

```bash
# 找到 utun 接口
ifconfig | grep "utun.*:" | awk '{print $1}' | tr -d ':'

# 检查双向流量（Ipkts 和 Opkts 都 > 0）
netstat -I utunN
# Ipkts=0 → 单向流量，stack 有问题
# Ipkts>0 && Opkts>0 → 双向正常
```

### 日志验证

```bash
sudo cat "/private/var/root/Library/Group Containers/group.io.kaitu.desktop/go_stderr.log" | grep -E "tunnel: route|appext: start|wire:"
# 期望:
#   "appext: start" 含 hasDirectDialer=true, hasNetworkMonitor=true
#   "tunnel: route" 行显示 proxy/direct 路由决策
#   "wire: QUIC client" 显示传输建立成功
```

### 故障排查

| 症状 | 可能原因 |
|------|----------|
| DNS 成功但 TCP 超时 | Stack 类型错误（System stack 不支持 NE），需要 gVisor |
| 所有流量走 VPN | `global: true` 或 k2rule 缓存不存在 |
| CN 站点也走 VPN | k2rule 规则不正确或 DNS 路由判断错误 |
| curl 走了本地代理 | 未清除 HTTPS_PROXY 环境变量 |
| TUN Ipkts=0 | tun.Options 中 Name 或 AutoRoute 设置错误（FD 路径应清空） |

---

## 诊断与排错

### 日志源（优先级排序）

#### 1. Go engine stderr（最有价值——捕获 panic 和 slog）
```bash
sudo cat "/private/var/root/Library/Group Containers/group.io.kaitu.desktop/go_stderr.log"
```

#### 2. Swift NE 日志（NSLog，覆盖整个 startTunnel 流程）
```bash
log show --predicate 'process == "KaituTunnel"' --last 5m --style compact
```

#### 3. configJSON 转储（确认 NE 收到的完整配置）
```bash
sudo cat "/private/var/root/Library/Group Containers/group.io.kaitu.desktop/diag_configJSON.txt"
```

#### 4. Crash Report（SIGABRT/SIGSEGV）
```bash
ls -lt /Library/Logs/DiagnosticReports/KaituTunnel* 2>/dev/null
```

#### 5. sysext 系统日志
```bash
log show --predicate 'subsystem == "com.apple.sx"' --last 5m --style compact
```

#### 6. VPN 错误（App Group UserDefaults）
```bash
sudo defaults read /private/var/root/Library/Group\ Containers/group.io.kaitu.desktop/Library/Preferences/group.io.kaitu.desktop.plist vpnError 2>/dev/null
```

### 正常启动日志序列

```
[KaituTunnel] startTunnel called, options keys: configJSON
[KaituTunnel] configJSON from options (len=413)
[KaituTunnel] Creating MobileNewEngine
[KaituTunnel] Engine created: ok
[KaituTunnel] Calling setTunnelNetworkSettings
[KaituTunnel] Network settings applied successfully
[KaituTunnel] TUN fd via utun scan: 9            ← KVC 返回 nil (sysext 正常), fd scan 成功
[KaituTunnel] cacheDir: /private/var/root/.../k2
[KaituTunnel] Redirecting stderr to: .../go_stderr.log
[KaituTunnel] freopen result: ok
[KaituTunnel] configJSON len=413, contains k2v5=YES
[KaituTunnel] Calling engine.start(fd=9)
[KaituTunnel:NE] transient state: connected       ← Engine 启动成功
[KaituTunnel] Engine started successfully
[KaituTunnel] Network path monitor started
[KaituTunnel] Network path satisfied, scheduling engine reset  ← NWPathMonitor 首次触发(正常)
[KaituTunnel] Triggering engine onNetworkChanged
[KaituTunnel:NE] transient state: reconnecting     ← 瞬态，正常
[KaituTunnel:NE] transient state: connected         ← 恢复

# go_stderr.log:
wire: QUIC client K2ARC enabled                     ← QUIC 连接成功
DNS query domain=... type=A                         ← DNS 代理工作
```

### 常见故障定位

| 症状 | 检查 | 可能原因 |
|------|------|----------|
| sysext 未激活 | `systemextensionsctl list` | CFBundleVersion 未递增 / 签名错误 |
| `up` 返回成功但立即断开 | go_stderr.log | Go engine panic (查看完整 backtrace) |
| `bind: can't assign requested address` | Swift IP 默认值 | TUN IP 与 Go 默认值不匹配 |
| `SIGABRT` in `NativeTun.Start` | InterfaceMonitor | FD 路径缺少 InterfaceMonitor |
| `wire: unsupported scheme ""` | diag_configJSON.txt | configJSON 解析失败或 server URL 为空 |
| `freopen result: FAILED` | 容器路径 | App Group 容器未创建 |
| sysext 显示 `terminated waiting to uninstall` | 旧版本 | 正常——重启后清理，不影响新版本 |
| `errSecInternalComponent` | 签名环境 | 用了 sudo codesign，改为用户上下文签名 |

### 清理诊断数据

```bash
sudo rm -f "/private/var/root/Library/Group Containers/group.io.kaitu.desktop/go_stderr.log"
sudo rm -f "/private/var/root/Library/Group Containers/group.io.kaitu.desktop/diag_configJSON.txt"
```

---

## 已知坑与修复记录

### Bug 1: InterfaceMonitor nil panic (SIGABRT)

**背景**: macOS sysext 用 desktop TUN 代码 (`darwin && !ios` build tag)，但 TUN fd 来自 NE（走 mobile/FD 路径）。

**根因**: `sing-tun NativeTun.Start()` 无条件调用 `InterfaceMonitor.RegisterMyInterface()`，FD 路径没提供 InterfaceMonitor → nil panic。

**修复**:
- `k2/provider/tun_desktop.go`: FD > 0 时创建 minimal `DefaultInterfaceMonitor`
- `k2/engine/engine.go`: FD 路径传递 `InterfaceMonitor` 到 provider config

### Bug 2: TUN IP 地址不匹配 (bind error)

**根因**: Swift NE 默认 `10.0.0.2/24`，Go 默认 `198.18.0.7/15`。NE 创建 utun 用 10.0.0.2，Go stack 绑定 198.18.0.7 → 失败。

**修复**: `PacketTunnelProvider.swift` 默认值对齐 Go `config.DefaultTunIPv4/IPv6`。

### Bug 3: KVC 获取 TUN fd 在 sysext 中失败

**现象**: `packetFlow.value(forKey: "socket")` 在 App Extension 中返回 fd，在 System Extension 中返回 nil。

**处理**: 已有 fallback — `findTunnelFileDescriptor()` 扫描 open fds 匹配 `com.apple.net.utun_control`（WireGuard 方案）。

### Bug 4: App Group 容器路径不同

**现象**: System Extension 以 root 运行，容器在 `/private/var/root/Library/Group Containers/`，非用户目录。

**影响**: `sudo` 读日志，不影响功能。

---

## 签名规则速查

```
签名顺序 (内→外):
  1. k2 sidecar         --options runtime                  (无 NE entitlements)
  2. sysext bundle       --entitlements KaituTunnel.entitlements --options runtime
  3. main app            --entitlements entitlements.plist  --options runtime

禁止:
  ✗ codesign --deep         → NE entitlements 污染 k2 sidecar
  ✗ sudo codesign           → 无法访问 login keychain
  ✗ Hardened Runtime 放松    → allow-jit / disable-library-validation 等被内核拒绝
  ✗ k2 带 NE entitlements   → AMFI 拒绝（无匹配 provisioning profile）

验证:
  codesign --verify --deep --strict /tmp/Kaitu.app
  codesign -dvvv /tmp/Kaitu.app/$SYSEXT_REL   # 检查 entitlements
```

---

## 附录: App Group 容器结构

```
/private/var/root/Library/Group Containers/group.io.kaitu.desktop/
├── k2/                          # engine cacheDir (k2rule 缓存等)
├── go_stderr.log                # Go engine stderr (freopen 重定向)
├── diag_configJSON.txt          # configJSON 转储 (诊断用)
└── Library/
    └── Preferences/
        └── group.io.kaitu.desktop.plist   # vpnError 等 UserDefaults
```
