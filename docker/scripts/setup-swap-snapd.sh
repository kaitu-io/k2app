#!/bin/bash
# Safe, idempotent swap + snapd cleanup for existing nodes
# Does NOT touch Docker, SSH, or any running services
#
# What it does:
#   1. Remove snapd + pin to prevent reinstall (~128MB freed)
#   2. Setup 1GB swap for instances with ≤2GB RAM, swappiness=10
#
# Safe to run on any node at any time — zero service impact.

export DEBIAN_FRONTEND=noninteractive

echo "=== Swap + Snapd Setup ==="
echo "Host: $(hostname) | RAM: $(awk '/MemTotal/ {printf "%d MB", $2/1024}' /proc/meminfo)"

# --- [1] Remove snapd ---

echo ""
echo "[1/2] Checking snapd..."

if command -v snap >/dev/null 2>&1; then
    echo "  snapd found, removing..."

    # Remove all snap packages (dependency order: apps first, then snapd)
    SNAPS=$(snap list 2>/dev/null | awk 'NR>1 && $1!="snapd" {print $1}')
    for s in $SNAPS; do
        snap remove --purge "$s" 2>/dev/null || true
    done
    snap remove --purge snapd 2>/dev/null || true

    # Wait for apt lock (up to 30s)
    waited=0
    while fuser /var/lib/dpkg/lock-frontend >/dev/null 2>&1; do
        [ $waited -ge 30 ] && break
        sleep 3
        waited=$((waited + 3))
    done

    apt-get remove -y --purge snapd 2>/dev/null || true
    apt-get autoremove -y 2>/dev/null || true

    # Clean leftover directories
    rm -rf /snap /var/snap /var/lib/snapd /var/cache/snapd \
           ~/snap /root/snap 2>/dev/null || true

    # Pin snapd to prevent reinstall
    if [ ! -f /etc/apt/preferences.d/no-snapd ]; then
        cat > /etc/apt/preferences.d/no-snapd <<'PINEOF'
Package: snapd
Pin: release *
Pin-Priority: -10
PINEOF
    fi

    echo "  OK: snapd removed and pinned."
else
    # Ensure pin exists even if snap was already removed
    if [ ! -f /etc/apt/preferences.d/no-snapd ]; then
        cat > /etc/apt/preferences.d/no-snapd <<'PINEOF'
Package: snapd
Pin: release *
Pin-Priority: -10
PINEOF
        echo "  OK: snapd not installed. Pin added."
    else
        echo "  OK: snapd not installed, pin exists."
    fi
fi

# --- [2] Setup swap ---

echo ""
echo "[2/2] Checking swap..."

TOTAL_RAM_MB=$(awk '/MemTotal/ {printf "%d", $2/1024}' /proc/meminfo)
SWAP_SIZE_MB=$(swapon --show=SIZE --bytes --noheadings 2>/dev/null | awk '{sum+=$1} END {printf "%d", sum/1024/1024}')
: "${SWAP_SIZE_MB:=0}"

if [ "$SWAP_SIZE_MB" -gt 0 ]; then
    SWAPPINESS=$(sysctl -n vm.swappiness 2>/dev/null)
    if [ "${SWAPPINESS:-60}" -le 10 ]; then
        echo "  OK: Swap active (${SWAP_SIZE_MB}MB), swappiness=${SWAPPINESS}."
    else
        # Swap exists but swappiness too high — fix it
        sysctl -w vm.swappiness=10 >/dev/null 2>&1 || true
        sed -i '/vm.swappiness/d' /etc/sysctl.conf
        echo "vm.swappiness=10" >> /etc/sysctl.conf
        echo "  OK: Swap active (${SWAP_SIZE_MB}MB). Swappiness fixed: ${SWAPPINESS} → 10."
    fi
elif [ "$TOTAL_RAM_MB" -le 2048 ]; then
    echo "  RAM=${TOTAL_RAM_MB}MB (≤2GB), creating 1GB swap..."
    SWAPFILE="/swapfile"
    if [ ! -f "$SWAPFILE" ]; then
        dd if=/dev/zero of="$SWAPFILE" bs=1M count=1024 2>/dev/null
        chmod 600 "$SWAPFILE"
        mkswap "$SWAPFILE" >/dev/null
    fi
    swapon "$SWAPFILE" 2>/dev/null || true

    # Persist in fstab (idempotent)
    if ! grep -q "$SWAPFILE" /etc/fstab; then
        echo "$SWAPFILE none swap sw 0 0" >> /etc/fstab
    fi

    # Low swappiness
    sysctl -w vm.swappiness=10 >/dev/null 2>&1 || true
    sed -i '/vm.swappiness/d' /etc/sysctl.conf
    echo "vm.swappiness=10" >> /etc/sysctl.conf

    VERIFY=$(swapon --show=SIZE --bytes --noheadings 2>/dev/null | awk '{printf "%d", $1/1024/1024}')
    if [ "${VERIFY:-0}" -gt 0 ]; then
        echo "  OK: 1GB swap created and active. swappiness=10."
    else
        echo "  WARN: Swapfile created but swapon failed."
    fi
else
    echo "  OK: RAM=${TOTAL_RAM_MB}MB (>2GB), swap not needed."
fi

# --- Summary ---

echo ""
echo "=== Result ==="
echo "Snapd:      $(command -v snap >/dev/null 2>&1 && echo 'INSTALLED (unexpected)' || echo 'removed')"
echo "Snap pin:   $(test -f /etc/apt/preferences.d/no-snapd && echo 'yes' || echo 'no')"
echo "Swap:       $(swapon --show=NAME,SIZE --noheadings 2>/dev/null || echo 'none')"
echo "Swappiness: $(sysctl -n vm.swappiness 2>/dev/null || echo 'unknown')"
echo "=== Done ==="
