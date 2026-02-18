# Feature: CI/CD & Release Pipeline

## Meta

| Field | Value |
|-------|-------|
| Feature | cicd-release-pipeline |
| Version | v1 |
| Status | implemented |
| Created | 2026-02-18 |
| Updated | 2026-02-18 |

## Overview

k2app 的 CI/CD 管线覆盖 5 个 GitHub Actions workflow + 6 个构建脚本 + 1 个发布脚本 + 1 个部署脚本 + 1 个验证脚本。流水线支持 6 个平台目标：macOS (universal binary)、Windows (x86_64)、iOS、Android、OpenWrt (aarch64/x86_64/armv7/mipsle)。版本唯一来源是根目录 `package.json`，通过 Makefile `pre-build` 传播到所有平台。

关键设计原则：
- **Shared scripts**: CI 和本地开发共用同一套 `scripts/*.sh`，workflow 只做环境准备 + 调用脚本
- **Version single source**: `package.json` → `version.json`（webapp）、ldflags（k2 Go binary）、`Cargo.toml`（Tauri，通过 `"../../package.json"` 引用）
- **Private submodule access**: k2 submodule 通过 SSH deploy key（`K2_DEPLOY_KEY`）+ `git insteadOf` URL 重写
- **Signing separation**: macOS 在 CI runner 内签名 + notarize；Windows 使用 S3 + SQS 异步签名服务（kaitu-signer）
- **Artifact distribution**: S3 (d0.all7.cc) 作为主存储，CloudFront 作为 CDN，App Store Connect（iOS），GitHub Release（release notes）

## Product Requirements

### P0 — Core CI

1. **Push/PR 自动测试**: main 分支的 push 和 PR 触发 CI，包含 vitest、tsc、cargo check、cargo test、K2Plugin tsc
2. **Concurrency 控制**: 同一 ref 的 CI 自动取消旧 run（`cancel-in-progress: true`）
3. **Desktop release**: `v*` tag push 或 manual dispatch 触发 macOS + Windows 并行构建

### P1 — Mobile & OpenWrt

4. **iOS build**: `v*` tag 或 manual dispatch（可选平台），包含 gomobile bind → xcframework → archive → IPA → App Store Connect 上传
5. **Android build**: `v*` tag 或 manual dispatch，包含 gomobile bind → AAR → Gradle assembleRelease → APK
6. **OpenWrt cross-compile**: `v*` tag 或 manual dispatch，4 架构矩阵（aarch64、x86_64、armv7、mipsle），含 qemu smoke test

### P2 — 辅助流程

7. **Antiblock config publish**: Manual dispatch，加密 entry URLs 后 push 到 ui-theme repo 的 dist 分支，CDN cache purge
8. **Center API deploy**: 半自动部署（`make deploy-api`），使用 devops 工具上传 + systemd 管理
9. **Build verification**: `scripts/test_build.sh` 14 项检查，可选 `--full` 进行完整 macOS 构建验证
10. **Slack notifications**: 构建成功/失败自动通知，两个 channel（alert + release）

## Technical Decisions

### TD-1: CI Pipeline 架构

**CI workflow** (`ci.yml`) 在 `ubuntu-latest` 单 runner 上顺序执行：

| Step | 工具 | 用途 |
|------|------|------|
| vitest | Node 20 + yarn | webapp 单元测试 |
| tsc --noEmit (webapp) | TypeScript | webapp 类型检查 |
| tsc --noEmit (k2-plugin) | TypeScript | mobile 插件类型检查 |
| cargo check | Rust stable | Tauri Rust 编译验证 |
| cargo test | Rust stable | Tauri Rust 测试 |

Linux 系统依赖：`libwebkit2gtk-4.1-dev`, `librsvg2-dev`, `patchelf`, `libssl-dev`, `libgtk-3-dev`, `libayatana-appindicator3-dev`

缓存策略：
- Node: yarn cache
- Go: `go-version: 1.24`, cache key from `k2/go.sum`
- Rust: `~/.cargo/registry` + `~/.cargo/git` + `desktop/src-tauri/target`, key from `Cargo.lock`

### TD-2: Desktop Release 流程

**macOS 构建** (runner: `macos-latest`):

