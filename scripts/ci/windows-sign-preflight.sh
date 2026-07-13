#!/bin/bash
set -e

# Windows Authenticode signing preflight.
#
# Proves the code-signing path can actually perform a private-key operation
# BEFORE the ~15-minute Tauri bundle runs. The SimplySign login gate only
# checks that the PKCS#11 slot is exposed, and that slot stays cached locally
# after the cloud session drops — so the real signing op fails only deep
# inside `tauri build`, where Tauri swallows the signer's stderr and emits an
# opaque "failed to run bash". Signing a throwaway copy of the k2 sidecar here
# surfaces the real osslsigncode error (e.g. CKR_ATTRIBUTE_TYPE_INVALID) and
# aborts the build immediately with an actionable message.
#
# Honors SKIP_WINDOWS_SIGNING=true (the wrapper skips, so this passes too).

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SIGNER="$REPO_ROOT/desktop/src-tauri/windows-sign.sh"
K2_EXE="$REPO_ROOT/desktop/src-tauri/binaries/k2-x86_64-pc-windows-msvc.exe"

echo "=== Windows signing preflight ==="

if [ ! -f "$SIGNER" ]; then
    echo "ERROR: signer wrapper not found: $SIGNER" >&2
    exit 1
fi

if [ ! -f "$K2_EXE" ]; then
    echo "ERROR: k2 Windows binary not found: $K2_EXE" >&2
    echo "Build it first (make build-k2-windows)." >&2
    exit 1
fi

# Sign a disposable copy so we never perturb the binary Tauri will sign.
PREFLIGHT="$(mktemp -t k2-sign-preflight)"
cleanup() { rm -f "$PREFLIGHT"; }
trap cleanup EXIT
cp "$K2_EXE" "$PREFLIGHT"

if bash "$SIGNER" "$PREFLIGHT"; then
    echo "Signing preflight OK — the cloud session can sign."
    exit 0
fi

echo "" >&2
echo "ERROR: Windows signing preflight FAILED." >&2
echo "The SimplySign cloud session cannot perform a private-key signing" >&2
echo "operation (see the osslsigncode error above). The PKCS#11 slot may" >&2
echo "still be cached locally, but the cloud session is not live." >&2
echo "" >&2
echo "Fix, then re-run the build:" >&2
echo "  make simplisign-login        # automated (needs SIMPLISIGN_TOTP_URI)" >&2
echo "  or open 'SimplySign Desktop' -> 'Connect with cloud' + approve on phone" >&2
exit 1
