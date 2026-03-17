# dl.kaitu.io 下载域名迁移 + 统一安装脚本

## 背景

安装包托管在 S3 bucket `d0.all7.cc`，通过 CloudFront `d13jc1jqzlg4yt.cloudfront.net` 分发。用户下载时浏览器显示第三方 CDN 域名，Windows SmartScreen 对非官网域名的下载信任度较低，容易触发拦截警告。

同时，Linux 安装脚本 `scripts/install-linux.sh` 未部署到 `web/public/`，且 macOS 缺少命令行安装方式。需要统一为 `kaitu.io/i/k2`。

## 目标

1. 将用户手动下载链接从 CDN 域名改为 `dl.kaitu.io`（与官网同根域），提升 SmartScreen 信任度，减少用户困扰。
2. 创建统一安装脚本 `web/public/i/k2`，根据 OS 自动分流安装桌面客户端。

## 覆盖范围

**变更**：所有用户手动下载的安装包链接
- Windows EXE
- macOS PKG
- Linux AppImage
- Android APK
- PDF 指南、安装脚本等静态资源

**不变更**：
- Tauri updater 自动更新端点（`tauri.conf.json`、`channel.rs`）
- Mobile OTA 更新端点（K2Plugin.swift、K2PluginUtils.kt）
- k2 子模块内所有 URL（只读子模块）
- S3 bucket 和现有 CloudFront distribution（长期保留）

## 基础设施变更（手动操作）

### 1. ACM 证书
- Region: `us-east-1`（CloudFront 要求）
- Domain: `dl.kaitu.io`

### 2. CloudFront Distribution
- Origin: S3 bucket `d0.all7.cc`
- Alternate domain name: `dl.kaitu.io`
- SSL certificate: 上述 ACM 证书
- 缓存策略: 与现有 `d13jc1jqzlg4yt.cloudfront.net` distribution 一致

### 3. DNS
- CNAME `dl.kaitu.io` → `{new-distribution-id}.cloudfront.net`

### 路径结构
保持原样，与 S3 key 一致。新 distribution 直通整个 S3 bucket，所有路径均可通过 `dl.kaitu.io` 访问：
```
dl.kaitu.io/kaitu/desktop/{version}/Kaitu_{version}_x64.exe
dl.kaitu.io/kaitu/desktop/{version}/Kaitu_{version}_universal.pkg
dl.kaitu.io/kaitu/desktop/{version}/Kaitu_{version}_amd64.AppImage
dl.kaitu.io/kaitu/android/{version}/Kaitu-{version}.apk
dl.kaitu.io/kaitu/k2/{version}/k2-{os}-{arch}          # k2 binary
dl.kaitu.io/kaitu/guides/*.pdf                          # PDF guides
```

## Fallback 策略

| 优先级 | 域名 | 类型 | 说明 |
|--------|------|------|------|
| Primary | `dl.kaitu.io` | 新 CloudFront distribution | 用户可见的官方下载域名 |
| Backup | `d13jc1jqzlg4yt.cloudfront.net` | 现有 CloudFront distribution | 长期保留，作为备用 |

`d0.all7.cc` 作为 S3 bucket origin 继续存在，但不再出现在用户可见的下载链接中。

## 统一安装脚本 `web/public/i/k2`

### 现状
- `web/public/install.sh` — 旧版 k2/k2s CLI 安装脚本（只装 binary 到 `/usr/local/bin/`）
- `web/public/i/k2s` — k2s 服务端安装脚本（独立文件）
- `scripts/install-linux.sh` — Linux 桌面安装脚本（AppImage + daemon + systemd），**未部署**到 web/public/
- `web/public/i/k2` — **不存在**，但 `install.sh` 帮助信息已指向此路径

### 设计

创建 `web/public/i/k2` 作为桌面客户端统一安装入口：

```
curl -fsSL https://kaitu.io/i/k2 | sudo bash
```

脚本内部通过 `uname -s` 判断 OS，分流处理：

| OS | 行为 |
|---|---|
| Linux | 下载 AppImage + k2 daemon，安装 systemd service，创建 desktop entry（合并自 `scripts/install-linux.sh`）|
| macOS | 下载 PKG，通过 `installer -pkg` 安装 |
| Windows | 不适用（`curl \| bash` 不是 Windows 场景）|

Windows 用户通过网站 install 页面下载 EXE，浏览器直接触发下载。

### 脚本结构

```bash
#!/bin/sh
set -e

CDN_PRIMARY="https://dl.kaitu.io/kaitu"
CDN_FALLBACK="https://d13jc1jqzlg4yt.cloudfront.net/kaitu"

detect_platform()  # uname -s → linux/darwin, uname -m → amd64/arm64

get_latest_version()  # 从 CDN manifest 获取最新版本

install_linux()
  # 检查 webkit2gtk-4.1, libfuse2
  # 下载 AppImage → /opt/kaitu/Kaitu.AppImage
  # 下载 k2 daemon → /opt/kaitu/k2, symlink /usr/local/bin/k2
  # 安装 systemd service (k2 service install)
  # 创建 desktop entry
  # 创建 kaitu-uninstall

install_macos()
  # 下载 PKG → /tmp/
  # installer -pkg /tmp/Kaitu_xxx.pkg -target /
  # 清理临时文件

main()
  detect_platform
  get_latest_version
  case $OS in
    linux)  install_linux ;;
    darwin) install_macos ;;
  esac
```

