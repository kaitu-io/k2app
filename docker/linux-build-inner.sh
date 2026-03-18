#!/usr/bin/env bash
set -euo pipefail
# Inner build script — runs INSIDE the Docker container (ARM64 native).
# Cross-compiles Tauri binary to x86_64 using gcc-x86-64-linux-gnu + multiarch libs.

cd /src

export CARGO_HOME="/cargo-cache"
K2_BUILD_LOG_LEVEL="${K2_BUILD_LOG_LEVEL:-debug}"
export K2_BUILD_LOG_LEVEL

echo "--- Installing Linux-native JS deps ---"
yarn install --frozen-lockfile 2>&1 | tail -3

echo "--- Cross-compiling Tauri binary (ARM64 → x86_64) ---"
cd desktop && yarn tauri build --no-bundle --target x86_64-unknown-linux-gnu

echo "--- Done ---"
ls -lh src-tauri/target/x86_64-unknown-linux-gnu/release/k2app
