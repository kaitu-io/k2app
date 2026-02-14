# Plan: build-unification — macOS PKG 构建修复

## Meta

| Field | Value |
|-------|-------|
| Feature | build-unification |
| Date | 2026-02-14 |
| Complexity | Simple (2 files) |
| Base commit | fc2bd2a |

## Task: T1 — 修复 build-macos.sh + Cargo.toml

**Files**: `scripts/build-macos.sh`, `desktop/src-tauri/Cargo.toml`
**Depends on**: none

### 修改内容

#### 1. `scripts/build-macos.sh` — 4 处修复

**Fix 1: Go 交叉编译需要显式 GOARCH/GOOS**

问题：arm64 机器上不设 GOARCH，两个 target 都编译成 arm64，lipo 报 "same architectures"。

将第 38 行附近：
```
make build-k2 TARGET=aarch64-apple-darwin
```
改为：
```
GOARCH=arm64 GOOS=darwin make build-k2 TARGET=aarch64-apple-darwin
```

将第 41 行附近：
```
make build-k2 TARGET=x86_64-apple-darwin
```
改为：
```
GOARCH=amd64 GOOS=darwin make build-k2 TARGET=x86_64-apple-darwin
```

**Fix 2: lipo 创建 universal k2 binary**

问题：Tauri `--target universal-apple-darwin` 查找 `k2-universal-apple-darwin`，但只有 arch-specific 版本。

在两个 build-k2 之后、tauri build 之前插入：
```bash
# --- Create universal k2 binary with lipo ---
echo ""
echo "--- Creating universal k2 binary ---"
K2_BIN_DIR="desktop/src-tauri/binaries"
lipo -create \
  "$K2_BIN_DIR/k2-aarch64-apple-darwin" \
  "$K2_BIN_DIR/k2-x86_64-apple-darwin" \
  -output "$K2_BIN_DIR/k2-universal-apple-darwin"
chmod +x "$K2_BIN_DIR/k2-universal-apple-darwin"
echo "Created universal binary: $K2_BIN_DIR/k2-universal-apple-darwin"
```

**Fix 3: --skip-notarization 时 unset Apple 环境变量**

问题：Tauri bundler 检测到 APPLE_ID/APPLE_PASSWORD/APPLE_TEAM_ID 环境变量后自动公证，即使脚本层已跳过。

在 `yarn tauri build` 之前插入：
```bash
if [ "$SKIP_NOTARIZATION" = true ]; then
  # Unset Apple credentials to prevent Tauri's built-in notarization
  unset APPLE_ID APPLE_PASSWORD APPLE_TEAM_ID
fi
```

**Fix 4: pkgbuild 使用 staging 目录 + 正确的 component plist**

问题：
- `--root $BUNDLE_DIR` 会把 `.app.tar.gz` 和 `.sig` 也打包进 .pkg
- `--component-plist /dev/stdin` 在 macOS 上不工作
- component plist 缺少 `RootRelativeBundlePath` 等必需字段

替换整个 pkgbuild 段为：
```bash
# Stage only the .app for pkgbuild (exclude updater artifacts)
PKG_STAGE=$(mktemp -d /tmp/k2app-pkg-stage.XXXXXX)
cp -R "$APP_PATH" "$PKG_STAGE/"

# Create component plist with BundleIsRelocatable=false
COMPONENT_PLIST=$(mktemp /tmp/k2app-component.XXXXXX)
cat > "$COMPONENT_PLIST" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<array>
  <dict>
    <key>BundleHasStrictIdentifier</key>
    <true/>
    <key>BundleIsRelocatable</key>
    <false/>
    <key>BundleIsVersionChecked</key>
    <false/>
    <key>BundleOverwriteAction</key>
    <string>upgrade</string>
    <key>RootRelativeBundlePath</key>
    <string>Kaitu.app</string>
  </dict>
</array>
</plist>
PLIST

pkgbuild \
  --root "$PKG_STAGE" \
  --component-plist "$COMPONENT_PLIST" \
  --identifier io.kaitu.desktop \
  --version "$VERSION" \
  --install-location "/Applications" \
  "$PKG_UNSIGNED"

rm -rf "$PKG_STAGE" "$COMPONENT_PLIST"
```

#### 2. `desktop/src-tauri/Cargo.toml` — 格式修正

```diff
-tauri-build = "2"
+tauri-build = { version = "2", features = [] }
```

### Verification

```bash
# 基础检查
scripts/test_build.sh            # 14/14 pass

# 构建已验证通过（上一轮会话），产物在：
# release/0.4.0/Kaitu-0.4.0.pkg (29.3 MB, xar archive, productsign 签名)
# release/0.4.0/Kaitu.app.tar.gz (29.8 MB)
# release/0.4.0/Kaitu.app.tar.gz.sig (404 B)
```

## AC Mapping

| AC | Test | Task |
|----|------|------|
| Go 交叉编译 GOARCH/GOOS | `make build-macos-fast` 不报 lipo same arch 错误 | T1 |
| lipo universal binary | `k2-universal-apple-darwin` 存在且可执行 | T1 |
| --skip-notarization 防 Tauri 自动公证 | `make build-macos-fast` 不报 notarization 错误 | T1 |
| pkgbuild staging + plist | `.pkg` 只含 `.app`，不含 `.tar.gz`/`.sig` | T1 |
| 14/14 不退化 | `scripts/test_build.sh` 14/14 pass | T1 |
| Cargo.toml 格式 | `cargo check` pass | T1 |
