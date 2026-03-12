#!/bin/bash
set -e

# Tauri signCommand wrapper — called for every .exe/.dll during Windows bundle.
# macOS: osslsigncode + SimplySign PKCS#11
# Windows: signtool.exe from Windows SDK

if [ "${SKIP_WINDOWS_SIGNING:-false}" = "true" ]; then
    echo "SKIP_WINDOWS_SIGNING=true, skipping: $(basename "$1")"
    exit 0
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

case "$(uname -s)" in
    Darwin)
        exec bash "$REPO_ROOT/scripts/ci/macos/windows-sign.sh" "$1"
        ;;
    MINGW*|MSYS*|CYGWIN*|Windows_NT)
        exec powershell -NoProfile -File "$REPO_ROOT/scripts/ci/windows/sign-binary.ps1" "$1"
        ;;
    *)
        echo "ERROR: Unsupported OS for Windows signing: $(uname -s)" >&2
        exit 1
        ;;
esac
