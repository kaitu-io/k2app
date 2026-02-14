#!/usr/bin/env bash
set -euo pipefail

REPO="kaitu-io/k2app"

# All secrets to push — variable name maps directly to GitHub secret name
SECRETS=(
  APPLE_CERTIFICATE
  APPLE_CERTIFICATE_PASSWORD
  APPLE_SIGNING_IDENTITY
  APPLE_ID
  APPLE_PASSWORD
  APPLE_TEAM_ID
  APPLE_INSTALLER_IDENTITY
  TAURI_SIGNING_PRIVATE_KEY
  TAURI_SIGNING_PRIVATE_KEY_PASSWORD
  AWS_ACCESS_KEY_ID
  AWS_SECRET_ACCESS_KEY
  SIMPLISIGN_TOTP_URI
  SIMPLISIGN_USERNAME
  SLACK_WEBHOOK_ALERT
  SLACK_WEBHOOK_RELEASE
  K2_DEPLOY_KEY
)

echo "Pushing secrets to ${REPO}..."
echo ""

FAILED=0
SKIPPED=0
SET_COUNT=0

for SECRET in "${SECRETS[@]}"; do
  VALUE="${!SECRET:-}"
  if [ -z "$VALUE" ]; then
    echo "  SKIP  ${SECRET} (empty)"
    SKIPPED=$((SKIPPED + 1))
    continue
  fi
  if echo "$VALUE" | gh -R "$REPO" secret set "$SECRET" 2>/dev/null; then
    echo "  SET   ${SECRET}"
    SET_COUNT=$((SET_COUNT + 1))
  else
    echo "  FAIL  ${SECRET}"
    FAILED=$((FAILED + 1))
  fi
done

echo ""
echo "Done: ${SET_COUNT} set, ${SKIPPED} skipped, ${FAILED} failed"

if [ "$FAILED" -gt 0 ]; then
  exit 1
fi