```
make pre-build → make build-webapp
  → build k2 aarch64-apple-darwin (GOARCH=arm64)
  → build k2 x86_64-apple-darwin (GOARCH=amd64)
  → lipo -create → universal binary
  → yarn tauri build --target universal-apple-darwin --config tauri.bundle.conf.json
  → codesign --verify --deep --strict
  → pkgbuild → productsign (APPLE_INSTALLER_IDENTITY)
  → notarytool submit --wait → stapler staple
  → collect .app.tar.gz + .app.tar.gz.sig (Tauri updater)
  → aws s3 cp release/{version}/ → s3://d0.all7.cc/kaitu/desktop/{version}/
```

macOS 签名证书通过 base64 编码的 `.p12` 导入临时 keychain，workflow 结束后自动删除 keychain。

**Windows 构建** (runner: `windows-latest`):

```
make pre-build → make build-webapp
  → make build-k2 TARGET=x86_64-pc-windows-msvc
  → yarn tauri build --target x86_64-pc-windows-msvc
  → 上传 unsigned .exe + .exe.sig 到 S3 signing/pending/{run_id}/
  → 发送 SQS 消息到 kaitu-signer
  → wait-for-signing.sh 轮询 S3 signing/completed/{run_id}/status.json（10s 间隔，600s 超时）
  → 下载 signed .exe
  → 上传 signed .exe + .exe.sig 到 s3://d0.all7.cc/kaitu/desktop/{version}/
```

Windows 代码签名使用外部 kaitu-signer 服务（基于 AWS SQS），不在 CI runner 内完成。签名证书为 "Wordgate LLC"，时间戳服务器 `http://timestamp.sectigo.com`。

### TD-3: Tauri Updater 签名

Tauri 内置 updater 使用独立的 minisign 密钥对（`TAURI_SIGNING_PRIVATE_KEY` / `TAURI_SIGNING_PRIVATE_KEY_PASSWORD`），生成 `.app.tar.gz.sig`（macOS）和 `.exe.sig`（Windows）。这不同于操作系统级的代码签名。

发布时 `publish-release.sh` 生成两份 `latest.json`：
- `cloudfront.latest.json` — 指向 CloudFront CDN URL
- `d0.latest.json` — 指向 S3 直接 URL

两份文件包含相同的 version、signature、pub_date，供 Tauri updater 检查更新。

### TD-4: Mobile Build 流程

**iOS 构建流程**:

```
gomobile bind -target=ios → K2Mobile.xcframework
  → cp xcframework → mobile/ios/App/
  → npx cap sync ios → pod install --repo-update
  → xcodebuild archive (App Store Connect API key for automatic signing)
  → xcodebuild -exportArchive → IPA
  → xcrun altool --upload-app (App Store Connect)
  → upload-mobile-s3.sh --web --ios
```

App Store Connect API key 通过 `AuthKey_{KEY_ID}.p8` 文件提供，支持 automatic provisioning。

**Android 构建流程**:

```
gomobile bind -target=android -androidapi 24 → k2mobile.aar
  → cp AAR → mobile/android/app/libs/
  → npx cap sync android
  → ./gradlew assembleRelease
  → upload-mobile-s3.sh --android
```

Android 环境：Java 17 (Temurin)、NDK 26.1.10909125。

### TD-5: OpenWrt Cross-Compile

4 架构矩阵，CGO_ENABLED=0 纯静态编译：

| GOARCH | GOARM | 输出名 |
|--------|-------|--------|
| arm64 | — | aarch64 |
| amd64 | — | x86_64 |
| arm | 7 | armv7 |
| mipsle | — | mipsle |

OpenWrt 构建与 desktop 不同：webapp 嵌入 k2 binary（`cp -r webapp/dist k2/cloud/dist`），不使用 `-tags nowebapp`。

打包内容：`k2` binary + `install.sh` + `k2.init`（OpenRC init script）+ `luci-app-k2/`（LuCI web 界面）。

每个架构构建后用 qemu-user-static 运行 `k2 version` 做 smoke test。

### TD-6: S3 Artifact Layout

