# Feature: OpenWrt Support

## Meta

| Field     | Value                                    |
|-----------|------------------------------------------|
| Feature   | openwrt-support                          |
| Version   | v1                                       |
| Status    | implemented                              |
| Created   | 2026-02-16                               |
| Updated   | 2026-02-16                               |
| Tests     | TBD                                      |

## Version History

| Version | Date       | Summary                                                    |
|---------|------------|------------------------------------------------------------|
| v1      | 2026-02-16 | Initial spec: cross-compile, webapp embed, CI/CD, LuCI     |

## Overview

为 k2 添加完整的 OpenWrt 路由器支持。复用现有 daemon 架构（HTTP :1777），通过 Go `embed.FS` 将 webapp 内嵌到 daemon 二进制中，使路由器用户通过浏览器访问 `http://路由器IP:1777` 管理 VPN。GitHub Actions 自动交叉编译 4 个架构，打包 tar.gz 发布。

**当前状态**：k2 daemon 仅支持 linux/amd64、macOS、Windows。webapp 与 daemon 是分离的（Tauri/Capacitor 各自加载）。无 OpenWrt 构建、打包或分发流程。

**目标状态**：单一 k2 二进制包含 daemon + 内嵌 webapp，支持 arm64/amd64/armv7/mipsle 四架构，tar.gz + install.sh 分发，LuCI 菜单集成，init.d 服务管理，推送 v* tag 自动发布到 S3。

## Architecture

```
OpenWrt 路由器
┌──────────────────────────────────┐
│  /usr/bin/k2                     │
│  ┌────────────────────────────┐  │
│  │ daemon (HTTP :1777)        │  │
│  │ ├─ /api/core  (VPN API)   │  │
│  │ ├─ /ping      (健康检查)  │  │
│  │ ├─ /metrics   (监控)      │  │
│  │ └─ /*         (webapp UI) │  │  ← 新增：embed.FS serve 静态文件
│  ├────────────────────────────┤  │
│  │ engine (wire → router →   │  │
│  │         dns → provider)   │  │
│  ├────────────────────────────┤  │
│  │ TUN provider (sing-tun)   │  │  ← sing-tun 自动检测 OpenWrt，用 nftables
│  └────────────────────────────┘  │
│                                  │
│  /etc/init.d/k2    (服务脚本)    │
│  /etc/k2/config.yaml (配置)      │
│  /etc/k2/rules/    (路由规则缓存)│
└──────────────────────────────────┘

LAN 设备（手机/电脑）
  浏览器 → http://192.168.1.1:1777 → webapp UI
```

## Product Requirements

### PR1: Webapp 内嵌到 Daemon

将 webapp `yarn build` 产出的 dist/ 静态文件通过 Go `embed.FS` 打包进 k2 二进制。daemon 启动时在 :1777 同时 serve API 和 UI。

**修改点：**

1. **Webapp 内嵌已实现** — 无需新增代码
   - `k2/cloud/embed.go`: `//go:embed dist/*` 已将 `cloud/dist/` 打包进二进制
   - `k2/cloud/embed_nowebapp.go`: `-tags nowebapp` 禁用（桌面版使用此 tag）
   - `k2/daemon/api.go`: `webappHandler()` 已实现 SPA fallback + 静态文件 serving
   - `k2/daemon/daemon.go`: 已注册 `mux.Handle("/", webappHandler())`
   - **OpenWrt 构建方式**：将 webapp dist/ 复制到 `k2/cloud/dist/`，编译时**不带** `-tags nowebapp`
   - 现有 `cloud/dist/` 仅含占位 `index.html`，需替换为真实 webapp 产物

2. **k2/daemon/ — 监听地址可配置**
   - 现状：硬编码 `DefaultAddr = "127.0.0.1:1777"`
   - 修改：config.yaml 支持 `listen: "0.0.0.0:1777"`（OpenWrt 场景需要 LAN 访问）
   - 默认值保持 `127.0.0.1:1777`（桌面安全性不变）

