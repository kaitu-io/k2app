#!/usr/bin/env bash
# build.sh — Compile K2NEHelper.swift to a macOS static library (libk2_ne_helper.a)
#
# Usage:
#   ./build.sh                    # Build for arm64 (default, Apple Silicon)
#   ./build.sh --arch x86_64      # Build for x86_64 (Intel)
#   ./build.sh --arch universal   # Build universal binary (arm64 + x86_64)
#
# Output: libk2_ne_helper.a in the ne_helper/ directory

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SWIFT_FILE="$SCRIPT_DIR/K2NEHelper.swift"
MIN_MACOS="11"

ARCH="arm64"
UNIVERSAL=false

# Parse arguments
while [[ $# -gt 0 ]]; do
    case "$1" in
        --arch)
            ARCH="$2"
            shift 2
            ;;
        *)
            echo "Unknown argument: $1" >&2
            exit 1
            ;;
    esac
done

if [[ "$ARCH" == "universal" ]]; then
    UNIVERSAL=true
fi

SDK_PATH="$(xcrun --show-sdk-path --sdk macosx)"

build_arch() {
    local target_arch="$1"
    local target="$target_arch-apple-macos$MIN_MACOS"
    local out="$SCRIPT_DIR/libk2_ne_helper_$target_arch.a"

    echo "[build.sh] Compiling K2NEHelper.swift for $target..."
    swiftc \
        -emit-library \
        -static \
        -module-name K2NEHelper \
        -sdk "$SDK_PATH" \
        -target "$target" \
        -O \
        "$SWIFT_FILE" \
        -o "$out"
    echo "[build.sh] Built: $out"
}

if [[ "$UNIVERSAL" == true ]]; then
    build_arch "arm64"
    build_arch "x86_64"

    echo "[build.sh] Creating universal binary with lipo..."
    lipo -create \
        "$SCRIPT_DIR/libk2_ne_helper_arm64.a" \
        "$SCRIPT_DIR/libk2_ne_helper_x86_64.a" \
        -output "$SCRIPT_DIR/libk2_ne_helper.a"

    # Clean up per-arch intermediates
    rm -f "$SCRIPT_DIR/libk2_ne_helper_arm64.a" "$SCRIPT_DIR/libk2_ne_helper_x86_64.a"
    echo "[build.sh] Universal binary: $SCRIPT_DIR/libk2_ne_helper.a"
else
    build_arch "$ARCH"
    mv "$SCRIPT_DIR/libk2_ne_helper_$ARCH.a" "$SCRIPT_DIR/libk2_ne_helper.a"
    echo "[build.sh] Static library: $SCRIPT_DIR/libk2_ne_helper.a"
fi