```
s3://d0.all7.cc/kaitu/
├── desktop/
│   ├── {version}/
│   │   ├── Kaitu-{version}.pkg                    # macOS installer
│   │   ├── Kaitu.app.tar.gz                       # macOS updater bundle
│   │   ├── Kaitu.app.tar.gz.sig                   # macOS updater signature
│   │   ├── Kaitu_{version}_x64-setup.exe          # Windows installer (signed)
│   │   └── Kaitu_{version}_x64-setup.exe.sig      # Windows updater signature
│   ├── cloudfront.latest.json                     # Tauri updater manifest (CloudFront URLs)
│   └── d0.latest.json                             # Tauri updater manifest (S3 direct URLs)
├── web/
│   ├── {version}/webapp.zip                       # Web OTA bundle
│   └── latest.json                                # Web OTA manifest (version/url/hash/size)
├── android/
│   ├── {version}/Kaitu-{version}.apk              # Android APK
│   └── latest.json                                # Android manifest (version/url/hash/size)
├── ios/
│   └── latest.json                                # iOS manifest (version/appstore_url)
├── openwrt/
│   └── {version}/
│       ├── k2-openwrt-aarch64-v{version}.tar.gz
│       ├── k2-openwrt-x86_64-v{version}.tar.gz
│       ├── k2-openwrt-armv7-v{version}.tar.gz
│       └── k2-openwrt-mipsle-v{version}.tar.gz
└── signing/                                       # Windows signing queue (transient)
    ├── pending/{run_id}/                          # Unsigned artifacts
    └── completed/{run_id}/                        # Signed artifacts + status.json
```

### TD-7: Antiblock Config Publish

手动触发的 workflow，用于更新 CDN antiblock 入口 URL 配置：

1. `antiblock-encrypt.js` 使用 AES-256-GCM 加密 entry URL 列表
2. 输出 JSONP 格式：`window.__k2ac={"v":1,"data":"<base64>"};`
3. Push 到 `kaitu-io/ui-theme` repo 的 `dist` 分支（config.js）
4. 通过 jsDelivr CDN 分发，publish 后自动 purge cache

加密密钥硬编码在 `antiblock-encrypt.js` 和 webapp 解密端（`webapp/src/api/antiblock.ts`），对称密钥。

### TD-8: Version 传播链

```
package.json (0.4.0)
  ├── make pre-build → webapp/public/version.json
  ├── Makefile VERSION → go build -ldflags "-X main.version={VERSION}"
  ├── desktop/src-tauri/tauri.conf.json → "version": "../../package.json"
  ├── publish-release.sh → GitHub Release tag v{VERSION}
  └── upload-mobile-s3.sh → S3 path + latest.json version field
```

`test_build.sh` 验证 package.json、version.json、Cargo.toml 三者版本一致。

### TD-9: Secrets 管理

通过 `scripts/ci/setup-secrets.sh` + `push-secrets.sh` 管理 GitHub Actions secrets：

| Secret | 用途 |
|--------|------|
| `K2_DEPLOY_KEY` | SSH deploy key for k2 private submodule |
| `APPLE_CERTIFICATE` | base64 .p12 (Developer ID Application + Installer) |
| `APPLE_CERTIFICATE_PASSWORD` | .p12 密码 |
| `APPLE_ID` | Apple ID email (notarization) |
| `APPLE_PASSWORD` | App-specific password (notarization) |
| `APPLE_TEAM_ID` | Apple Developer Team ID |
| `APPLE_INSTALLER_IDENTITY` | Developer ID Installer identity (pkg signing) |
| `TAURI_SIGNING_PRIVATE_KEY` | Tauri updater minisign private key |
| `TAURI_SIGNING_PRIVATE_KEY_PASSWORD` | Tauri updater key password |
| `AWS_ACCESS_KEY_ID` | IAM user for S3 upload |
| `AWS_SECRET_ACCESS_KEY` | IAM secret |
| `SQS_SIGNING_QUEUE_URL` | Windows signing SQS queue |
| `APP_STORE_CONNECT_API_KEY_BASE64` | ASC API key (.p8, base64) |
| `APP_STORE_CONNECT_KEY_ID` | ASC API key ID |
| `APP_STORE_CONNECT_ISSUER_ID` | ASC issuer ID |
| `UI_THEME_DEPLOY_KEY` | SSH deploy key for ui-theme repo (antiblock) |
| `SLACK_WEBHOOK_ALERT` | Slack webhook for build/test alerts |
| `SLACK_WEBHOOK_RELEASE` | Slack webhook for release notifications |

### TD-10: Slack 通知

`scripts/ci/notify-slack.sh` 支持 4 种通知类型：

| Type | Channel | 触发条件 |
|------|---------|---------|
| `deploy-success` | `SLACK_WEBHOOK_RELEASE` | Desktop/OpenWrt 构建成功 |
| `build-failure` | `SLACK_WEBHOOK_ALERT` | Desktop/OpenWrt 构建失败 |
| `test-failure` | `SLACK_WEBHOOK_ALERT` | 测试失败 |
| `test-success` | `SLACK_WEBHOOK_ALERT` | 测试通过 |

