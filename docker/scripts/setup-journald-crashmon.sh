#!/bin/bash
# Configure journald persistence + k2v5 crash monitor service
# Safe to run on any active node — idempotent, no service restart needed.
#
# What it does:
#   1. Configure journald for persistent storage (500M/30d)
#   2. Install k2v5-crash-monitor.sh as a systemd service
#
# Prereq: k2v5-crash-monitor.sh must already be at /apps/kaitu-slave/

set -euo pipefail

echo "=== Journald + Crash Monitor Setup ==="
echo "Host: $(hostname)"

# --- [1] Journald persistent storage ---

echo ""
echo "[1/2] Configuring journald persistence..."

mkdir -p /etc/systemd/journald.conf.d

if [ -f /etc/systemd/journald.conf.d/k2v5.conf ]; then
    echo "  OK: journald config already exists."
else
    cat > /etc/systemd/journald.conf.d/k2v5.conf <<'JEOF'
[Journal]
Storage=persistent
SystemMaxUse=500M
MaxRetentionSec=30day
JEOF
    echo "  Created /etc/systemd/journald.conf.d/k2v5.conf"
fi

mkdir -p /var/log/journal
systemd-tmpfiles --create --prefix /var/log/journal 2>/dev/null || true
systemctl restart systemd-journald 2>/dev/null || true

echo "  OK: journald persistent (500M/30d)."

# --- [2] Crash monitor systemd service ---

echo ""
echo "[2/2] Installing crash monitor service..."

SCRIPT="/apps/kaitu-slave/k2v5-crash-monitor.sh"
if [ ! -f "$SCRIPT" ]; then
    echo "  ERROR: $SCRIPT not found. Deploy it first."
    exit 1
fi

chmod +x "$SCRIPT"

cat > /etc/systemd/system/k2v5-crash-monitor.service <<'SEOF'
[Unit]
Description=k2v5 crash monitor
After=docker.service
Requires=docker.service

[Service]
Type=simple
ExecStart=/apps/kaitu-slave/k2v5-crash-monitor.sh
Restart=always
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=k2v5-crash-monitor

[Install]
WantedBy=multi-user.target
SEOF

systemctl daemon-reload
systemctl enable k2v5-crash-monitor.service 2>/dev/null || true
systemctl restart k2v5-crash-monitor.service 2>/dev/null || true

if systemctl is-active --quiet k2v5-crash-monitor.service 2>/dev/null; then
    echo "  OK: crash monitor active."
else
    echo "  WARN: crash monitor installed but not active (Docker may not be running)."
fi

# --- Summary ---

echo ""
echo "=== Result ==="
echo "Journald:     $(test -f /etc/systemd/journald.conf.d/k2v5.conf && echo 'persistent (500M/30d)' || echo 'not configured')"
echo "CrashMon:     $(systemctl is-active k2v5-crash-monitor 2>/dev/null || echo 'not installed')"
echo "=== Done ==="
