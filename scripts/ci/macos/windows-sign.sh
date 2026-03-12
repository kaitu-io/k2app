#!/bin/bash
set -e

# Windows Authenticode signing on macOS via osslsigncode + SimplySign PKCS#11
#
# Called by Tauri as signCommand during Windows bundle phase.
# Requires: osslsigncode (brew install osslsigncode), libp11 (brew install libp11),
#           SimplySign Desktop logged in (use simplisign-login.sh first)
#
# Usage: ./windows-sign.sh <file-to-sign>

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo ""
echo "=== SIGNING: $(basename "$1") ==="
echo ""

# --- Config ---
PKCS11_ENGINE="/opt/homebrew/Cellar/openssl@3/3.6.1/lib/engines-3/pkcs11.dylib"
PKCS11_MODULE="/usr/local/lib/SimplySignPKCS/SimplySignPKCS-MS-1.1.24.dylib"
PKCS11_CERT="pkcs11:token=wordgate;object=334AB051AA095E46AF497253EB398C98;type=cert"
PKCS11_KEY="pkcs11:token=wordgate"
TIMESTAMP_URL="http://time.certum.pl"

# --- Validate ---
if [ "${SKIP_WINDOWS_SIGNING:-false}" = "true" ]; then
    echo "SKIP_WINDOWS_SIGNING=true, skipping."
    exit 0
fi

if [ -z "$1" ]; then
    echo "ERROR: No file path provided" >&2
    exit 1
fi

FILE_PATH="$1"
FILE_NAME=$(basename "$FILE_PATH")

if [ ! -f "$FILE_PATH" ]; then
    echo "ERROR: File not found: $FILE_PATH" >&2
    exit 1
fi

# Skip wintun.dll (already signed by WinTun official)
FILE_LOWER=$(echo "$FILE_NAME" | tr '[:upper:]' '[:lower:]')
if [ "$FILE_LOWER" = "wintun.dll" ]; then
    echo "Skipping $FILE_NAME — already signed by official WinTun"
    exit 0
fi

# --- Check prerequisites ---
if ! command -v osslsigncode >/dev/null 2>&1; then
    echo "ERROR: osslsigncode not found. Install: brew install osslsigncode" >&2
    exit 1
fi

if [ ! -f "$PKCS11_ENGINE" ]; then
    echo "ERROR: PKCS#11 engine not found at $PKCS11_ENGINE" >&2
    echo "Install: brew install libp11" >&2
    exit 1
fi

if [ ! -f "$PKCS11_MODULE" ]; then
    echo "ERROR: SimplySign PKCS#11 module not found" >&2
    exit 1
fi

# Verify PKCS#11 token is available — auto-login if not
if ! pkcs11-tool --module "$PKCS11_MODULE" --list-slots 2>&1 | grep -q "token label"; then
    echo "PKCS#11 token not available, attempting auto-login..."
    LOGIN_SCRIPT="$SCRIPT_DIR/simplisign-login.sh"
    if [ -f "$LOGIN_SCRIPT" ]; then
        bash "$LOGIN_SCRIPT"
    else
        echo "ERROR: PKCS#11 token not available and login script not found at $LOGIN_SCRIPT" >&2
        exit 1
    fi
    # Re-check after login
    if ! pkcs11-tool --module "$PKCS11_MODULE" --list-slots 2>&1 | grep -q "token label"; then
        echo "ERROR: PKCS#11 token still not available after login attempt." >&2
        exit 1
    fi
fi

# --- Sign ---
TEMP_SIGNED="${FILE_PATH}.signed"

echo "Signing: $FILE_PATH"
rm -f "$TEMP_SIGNED"
osslsigncode sign \
    -pkcs11engine "$PKCS11_ENGINE" \
    -pkcs11module "$PKCS11_MODULE" \
    -pkcs11cert "$PKCS11_CERT" \
    -key "$PKCS11_KEY" \
    -h sha256 \
    -n "Kaitu Desktop" \
    -ts "$TIMESTAMP_URL" \
    -in "$FILE_PATH" \
    -out "$TEMP_SIGNED"

# Replace original with signed version
mv "$TEMP_SIGNED" "$FILE_PATH"

echo "Signed: $FILE_NAME"
echo ""