通知包含 workflow name、commit SHA、触发者、workflow run 链接。

### TD-11: Center API 部署

半自动流程（`make deploy-api` → `scripts/deploy-center.sh`）：

1. 本地 cross-compile：`GOOS=linux GOARCH=amd64 go build -o release/kaitu-center`
2. `devops server upload-file` 上传到 center 集群
3. `devops server run-cmd` 执行 `kaitu-center install -c config.yml`（systemd 注册）
4. 暂停等待管理员确认（可选 migrate）
5. `systemctl restart kaitu-center`

### TD-12: Build Verification (test_build.sh)

14 项检查（基础模式），`--full` 模式额外增加 macOS 构建验证：

**基础模式 (14 checks)**:

| 分类 | 检查项 |
|------|--------|
| Version Consistency | package.json version = 0.4.0 |
| Version Consistency | version.json matches package.json |
| Version Consistency | Cargo.toml version matches package.json |
| Webapp Build | yarn build succeeded |
| Webapp Build | dist/index.html exists |
| Webapp Build | dist/assets/ contains JS files |
| Webapp Build | dist/assets/ contains CSS files |
| k2 Go Build | build-k2.sh succeeded |
| k2 Go Build | k2 binary exists |
| k2 Go Build | k2 binary is executable |
| Cargo Check | cargo check succeeded |
| Test Suites | vitest passed |
| Test Suites | cargo test passed |
| TypeScript Check | tsc --noEmit passed |

**Full 模式 (追加)**:
- make build-macos-fast succeeded
- .pkg exists + valid xar archive
- .app.tar.gz exists
- .app.tar.gz.sig exists
- codesign --verify --deep passed
- pkgutil --payload-files succeeded

## Key Files

### GitHub Actions Workflows

| 文件 | 触发 | 用途 |
|------|------|------|
| `.github/workflows/ci.yml` | push/PR to main | 测试 + 类型检查 + Rust check |
| `.github/workflows/release-desktop.yml` | `v*` tag / manual | macOS + Windows signed desktop release |
| `.github/workflows/build-mobile.yml` | `v*` tag / manual | iOS + Android build + publish |
| `.github/workflows/release-openwrt.yml` | `v*` tag / manual | OpenWrt 4-arch cross-compile |
| `.github/workflows/publish-antiblock.yml` | manual | Antiblock config encrypt + CDN publish |

### Build Scripts

| 文件 | 用途 |
|------|------|
| `scripts/build-macos.sh` | macOS universal binary + PKG + sign + notarize |
| `scripts/build-mobile-ios.sh` | iOS gomobile bind + cap sync + archive + IPA |
| `scripts/build-mobile-android.sh` | Android gomobile bind + cap sync + Gradle build |
| `scripts/build-openwrt.sh` | OpenWrt 4-arch cross-compile + package |
| `scripts/build-k2.sh` | 单架构 k2 Go binary 构建 |
| `scripts/publish-release.sh` | 生成 latest.json + S3 上传 + GitHub Release |
| `scripts/deploy-center.sh` | Center API 部署（devops 工具 + systemd） |
| `scripts/test_build.sh` | 14-check 构建验证 |
| `scripts/antiblock-encrypt.js` | Antiblock config AES-256-GCM 加密 |

### CI Helper Scripts

| 文件 | 用途 |
|------|------|
| `scripts/ci/wait-for-signing.sh` | 轮询 S3 等待 Windows 签名完成 |
| `scripts/ci/upload-mobile-s3.sh` | Mobile artifact S3 上传 + latest.json 生成 |
| `scripts/ci/notify-slack.sh` | Slack 通知（4 种类型） |
| `scripts/ci/setup-secrets.sh` | Secrets 模板（本地填入值） |
| `scripts/ci/push-secrets.sh` | 批量推送 secrets 到 GitHub |

### Makefile Targets