### 废弃
- `scripts/install-linux.sh` — 逻辑合并到 `web/public/i/k2` 后可删除
- `web/public/install.sh` — 保留作为旧 URL 兼容（`kaitu.io/install.sh`），帮助信息已指向新路径

## 代码变更

### web/src/lib/constants.ts
```typescript
// Before
export const CDN_PRIMARY = 'https://d13jc1jqzlg4yt.cloudfront.net/kaitu/desktop';
export const CDN_BACKUP = 'https://d0.all7.cc/kaitu/desktop';

// After
export const CDN_PRIMARY = 'https://dl.kaitu.io/kaitu/desktop';
export const CDN_BACKUP = 'https://d13jc1jqzlg4yt.cloudfront.net/kaitu/desktop';
```

### web/src/app/[locale]/support/page.tsx
PDF guide 链接域名从 `d13jc1jqzlg4yt.cloudfront.net` 改为 `dl.kaitu.io`。

### scripts/generate-changelog.js
```javascript
// Before
const CDN_PRIMARY = 'https://d13jc1jqzlg4yt.cloudfront.net/kaitu/desktop';
const CDN_BACKUP = 'https://d0.all7.cc/kaitu/desktop';

// After
const CDN_PRIMARY = 'https://dl.kaitu.io/kaitu/desktop';
const CDN_BACKUP = 'https://d13jc1jqzlg4yt.cloudfront.net/kaitu/desktop';
```

### web/public/i/k2（新建）
统一桌面客户端安装脚本，合并 `scripts/install-linux.sh` 的 Linux 逻辑 + 新增 macOS PKG 安装。
使用 `dl.kaitu.io` 作为 primary CDN，`d13jc1jqzlg4yt.cloudfront.net` 作为 fallback。
详见上方「统一安装脚本」章节。

### scripts/install-linux.sh（删除）
逻辑已合并到 `web/public/i/k2`，删除此文件。

### web/public/install.sh
```bash
# Before
CDN_PRIMARY="https://d13jc1jqzlg4yt.cloudfront.net/kaitu/k2"
CDN_FALLBACK="https://d0.all7.cc/kaitu/k2"

# After
CDN_PRIMARY="https://dl.kaitu.io/kaitu/k2"
CDN_FALLBACK="https://d13jc1jqzlg4yt.cloudfront.net/kaitu/k2"
```

### web/public/i/k2s
```bash
# Before
CDN_PRIMARY="https://d13jc1jqzlg4yt.cloudfront.net/kaitu/k2"
CDN_FALLBACK="https://d0.all7.cc/kaitu/k2"

# After
CDN_PRIMARY="https://dl.kaitu.io/kaitu/k2"
CDN_FALLBACK="https://d13jc1jqzlg4yt.cloudfront.net/kaitu/k2"
```

### .github/workflows/release-desktop.yml
Slack 通知中的下载链接（团队成员可见）：
```yaml
# Before (line 412)
CDN_BASE="https://d13jc1jqzlg4yt.cloudfront.net/kaitu/desktop/${VERSION}"

# After
CDN_BASE="https://dl.kaitu.io/kaitu/desktop/${VERSION}"
```

### .github/workflows/build-mobile.yml
Slack 通知中的 Android APK 下载链接：
```yaml
# Before (line 337)
CDN_BASE="https://d13jc1jqzlg4yt.cloudfront.net/kaitu/android"

# After
CDN_BASE="https://dl.kaitu.io/kaitu/android"
```

### scripts/generate-changelog.js — 后续操作
修改脚本后需重新生成 `web/public/changelog.json`：
```bash
node scripts/generate-changelog.js
```

### web/src/lib/constants.ts — DOWNLOAD_LINKS.android 补充说明
`DOWNLOAD_LINKS.android` 当前指向 `/waymaker/` 路径（旧产品 APK），不在本次范围内。待 Android 正式发布后使用 `getDownloadLinks()` 动态生成链接时统一切换。

## 不变更的文件

| 文件 | 原因 |
|------|------|
| `desktop/src-tauri/tauri.conf.json` | 自动更新端点 |
| `desktop/src-tauri/src/channel.rs` | 自动更新通道 |
| `mobile/plugins/k2-plugin/ios/Plugin/K2Plugin.swift` | OTA 更新端点 |
| `mobile/plugins/k2-plugin/android/.../K2PluginUtils.kt` | OTA 更新端点 |
| `k2/` 子模块内所有文件 | 只读子模块 |
| `.github/workflows/release-desktop.yml` S3 上传行 | S3 上传目标，不是用户可见链接 |
| `.github/workflows/release-k2s.yml` | k2s 发布 manifest 生成，updater 用途 |
| `.github/workflows/release-openwrt.yml` | OpenWrt S3 上传 |
| `scripts/publish-desktop.sh` | 生成 latest.json manifest，Tauri updater 用途 |
| `scripts/publish-mobile.sh` | 生成 latest.json manifest，OTA 用途 |
| `scripts/publish-k2.sh` | k2 binary 发布，manifest 生成 |
| `tools/kaitu-signer/config.py` | S3 bucket 名称配置（`d0.all7.cc`），不变 |