3. **webapp HttpVpnClient — 支持同源模式**
   - 现状：`this.baseUrl = import.meta.env.DEV ? '' : 'http://127.0.0.1:1777'`
   - 修改：当 webapp 从 daemon 自身 serve 时（非 Tauri、非 Capacitor、非 localhost 开发），baseUrl 为 `''`（相对路径，同源请求）
   - 判断逻辑：如果 `window.location.port === '1777'` 或 `window.location.pathname` 命中 daemon，使用同源模式
   - 更简洁的方案：生产模式改为 `baseUrl = window.location.origin.includes(':1777') ? '' : 'http://127.0.0.1:1777'`

4. **webapp — 隐藏桌面专属功能**
   - 在 WebPlatform（非 Tauri、非 Capacitor）下隐藏：
     - ServiceReadiness（daemon 就是自身，无需等待）
     - UpdatePrompt（路由器不需要 OTA）
     - 托盘相关功能
   - 现有 PlatformApi 已有平台检测能力，复用即可

### PR2: 交叉编译与打包

4 架构交叉编译 + tar.gz 打包。

**目标架构：**

| GOOS  | GOARCH | GOARM | 产物名                    | 目标设备                        |
|-------|--------|-------|---------------------------|---------------------------------|
| linux | arm64  | —     | k2-openwrt-aarch64        | MT7981/7986, IPQ5018/6018       |
| linux | amd64  | —     | k2-openwrt-x86_64         | 软路由 J4125/N100, ESXi         |
| linux | arm    | 7     | k2-openwrt-armv7          | IPQ4019, 部分 MT7621            |
| linux | mipsle | —     | k2-openwrt-mipsle         | MT7621 (小米/红米/新路由)       |

**构建命令（每架构）：**
```bash
# 1. 构建 webapp
cd webapp && yarn build

# 2. 将 dist/ 复制到 Go embed 路径
cp -r webapp/dist k2/daemon/webapp_dist/

# 3. 交叉编译（以 arm64 为例）
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
  go build -tags embed_webapp \
  -ldflags '-s -w -X main.version=${VERSION}' \
  -o build/k2-openwrt-aarch64 ./cmd/k2
```

**打包结构（每架构一个 tar.gz）：**
```
k2-openwrt-aarch64-v0.4.0.tar.gz
├── k2                    # 二进制（含内嵌 webapp）
├── install.sh            # 安装脚本
├── k2.init               # init.d 模板
└── luci-app-k2/          # LuCI 集成文件
    ├── controller/k2.lua
    └── view/k2.htm
```

### PR3: 安装脚本 (install.sh)

一键安装脚本，处理：

```bash
#!/bin/sh
# k2 OpenWrt Installer

# 1. 停止旧服务（如存在）
/etc/init.d/k2 stop 2>/dev/null

# 2. 安装二进制
cp k2 /usr/bin/k2
chmod +x /usr/bin/k2

# 3. 创建配置目录
mkdir -p /etc/k2

# 4. 生成默认配置（如不存在）
if [ ! -f /etc/k2/config.yaml ]; then
  cat > /etc/k2/config.yaml << 'CONF'
listen: "0.0.0.0:1777"
mode: tun
log:
  level: info
CONF
fi

# 5. 安装 init.d 脚本
cp k2.init /etc/init.d/k2
chmod +x /etc/init.d/k2

# 6. 安装 LuCI 集成（如 LuCI 存在）
if [ -d /usr/lib/lua/luci ]; then
  mkdir -p /usr/lib/lua/luci/controller
  mkdir -p /usr/lib/lua/luci/view
  cp luci-app-k2/controller/k2.lua /usr/lib/lua/luci/controller/k2.lua
  cp luci-app-k2/view/k2.htm /usr/lib/lua/luci/view/k2.htm
  # 清除 LuCI 缓存
  rm -rf /tmp/luci-*
fi

# 7. 启用开机自启
/etc/init.d/k2 enable

# 8. 启动服务
/etc/init.d/k2 start

echo "k2 installed. Web UI: http://$(uci get network.lan.ipaddr 2>/dev/null || echo '路由器IP'):1777"
```

### PR4: init.d 服务脚本

