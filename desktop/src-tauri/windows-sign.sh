#!/bin/bash
set -e

echo ""
echo "🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐"
echo "🔐"
echo "🔐   SIGNING: $(basename "$1")"
echo "🔐"
echo "🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐🔐"
echo ""
echo "Input file: $1"
echo "Current directory: $(pwd)"
echo "Script path: ${BASH_SOURCE[0]}"
echo ""

# Check if signing should be skipped entirely
if [ "${SKIP_WINDOWS_SIGNING:-false}" = "true" ]; then
    echo "SKIP_WINDOWS_SIGNING=true, skipping signing for: $1"
    exit 0
fi

if [ -z "$1" ]; then
    echo "ERROR: No file path provided" >&2
    exit 1
fi

FILE_PATH="$1"
FILE_NAME=$(basename "$FILE_PATH")
echo "Processing file: $FILE_PATH"

# NSIS generates temp uninstaller without .exe extension (e.g., makensisXXXXXX)
# Add .exe suffix for signing server if missing
SIGN_FILE_NAME="$FILE_NAME"
if [[ ! "$FILE_NAME" =~ \.(exe|dll)$ ]]; then
    SIGN_FILE_NAME="${FILE_NAME}.exe"
    echo "Note: Adding .exe extension for signing server (temp NSIS uninstaller)"
fi

# Skip wintun.dll - it's already signed by WinTun official (driver-level signature)
# Case-insensitive comparison to handle Wintun.dll, WINTUN.DLL, etc.
FILE_NAME_LOWER=$(echo "$FILE_NAME" | tr '[:upper:]' '[:lower:]')
if [ "$FILE_NAME_LOWER" = "wintun.dll" ]; then
    echo "Skipping $FILE_NAME - already signed by official WinTun (driver-level signature, cannot be re-signed)"
    exit 0
fi

if [ ! -f "$FILE_PATH" ]; then
    echo "ERROR: File not found: $FILE_PATH" >&2
    exit 1
fi

SIGNED_FILE="${FILE_PATH}.signed"

echo "File name: $FILE_NAME"
echo "Signed file path: $SIGNED_FILE"
echo ""

# Auto-detect Parallels Windows VM IP or use environment variable
detect_sign_server_url() {
    # 1. Check environment variable first
    if [ -n "${KAITU_SIGN_SERVER_URL:-}" ]; then
        echo "$KAITU_SIGN_SERVER_URL"
        return 0
    fi

    # 2. Try to detect Parallels Windows VM
    if command -v prlctl >/dev/null 2>&1; then
        # Auto-detect running Windows VM
        local vm_name=$(prlctl list -a -o name,status 2>/dev/null | grep -i windows | grep running | awk '{print $1, $2}' | sed 's/ *running$//')

        if [ -z "$vm_name" ]; then
            return 1
        fi

        # Get VM IP address (skip header line, get first IPv4 address)
        local vm_ip=$(prlctl list -f -o ip "$vm_name" 2>/dev/null | tail -1 | awk '{print $1}' | grep -E '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$')

        if [ -n "$vm_ip" ] && [ "$vm_ip" != "-" ]; then
            echo "http://$vm_ip:8777"
            return 0
        fi
    fi

    # 3. No auto-detection possible - must use environment variable
    return 1
}

# Detect or use configured signing server URL
SIGN_SERVER_URL=$(detect_sign_server_url)

if [ -z "$SIGN_SERVER_URL" ]; then
    echo "ERROR: Unable to auto-detect signing server" >&2
    echo "" >&2
    echo "Please ensure one of the following:" >&2
    echo "  1. A Parallels Windows VM is running (will be auto-detected)" >&2
    echo "     prlctl list -a  # Check VM status" >&2
    echo "" >&2
    echo "  2. Or set the signing server URL manually:" >&2
    echo "     export KAITU_SIGN_SERVER_URL=\"http://YOUR_VM_IP:8777\"" >&2
    echo "" >&2
    exit 1
fi

echo "Signing server URL: $SIGN_SERVER_URL"
echo ""

echo "Creating signing task..."
echo "Using filename for signing: $SIGN_FILE_NAME"
TEMP_RESPONSE=$(mktemp)
HTTP_CODE=$(curl -sSf -w "%{http_code}" -o "$TEMP_RESPONSE" \
    -X POST \
    -H "Content-Type: application/json" \
    -d "{\"filename\":\"$SIGN_FILE_NAME\",\"cert_name\":\"Wordgate LLC\"}" \
    "$SIGN_SERVER_URL/tasks" 2>&1 || echo "000")

echo "HTTP response code: $HTTP_CODE"

