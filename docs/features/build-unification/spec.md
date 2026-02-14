# Spec: build-unification — macOS PKG 构建修复

## Context

`fc2bd2a` 提交了 build-unification 的主要改动（8 files），但实际执行 `make build-macos-fast` 时发现 4 个阻塞性问题。修复已在工作区但未提交。

## AC

1. `scripts/build-macos.sh` 中 Go 交叉编译使用显式 GOARCH/GOOS
2. `scripts/build-macos.sh` 使用 lipo 创建 universal k2 binary
3. `scripts/build-macos.sh` --skip-notarization 时 unset Apple 环境变量防止 Tauri 自动公证
4. `scripts/build-macos.sh` pkgbuild 使用 staging 目录 + 正确的 component plist
5. `scripts/test_build.sh` 14/14 pass（不退化）
6. `desktop/src-tauri/Cargo.toml` tauri-build 依赖格式修正