```bash
#!/bin/sh /etc/rc.common
# k2 VPN service for OpenWrt

START=95
STOP=15

USE_PROCD=1

start_service() {
    procd_open_instance
    procd_set_param command /usr/bin/k2 run -c /etc/k2/config.yaml
    procd_set_param respawn 3600 5 5    # 崩溃后自动重启
    procd_set_param stdout 1
    procd_set_param stderr 1
    procd_close_instance
}

stop_service() {
    /usr/bin/k2 down 2>/dev/null
}
```

**选择 procd 而非裸 start/stop 的原因**：
- procd 是 OpenWrt 14.07+ 的标准服务管理器
- 提供进程监控、崩溃自动重启、日志收集
- 所有现代 OpenWrt（19.07+）都支持

### PR5: LuCI 菜单集成

轻量集成：在 LuCI 侧边栏添加 K2 VPN 入口，iframe 嵌入 webapp。

**controller/k2.lua:**
```lua
module("luci.controller.k2", package.seeall)

function index()
    entry({"admin", "services", "k2"}, template("k2"), _("K2 VPN"), 90)
end
```

**view/k2.htm:**
```html
<%+header%>
<div style="width:100%;height:calc(100vh - 120px);overflow:hidden;">
  <iframe src="http://127.0.0.1:1777"
          style="width:100%;height:100%;border:none;"
          allowfullscreen></iframe>
</div>
<%+footer%>
```

**为什么用 iframe 而不是原生 LuCI 页面**：
- 我们的 webapp 已经是完整的管理界面（React + i18n + 主题）
- 用 iframe 零维护成本，webapp 更新对 LuCI 透明
- 不依赖 LuCI 版本（LuCI1 Lua 和 LuCI2 JS 都兼容）
- 不需要学习 UCI 配置系统

### PR6: CI/CD 自动发布

新建 `.github/workflows/release-openwrt.yml`，与 `release-desktop.yml` 并行触发。

**触发条件**：`push tags: v*`（与桌面发布相同）

**构建矩阵**：
```yaml
strategy:
  fail-fast: false
  matrix:
    include:
      - goos: linux
        goarch: arm64
        name: aarch64
      - goos: linux
        goarch: amd64
        name: x86_64
      - goos: linux
        goarch: arm
        goarm: '7'
        name: armv7
      - goos: linux
        goarch: mipsle
        name: mipsle
```

**构建步骤**：
1. Checkout + k2 submodule（复用 deploy key 方式）
2. Setup Node.js + Go
3. `yarn install && yarn build`（webapp dist/）
4. 复制 dist/ 到 Go embed 路径
5. 4 架构并行交叉编译（`-tags embed_webapp -ldflags '-s -w'`）
6. qemu-user-static 冒烟测试（每架构运行 `k2 version`）
7. 打包 tar.gz（二进制 + install.sh + init.d + luci）
8. 上传到 S3（`s3://d0.all7.cc/kaitu/openwrt/${VERSION}/`）
9. Slack 通知

**测试策略（三层）**：

| 层级 | 位置 | 验证内容 | 自动化 |
|------|------|---------|--------|
| L1: 架构验证 | CI | `file` 检查 ELF 架构正确 | 是 |
| L2: 冒烟测试 | CI | qemu-user-static 执行 `k2 version` | 是 |
| L3: 真机验证 | 手动 | 真实路由器连接 VPN、访问 webapp | 否 |

**L2 qemu 冒烟测试的价值**：
- 成本：3 行 CI 配置（安装 qemu-user-static + binfmt-support）
- 收益：捕获静态链接缺失、Go runtime MIPS softfloat bug、符号错误
- 不测试网络/TUN（那是 L3 的事）

## Implementation Notes

### Go 代码修改范围（k2 子模块）

| 文件 | 修改 |
|------|------|
| `daemon/daemon.go` | listen addr 从 config 读取（替代硬编码 DefaultAddr） |
| `daemon/api.go` | CORS 允许 LAN origin（当 listen 0.0.0.0 时） |
| `config/config.go` | ClientConfig 增加 `Listen string` 字段 |
| `Makefile` | 增加 `build-openwrt` target |
| `cloud/embed.go` | **已存在，无需修改** — `//go:embed dist/*` |
| `cloud/embed_nowebapp.go` | **已存在，无需修改** — 桌面版用 `-tags nowebapp` |
| `daemon/api.go:webappHandler()` | **已存在，无需修改** — SPA serving 已实现 |

