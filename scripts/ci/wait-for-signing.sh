#!/usr/bin/env bash
#
# wait-for-signing.sh — Poll S3 for signed artifacts from kaitu-signer.
#
# Usage:
#   ./wait-for-signing.sh <run-id> <s3-bucket> [timeout-seconds]
#
# Polls s3://<bucket>/kaitu/signing/completed/<run-id>/status.json
# every 10 seconds until it appears (success) or timeout (failure).
#
# Exit codes:
#   0 — signed artifacts found, status.json reports success
#   1 — timeout or signing reported failure

set -euo pipefail

RUN_ID="${1:?Usage: wait-for-signing.sh <run-id> <s3-bucket> [timeout]}"
S3_BUCKET="${2:?Usage: wait-for-signing.sh <run-id> <s3-bucket> [timeout]}"
TIMEOUT="${3:-600}"  # default 10 minutes

POLL_INTERVAL=10
STATUS_KEY="kaitu/signing/completed/${RUN_ID}/status.json"
S3_URI="s3://${S3_BUCKET}/${STATUS_KEY}"
STATUS_FILE="${RUNNER_TEMP:-/tmp}/signing-status-${RUN_ID}.json"

echo "Waiting for signed artifacts..."
echo "  Polling: ${S3_URI}"
echo "  Timeout: ${TIMEOUT}s"

elapsed=0
while [ "$elapsed" -lt "$TIMEOUT" ]; do
    # Try to download status.json
    if aws s3 cp "${S3_URI}" "${STATUS_FILE}" --quiet 2>/dev/null; then
        echo "Found status.json after ${elapsed}s"

        # Check if signing succeeded (use jq — available on all GitHub runners)
        success=$(jq -r '.success' "${STATUS_FILE}" 2>/dev/null || echo "false")

        if [ "$success" = "true" ]; then
            echo "Signing completed successfully!"
            jq . "${STATUS_FILE}"
            exit 0
        else
            echo "ERROR: Signing reported failure:"
            jq . "${STATUS_FILE}"
            exit 1
        fi
    fi

    sleep "$POLL_INTERVAL"
    elapsed=$((elapsed + POLL_INTERVAL))
    echo "  ... waiting (${elapsed}s / ${TIMEOUT}s)"
done

echo "ERROR: Timed out after ${TIMEOUT}s waiting for signing to complete"
exit 1
