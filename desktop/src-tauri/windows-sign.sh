#!/bin/bash
set -e

# Tauri signCommand wrapper — called for every .exe/.dll during Windows bundle.
# macOS: osslsigncode + SimplySign PKCS#11
# Windows: signtool.exe from Windows SDK
#
# Every invocation is journalled to $K2_SIGN_LOG (unset = no journal, so local
# builds are unaffected). Tauri swallows this command's output entirely and
# reports only `failed to bundle project 'failed to run bash'` — a message that
# cannot distinguish two very different failures:
#
#   (a) the signer ran and the signing itself failed, or
#   (b) the signer process was never started at all.
#
# The journal settles that. The ENTER line is written before anything else in
# this script can fail, so for the file Tauri was signing:
#   ENTER present, EXIT rc!=0  -> (a), and the captured output is the real error
#   ENTER present, no EXIT     -> the signer was killed mid-run (OOM, timeout)
#   no ENTER at all            -> (b), the failure is upstream of this script
#                                 (spawn refused, wrong cwd, bash not found) and
#                                 no amount of signer-side logging will show it
# The resource counters exist for that last case: a spawn refused with EAGAIN or
# EMFILE is invisible here, but the trend across the invocations that DID run
# points at it.

SIGN_LOG="${K2_SIGN_LOG:-}"

journal() {
    [ -n "$SIGN_LOG" ] || return 0
    printf '%s\n' "$*" >> "$SIGN_LOG" 2>/dev/null || true
}

if [ -n "$SIGN_LOG" ]; then
    journal "=== ENTER $(date -u +%Y-%m-%dT%H:%M:%SZ) pid=$$ ppid=$PPID"
    journal "    arg=${1:-<none>}"
    journal "    arg_readable=$([ -r "${1:-/nonexistent}" ] && echo yes || echo NO)"
    journal "    cwd=$(pwd)"
    journal "    bash=${BASH_VERSION:-?} at $(command -v bash 2>/dev/null || echo '?')"
    journal "    procs=$(ps ax 2>/dev/null | wc -l | tr -d ' ') openfiles=$(sysctl -n kern.num_files 2>/dev/null || echo n/a)/$(sysctl -n kern.maxfiles 2>/dev/null || echo n/a)"
fi

if [ "${SKIP_WINDOWS_SIGNING:-false}" = "true" ]; then
    echo "SKIP_WINDOWS_SIGNING=true, skipping: $(basename "$1")"
    journal "=== EXIT rc=0 (skipped via SKIP_WINDOWS_SIGNING)"
    exit 0
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# Not `exec`: the signer's exit code and output have to survive so they can be
# journalled. stdout and stderr are merged because Tauri prints this command's
# output as one stream anyway, and keeping them split would let the half that
# matters (stderr) be the half that gets dropped.
run_signer() {
    case "$(uname -s)" in
        Darwin)
            bash "$REPO_ROOT/scripts/ci/macos/windows-sign.sh" "$1" 2>&1
            ;;
        MINGW*|MSYS*|CYGWIN*|Windows_NT)
            powershell -NoProfile -File "$REPO_ROOT/scripts/ci/windows/sign-binary.ps1" "$1" 2>&1
            ;;
        *)
            echo "ERROR: Unsupported OS for Windows signing: $(uname -s)" >&2
            return 1
            ;;
    esac
}

set +e
SIGN_OUTPUT="$(run_signer "$1")"
SIGN_RC=$?
set -e

printf '%s\n' "$SIGN_OUTPUT"
journal "$SIGN_OUTPUT"
journal "=== EXIT rc=$SIGN_RC $(basename "${1:-<none>}")"

exit $SIGN_RC