### Webapp 代码修改范围

| 文件 | 修改 |
|------|------|
| `webapp/src/vpn-client/http-client.ts` | baseUrl 逻辑：检测同源模式 |
| `webapp/src/components/ServiceReadiness.tsx` | WebPlatform 下跳过 daemon 等待 |
| `webapp/src/components/UpdatePrompt.tsx` | WebPlatform 下隐藏 |

### 新增文件

| 文件 | 说明 |
|------|------|
| `.github/workflows/release-openwrt.yml` | OpenWrt CI/CD |
| `scripts/build-openwrt.sh` | 构建脚本（CI 调用） |
| `scripts/openwrt/install.sh` | 安装脚本模板 |
| `scripts/openwrt/k2.init` | init.d 服务脚本 |
| `scripts/openwrt/luci-app-k2/` | LuCI 集成文件 |

## Acceptance Criteria

### AC1: Webapp 内嵌 Serving
- [ ] `go build -tags embed_webapp` 产出的二进制内含 webapp 静态文件
- [ ] 启动 daemon 后，浏览器访问 `http://IP:1777/` 显示 webapp UI
- [ ] API 路由（`/api/core`, `/ping`, `/metrics`）正常工作，优先级高于静态文件
- [ ] SPA history 模式：任意路径 fallback 到 index.html
- [ ] 不带 `embed_webapp` tag 编译时，行为与现在一致（桌面版不受影响）

### AC2: 同源 HttpVpnClient
- [ ] 从 daemon 自身 serve 的 webapp 中，HttpVpnClient 使用相对路径请求 API
- [ ] 从 Tauri 窗口中，HttpVpnClient 行为不变（绝对路径 `http://127.0.0.1:1777`）
- [ ] 连接、断开、状态查询、配置获取在同源模式下全部正常

### AC3: 监听地址可配置
- [ ] config.yaml 支持 `listen: "0.0.0.0:1777"`
- [ ] 默认值 `127.0.0.1:1777` 不变
- [ ] OpenWrt 安装脚本生成的默认配置使用 `0.0.0.0:1777`

### AC4: 交叉编译
- [ ] 4 架构（arm64, amd64, armv7, mipsle）均编译成功
- [ ] `file` 检查确认 ELF 架构正确
- [ ] qemu-user-static 运行 `k2 version` 输出正确版本号
- [ ] 二进制大小 < 25MB（strip 后，不含 UPX）

### AC5: 打包与安装
- [ ] 每架构产出 `k2-openwrt-{arch}-v{version}.tar.gz`
- [ ] tar.gz 包含：k2 二进制, install.sh, k2.init, luci-app-k2/
- [ ] install.sh 在 OpenWrt 上执行后：二进制到 /usr/bin/k2，配置到 /etc/k2/，init.d 注册完成
- [ ] `/etc/init.d/k2 start` 启动成功，`/etc/init.d/k2 stop` 停止成功
- [ ] `/etc/init.d/k2 enable` 设置开机自启

### AC6: LuCI 集成
- [ ] install.sh 检测 LuCI 存在时自动安装菜单
- [ ] LuCI 侧边栏「服务」下出现「K2 VPN」菜单项
- [ ] 点击后 iframe 加载 webapp，功能完整可用
- [ ] LuCI 不存在时安装脚本跳过，不报错

### AC7: CI/CD 自动发布
- [ ] push `v*` tag 触发 `release-openwrt.yml`
- [ ] 4 架构并行构建 + qemu 冒烟测试
- [ ] 产物上传到 S3 `kaitu/openwrt/${VERSION}/`
- [ ] 构建成功/失败发 Slack 通知
- [ ] 与 `release-desktop.yml` 互不干扰，并行运行

### AC8: 桌面版无回归
- [ ] 不带 `embed_webapp` tag 的普通编译（桌面版）功能不变
- [ ] Tauri webapp 的 HttpVpnClient 行为不变
- [ ] daemon 默认监听 127.0.0.1:1777 不变
- [ ] `cd webapp && yarn test` 全部通过
- [ ] `cd desktop/src-tauri && cargo test` 全部通过
