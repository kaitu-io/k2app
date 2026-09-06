#!/usr/bin/env bash
# scripts/test-sign-description.sh — the Authenticode description must follow K2_BRAND.
set -euo pipefail
S="$(cd "$(dirname "$0")" && pwd)/ci/sign-description.sh"
fails=0
check() { got=$(env -u K2_BRAND ${2:+K2_BRAND=$2} bash "$S"); if [ "$got" = "$1" ]; then echo "  PASS  brand=${2:-unset} → $got"; else echo "  FAIL  brand=${2:-unset} → '$got' (want '$1')"; fails=$((fails+1)); fi; }
check "Kaitu Desktop" ""
check "Kaitu Desktop" kaitu
check "Overleap Desktop" overleap
# The PowerShell signer must carry the same fork (string check — no Windows here).
grep -q "Overleap" "$(dirname "$S")/windows/sign-binary.ps1" && echo "  PASS  ps1 has overleap arm" || { echo "  FAIL  ps1 lacks overleap arm"; fails=$((fails+1)); }
exit $(( fails > 0 ))