| Target | 用途 |
|--------|------|
| `make dev` | 本地开发（k2 daemon + Vite HMR + Tauri） |
| `make pre-build` | 生成 version.json |
| `make build-k2` | Go build k2 binary |
| `make build-webapp` | yarn build webapp |
| `make build-macos` | macOS full build (signed + notarized) |
| `make build-macos-fast` | macOS build (skip notarization) |
| `make build-windows` | Windows build |
| `make build-openwrt` | OpenWrt cross-compile |
| `make build-mobile-ios` | iOS full build |
| `make build-mobile-android` | Android full build |
| `make mobile-ios` | gomobile bind iOS only |
| `make mobile-android` | gomobile bind Android only |
| `make deploy-api` | Center API build + deploy |
| `make publish-release` | S3 + GitHub Release publish |
| `make clean` | 清理构建产物 |

## Acceptance Criteria

### CI Pipeline

- [x] Push to main 触发 CI workflow
- [x] PR to main 触发 CI workflow
- [x] 同一 ref 的并发 CI 自动取消旧 run
- [x] vitest 测试通过
- [x] webapp tsc --noEmit 通过
- [x] K2Plugin tsc --noEmit 通过
- [x] cargo check 通过
- [x] cargo test 通过
- [x] k2 submodule 通过 SSH deploy key 正确 clone

### Desktop Release

- [x] `v*` tag push 触发 macOS + Windows 并行构建
- [x] workflow_dispatch 支持手动触发
- [x] macOS 生成 universal binary (arm64 + x86_64 via lipo)
- [x] macOS .app 通过 codesign --verify --deep --strict
- [x] macOS .pkg 使用 productsign 签名
- [x] macOS .pkg 通过 notarytool + stapler 公证
- [x] macOS --skip-notarization 选项正常工作
- [x] macOS 产物上传到 S3 (release/{version}/)
- [x] Windows 构建 NSIS 安装程序
- [x] Windows unsigned artifacts 上传到 S3 signing queue
- [x] Windows SQS 签名请求发送成功
- [x] Windows wait-for-signing.sh 正确轮询（10s 间隔，600s 超时）
- [x] Windows signed artifacts 下载并上传到 S3
- [x] Tauri updater .sig 文件（macOS + Windows）正确生成并上传
- [x] 构建成功/失败触发 Slack 通知
- [x] 临时 keychain 构建结束后清理（`always()` guard）

### Mobile Build

- [x] `v*` tag push 触发 iOS + Android 并行构建
- [x] workflow_dispatch 支持选择平台（ios/android/both）
- [x] iOS: gomobile bind → xcframework → cap sync → archive → IPA
- [x] iOS: App Store Connect API key 自动签名
- [x] iOS: IPA 上传到 App Store Connect（altool）
- [x] iOS: Web OTA bundle + manifest 上传到 S3
- [x] Android: gomobile bind → AAR → cap sync → Gradle assembleRelease
- [x] Android: APK 上传到 S3 + latest.json 生成
- [x] GitHub Actions artifact 保留 30 天

### OpenWrt Release

- [x] `v*` tag push 触发 4 架构并行构建
- [x] CGO_ENABLED=0 纯静态编译
- [x] webapp 嵌入 k2 binary（不使用 nowebapp tag）
- [x] qemu smoke test 验证 binary 架构正确
- [x] 打包包含 install.sh + k2.init + luci-app-k2
- [x] tar.gz 上传到 S3
- [x] 构建成功/失败触发 Slack 通知

### Antiblock Publish

- [x] workflow_dispatch 手动触发
- [x] 支持自定义 entry URLs（JSON array input）
- [x] 默认 entry URLs fallback
- [x] AES-256-GCM 加密输出 JSONP 格式
- [x] Push 到 ui-theme repo dist 分支
- [x] jsDelivr CDN cache purge

### Version Propagation

- [x] package.json 为唯一版本源
- [x] version.json 由 make pre-build 生成，版本一致
- [x] Cargo.toml 通过 tauri.conf.json 引用根 package.json
- [x] Go binary 通过 ldflags 注入版本
- [x] S3 路径包含版本号
- [x] GitHub Release tag 与版本一致

### Build Verification

- [x] test_build.sh 基础模式 14 项检查全部覆盖
- [x] test_build.sh --full 模式验证完整 macOS 构建
- [x] 版本一致性检查（package.json vs version.json vs Cargo.toml）
- [x] 所有检查失败时 exit code 1

### Publish & Deploy

- [x] publish-release.sh 生成 cloudfront.latest.json + d0.latest.json
- [x] publish-release.sh 创建 GitHub Release（含平台下载说明表格）
- [x] deploy-center.sh 支持 upload + install + restart 流程
- [x] deploy-center.sh 暂停等待管理员确认