if [ "$HTTP_CODE" != "200" ]; then
    echo "ERROR: Failed to create signing task (HTTP $HTTP_CODE)" >&2
    cat "$TEMP_RESPONSE" 2>/dev/null || true
    rm -f "$TEMP_RESPONSE"
    exit 1
fi

TASK_ID=$(grep -o '"id":"[^"]*"' "$TEMP_RESPONSE" | head -1 | cut -d'"' -f4)
rm -f "$TEMP_RESPONSE"

if [ -z "$TASK_ID" ]; then
    echo "ERROR: Failed to extract task ID" >&2
    exit 1
fi

echo "Task ID: $TASK_ID"
echo ""

echo "Uploading file to signing server..."
TEMP_RESPONSE=$(mktemp)
HTTP_CODE=$(curl -sSf -w "%{http_code}" -o "$TEMP_RESPONSE" \
    -X POST \
    -F "file=@$FILE_PATH" \
    "$SIGN_SERVER_URL/tasks/$TASK_ID/upload" 2>&1 || echo "000")

echo "Upload HTTP response code: $HTTP_CODE"

if [ "$HTTP_CODE" != "200" ]; then
    echo "ERROR: Upload failed (HTTP $HTTP_CODE)" >&2
    cat "$TEMP_RESPONSE" 2>/dev/null || true
    rm -f "$TEMP_RESPONSE"
    exit 1
fi

rm -f "$TEMP_RESPONSE"
echo "File uploaded successfully"
echo ""

echo "Waiting for signing to complete..."
MAX_RETRIES=30
RETRY_COUNT=0

while [ $RETRY_COUNT -lt $MAX_RETRIES ]; do
    sleep 2

    TEMP_RESPONSE=$(mktemp)
    HTTP_CODE=$(curl -sSf -w "%{http_code}" -o "$TEMP_RESPONSE" \
        "$SIGN_SERVER_URL/tasks/$TASK_ID" 2>&1 || echo "000")

    if [ "$HTTP_CODE" != "200" ]; then
        echo "ERROR: Failed to check status (HTTP $HTTP_CODE)" >&2
        rm -f "$TEMP_RESPONSE"
        exit 1
    fi

    STATUS=$(grep -o '"status":"[^"]*"' "$TEMP_RESPONSE" | head -1 | cut -d'"' -f4)
    rm -f "$TEMP_RESPONSE"

    echo "Status check $((RETRY_COUNT + 1))/$MAX_RETRIES: $STATUS"

    if [ "$STATUS" = "completed" ]; then
        echo "Signing completed successfully"
        break
    elif [ "$STATUS" = "failed" ]; then
        echo "ERROR: Signing failed on server" >&2
        TEMP_ERROR=$(mktemp)
        curl -sSf "$SIGN_SERVER_URL/tasks/$TASK_ID" -o "$TEMP_ERROR" 2>&1 || true
        ERROR_MSG=$(grep -o '"error":"[^"]*"' "$TEMP_ERROR" | head -1 | cut -d'"' -f4 || echo "Unknown error")
        rm -f "$TEMP_ERROR"
        echo "Server error: $ERROR_MSG" >&2
        exit 1
    fi

    RETRY_COUNT=$((RETRY_COUNT + 1))
done

if [ $RETRY_COUNT -ge $MAX_RETRIES ]; then
    echo "ERROR: Timeout waiting for signing" >&2
    exit 1
fi

echo ""
echo "Downloading signed file..."
DOWNLOAD_CODE=$(curl -sSf -w "%{http_code}" -o "$SIGNED_FILE" \
    "$SIGN_SERVER_URL/tasks/$TASK_ID/download" 2>&1 || echo "000")

echo "Download HTTP response code: $DOWNLOAD_CODE"

if [ "$DOWNLOAD_CODE" != "200" ]; then
    echo "ERROR: Download failed (HTTP $DOWNLOAD_CODE)" >&2
    rm -f "$SIGNED_FILE"
    exit 1
fi

if [ ! -f "$SIGNED_FILE" ]; then
    echo "ERROR: Signed file not found after download" >&2
    exit 1
fi

echo "Signed file downloaded successfully"
echo ""

echo "Replacing original file with signed version..."
mv "$SIGNED_FILE" "$FILE_PATH"

if [ $? -ne 0 ]; then
    echo "ERROR: Failed to replace original file" >&2
    exit 1
fi

echo "File replaced successfully"
echo ""
echo "✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅"
echo "✅"
echo "✅   SIGNED: $FILE_NAME"
echo "✅"
echo "✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅✅"
echo ""

exit 0
