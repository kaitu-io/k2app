# Feature: macOS PKG Service Lifecycle

## Meta

| Field     | Value                                    |
|-----------|------------------------------------------|
| Feature   | macos-pkg-service-lifecycle              |
| Version   | v1                                       |
| Status    | implemented                              |
| Created   | 2026-02-22                               |
| Updated   | 2026-02-22                               |
| Depends   | k2-cli-redesign                          |

## Version History

| Version | Date       | Summary                                              |
|---------|------------|------------------------------------------------------|
| v1      | 2026-02-22 | Initial: PKG preinstall/postinstall + service label unification |

## Overview

macOS PKG 安装器的服务生命周期管理。确保升级安装时正确执行：停止旧服务 → 卸载旧服务 → 覆盖文件 → 安装新服务 → 启动。

此前 macOS PKG 没有 install scripts，服务安装依赖 Tauri 启动时的 `ensure_service_running()`。但这意味着：
- 安装时旧服务可能持有文件句柄，导致覆盖失败
- 旧版 k2 进程可能继续运行
- 首次安装没有服务，需等用户打开 app 才触发安装

## Product Requirements

- PR1: PKG 升级安装时零手动干预，服务自动迁移
- PR2: 首次安装后服务立即可用，不需打开 app
- PR3: 兼容旧版 kaitu-service 格式的 plist

## Technical Decisions

### TD1: Service Label 统一为 `kaitu`

k2 替代 kaitu-service，launchd label 必须是 `kaitu`（与现有生产服务一致），而非 `io.kaitu.k2`。

理由：
- 现有生产环境 plist 路径 `/Library/LaunchDaemons/kaitu.plist`，label `kaitu`
- 直接覆盖 = 零迁移成本，无需检测+清理旧 label
- `k2 service install` 先 `launchctl unload` 再写 plist 再 `launchctl load`，确保覆盖干净

### TD2: preinstall + postinstall 分离

PKG install scripts 分为两阶段：

- **preinstall**（旧文件还在）：杀 app → `k2 service uninstall` → fallback 清理 → sleep 2s
- **postinstall**（新文件已到位）：`k2 service install`

不能合并为单个 postinstall，因为：
- 旧 k2 binary 需要在被覆盖前执行 uninstall（否则旧 binary 已被替换，可能不兼容）
- app 文件句柄需在覆盖前释放

### TD3: Fallback 清理

preinstall 中 `k2 service uninstall` 之后，仍检查 plist 文件是否存在。处理以下场景：
- 旧版 kaitu-service 没有 `service uninstall` 命令
- k2 binary 损坏无法执行
- 手动安装的 plist 格式不同

### TD4: 生产 plist 配置保留

`k2 service install` 生成的 plist 包含生产必需配置：
- `HardResourceLimits` / `SoftResourceLimits`：4000 进程、10240 文件描述符
- `ThrottleInterval: 2`：崩溃后 2 秒重启（默认 10 秒太慢）
- `KeepAlive: true`：进程退出自动重启

## Design

### 安装流程

```
PKG Installer
  │
  ├─ preinstall (root, 旧文件)
  │   1. pkill -f "Kaitu.app/Contents/MacOS/Kaitu"
  │   2. /Applications/Kaitu.app/Contents/MacOS/k2 service uninstall
  │   3. fallback: launchctl unload + rm kaitu.plist
  │   4. sleep 2
  │
  ├─ 文件复制: Kaitu.app → /Applications/
  │
  └─ postinstall (root, 新文件)
      1. /Applications/Kaitu.app/Contents/MacOS/k2 service install
```

### 文件

```
scripts/pkg-scripts/preinstall    — PKG preinstall hook
scripts/pkg-scripts/postinstall   — PKG postinstall hook
scripts/build-macos.sh            — pkgbuild --scripts 参数
k2/daemon/service_darwin.go       — label="kaitu", 含生产资源限制
```

### 对比 Windows NSIS

| 阶段 | macOS PKG | Windows NSIS |
|------|-----------|-------------|
| 停止 app | `pkill` (preinstall) | `taskkill /F /IM` (PREINSTALL) |
| 卸载服务 | `k2 service uninstall` (preinstall) | `k2.exe service uninstall` (PREINSTALL) |
| 等待清理 | `sleep 2` | `Sleep 10000` |
| 安装服务 | `k2 service install` (postinstall) | `k2.exe service install` (POSTINSTALL) |
| 服务恢复 | launchd KeepAlive + ThrottleInterval | `sc failure` restart/5000 |

## Acceptance Criteria

### AC1: preinstall 卸载旧服务
- PKG 安装时 preinstall 脚本以 root 执行
- 杀掉运行中的 Kaitu.app
- 调用 `k2 service uninstall` 停止并卸载现有服务
- 旧格式 plist 通过 fallback 清理

### AC2: postinstall 安装新服务
- 文件覆盖完成后 postinstall 以 root 执行
- 调用 `k2 service install` 写 plist + launchctl load
- 服务立即启动

### AC3: Service Label 统一
- `k2 service install` 使用 label `kaitu`，plist 路径 `/Library/LaunchDaemons/kaitu.plist`
- 直接覆盖现有 kaitu-service 的 plist
- install 前先 unload 现有服务

### AC4: 生产配置
- plist 包含 HardResourceLimits (4000 proc, 10240 files)
- plist 包含 SoftResourceLimits (同上)
- ThrottleInterval = 2

### AC5: build-macos.sh 集成
- `pkgbuild` 命令包含 `--scripts scripts/pkg-scripts`
- preinstall 和 postinstall 均为可执行文件
