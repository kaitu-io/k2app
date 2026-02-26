#!/bin/bash
# Provision a fresh Ubuntu node for Kaitu VPN service
#
# Combines: Docker CE install + IPv6 + BBR + SSH hardening + auto-update cron
# Target: Ubuntu 20.04 / 22.04 / 24.04
#
# What it does (12 steps):
#   1. Clean old Docker versions
#   2. Ensure iptables is installed
#   3. Configure official Docker apt source
#   4. Install Docker CE + plugins + docker group + disable unattended-upgrades
#   5. Create docker-compose compatibility wrapper
#   6. Enable IPv6 kernel params (sysctl)
#   7. Enable BBR congestion control (sysctl)
#   8. Configure Docker daemon (IPv6 + log rotation)
#   9. Install UFW-Docker security patch (if UFW active)
#  10. Harden SSH: switch to port 1022 only (with rollback on failure)
#  11. Deploy auto-update cron (daily 04:00 Beijing = 20:00 UTC)
#  12. Verify everything works
#
# Safety:
#   - NO set -e: each step handles its own errors
#   - Fatal steps (Docker install) abort. Best-effort steps warn and continue.
#   - SSH hardening has full rollback — never leaves SSH in a broken state.
#   - Idempotent: safe to re-run on already-provisioned nodes.
#
# Usage:
#   sudo bash provision-node.sh

export DEBIAN_FRONTEND=noninteractive

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

ERRORS=0
WARNINGS=0

ok()   { echo -e "${GREEN}>>> $1${NC}"; }
warn() { echo -e "${YELLOW}>>> $1${NC}"; WARNINGS=$((WARNINGS + 1)); }
fail() { echo -e "${RED}>>> $1${NC}"; ERRORS=$((ERRORS + 1)); }
die()  { echo -e "${RED}FATAL: $1${NC}"; exit 1; }

echo -e "${BLUE}==================================================${NC}"
echo -e "${BLUE}   Kaitu Node Provisioning (Ubuntu 20/22/24)      ${NC}"
echo -e "${BLUE}==================================================${NC}"

# --- Prerequisites ---

if [[ $EUID -ne 0 ]]; then
    die "Must run as root (sudo bash provision-node.sh)"
fi

if ! grep -qi "Ubuntu" /etc/os-release; then
    die "Ubuntu only."
fi

UBUNTU_VERSION=$(. /etc/os-release && echo "$VERSION_ID")
UBUNTU_CODENAME=$(. /etc/os-release && echo "$VERSION_CODENAME")
echo -e "Detected: Ubuntu ${UBUNTU_VERSION} (${UBUNTU_CODENAME})"

# --- Helpers ---

# Wait for apt/dpkg lock (up to 60s)
apt_wait() {
    local waited=0
    while fuser /var/lib/dpkg/lock-frontend >/dev/null 2>&1 || \
          fuser /var/lib/apt/lists/lock >/dev/null 2>&1; do
        if [ $waited -ge 60 ]; then
            fail "apt lock held for >60s, giving up"
            return 1
        fi
        echo "  Waiting for apt lock..."
        sleep 5
        waited=$((waited + 5))
    done
    return 0
}

# Restart SSH service (handles both socket and service modes)
# Returns 0 on success, 1 on failure
restart_ssh() {
    if systemctl is-enabled ssh.socket &>/dev/null; then
        # Ubuntu 24.04+: socket-activated
        # daemon-reload re-runs sshd-socket-generator which reads Port from sshd_config
        systemctl daemon-reload
        systemctl restart ssh.socket 2>&1 && return 0
        # Fallback: try stopping socket and starting ssh service directly
        systemctl stop ssh.socket 2>/dev/null
        systemctl start ssh.service 2>&1 && return 0
        return 1
    else
        # Ubuntu 20.04/22.04: direct service
        systemctl restart ssh 2>&1 && return 0
        systemctl restart sshd 2>&1 && return 0
        return 1
    fi
}

# --- Idempotency check ---

ALREADY_DONE=true
docker --version &>/dev/null || ALREADY_DONE=false
sysctl -n net.ipv4.tcp_congestion_control 2>/dev/null | grep -q bbr || ALREADY_DONE=false
ss -tlnp 2>/dev/null | grep -q ":1022 " || ALREADY_DONE=false

if [ "$ALREADY_DONE" = true ]; then
    echo -e "${GREEN}Node appears already provisioned (Docker + BBR + SSH 1022).${NC}"
    echo -e "Re-running to ensure all settings are current..."
fi

# ===================================================================
# [1/12] Clean old Docker versions
# ===================================================================

echo -e "${YELLOW}[1/12] Cleaning old Docker versions...${NC}"
systemctl stop docker >/dev/null 2>&1 || true
systemctl stop docker.socket >/dev/null 2>&1 || true

apt-get remove -y docker docker-engine docker.io containerd runc \
    docker-compose docker-compose-v2 podman-docker >/dev/null 2>&1 || true
apt-get autoremove -y >/dev/null 2>&1 || true

rm -f /usr/local/bin/docker-compose /usr/bin/docker-compose

ok "Old versions cleaned."

# ===================================================================
# [2/12] Ensure iptables is installed
# ===================================================================

echo -e "${YELLOW}[2/12] Ensuring iptables is installed...${NC}"
apt_wait || die "Cannot acquire apt lock"
apt-get update -qq 2>&1 || warn "apt-get update had warnings"
apt-get install -y iptables >/dev/null 2>&1 || warn "iptables install issue (may already be present)"

ok "iptables ready (using OS default backend)."

# ===================================================================
# [3/12] Configure official Docker apt source
# ===================================================================

echo -e "${YELLOW}[3/12] Configuring official Docker apt source...${NC}"
apt-get install -y ca-certificates curl gnupg >/dev/null 2>&1

install -m 0755 -d /etc/apt/keyrings
if ! curl -fsSL https://download.docker.com/linux/ubuntu/gpg | gpg --dearmor --yes -o /etc/apt/keyrings/docker.gpg 2>/dev/null; then
    die "Failed to download Docker GPG key (network issue?)"
fi
chmod a+r /etc/apt/keyrings/docker.gpg

echo \
  "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu \
  ${UBUNTU_CODENAME} stable" | \
  tee /etc/apt/sources.list.d/docker.list > /dev/null

apt_wait || die "Cannot acquire apt lock"
apt-get update -qq 2>&1 || warn "apt-get update had warnings"
ok "Apt source configured."

# ===================================================================
# [4/12] Install Docker CE + plugins
# ===================================================================

echo -e "${YELLOW}[4/12] Installing Docker CE + plugins...${NC}"
apt_wait || die "Cannot acquire apt lock"
if ! apt-get install -y docker-ce docker-ce-cli containerd.io \
     docker-buildx-plugin docker-compose-plugin 2>&1; then
    die "Docker CE installation failed"
fi
ok "Docker CE installed."

# Add default user to docker group
if id "ubuntu" &>/dev/null; then
    usermod -aG docker ubuntu
    ok "User 'ubuntu' added to docker group."
fi

# Disable unattended-upgrades (prevents surprise reboots)
if dpkg -l unattended-upgrades &>/dev/null 2>&1; then
    apt-get remove -y unattended-upgrades >/dev/null 2>&1 || true
    ok "unattended-upgrades removed."
fi

# ===================================================================
# [5/12] Create docker-compose compatibility wrapper
# ===================================================================

echo -e "${YELLOW}[5/12] Creating docker-compose compatibility wrapper...${NC}"
cat > /usr/local/bin/docker-compose << 'EOF'
#!/bin/bash
exec docker compose "$@"
EOF
chmod +x /usr/local/bin/docker-compose
ln -sf /usr/local/bin/docker-compose /usr/bin/docker-compose

ok "docker-compose wrapper created."

# ===================================================================
# [6/12] Enable IPv6 kernel params
# ===================================================================

echo -e "${YELLOW}[6/12] Enabling IPv6 kernel params...${NC}"
cp /etc/sysctl.conf /etc/sysctl.conf.bak_provision 2>/dev/null || true

# Remove existing entries then append (idempotent)
sed -i '/net.ipv6.conf.all.disable_ipv6/d' /etc/sysctl.conf
sed -i '/net.ipv6.conf.default.disable_ipv6/d' /etc/sysctl.conf
sed -i '/net.ipv6.conf.lo.disable_ipv6/d' /etc/sysctl.conf

echo "net.ipv6.conf.all.disable_ipv6 = 0" >> /etc/sysctl.conf
echo "net.ipv6.conf.default.disable_ipv6 = 0" >> /etc/sysctl.conf
echo "net.ipv6.conf.lo.disable_ipv6 = 0" >> /etc/sysctl.conf

sysctl -p > /dev/null 2>&1

# Best-effort networking restart (sysctl -p already applied the params)
if systemctl is-active NetworkManager &>/dev/null; then
    systemctl restart NetworkManager 2>/dev/null || true
elif systemctl is-active networking &>/dev/null; then
    systemctl restart networking 2>/dev/null || true
fi

ok "IPv6 kernel params enabled."

# ===================================================================
# [7/12] Enable BBR congestion control
# ===================================================================

echo -e "${YELLOW}[7/12] Enabling BBR congestion control...${NC}"

if sysctl net.ipv4.tcp_congestion_control 2>/dev/null | grep -q bbr; then
    ok "BBR already active."
else
    sed -i '/net.core.default_qdisc/d' /etc/sysctl.conf
    sed -i '/net.ipv4.tcp_congestion_control/d' /etc/sysctl.conf

    echo "net.core.default_qdisc = fq" >> /etc/sysctl.conf
    echo "net.ipv4.tcp_congestion_control = bbr" >> /etc/sysctl.conf

    sysctl -p > /dev/null 2>&1

    if sysctl net.ipv4.tcp_congestion_control 2>/dev/null | grep -q bbr; then
        ok "BBR enabled."
    else
        warn "BBR not available (kernel may not support it)."
    fi
fi

# ===================================================================
# [8/12] Configure Docker daemon (IPv6 + log rotation)
# ===================================================================

echo -e "${YELLOW}[8/12] Configuring Docker daemon...${NC}"
if [ -f /etc/docker/daemon.json ]; then
    cp /etc/docker/daemon.json /etc/docker/daemon.json.bak_$(date +%s)
fi

cat > /etc/docker/daemon.json <<DAEMONJSON
{
  "log-driver": "json-file",
  "log-opts": {
    "max-size": "100m",
    "max-file": "3"
  },
  "ipv6": true,
  "ip6tables": true,
  "fixed-cidr-v6": "fd00:1::/80"
}
DAEMONJSON

if ! systemctl restart docker 2>&1; then
    echo "  Retry docker restart in 5s..."
    sleep 5
    if ! systemctl restart docker 2>&1; then
        die "Docker daemon restart failed"
    fi
fi
ok "Docker daemon configured (IPv6 + log rotation)."

# ===================================================================
# [9/12] Install UFW-Docker security patch
# ===================================================================

echo -e "${YELLOW}[9/12] Installing UFW-Docker security patch...${NC}"
if command -v ufw >/dev/null && ufw status 2>/dev/null | grep -q "Status: active"; then
    if wget -q -O /usr/local/bin/ufw-docker \
         https://github.com/chaifeng/ufw-docker/raw/master/ufw-docker 2>/dev/null; then
        chmod +x /usr/local/bin/ufw-docker
        ufw-docker install >/dev/null 2>&1 || true
        ufw reload >/dev/null 2>&1 || true
        ok "UFW-Docker patch applied."
    else
        warn "Failed to download ufw-docker (network?), skipping."
    fi
else
    ok "UFW not active, skipping."
fi

# ===================================================================
# [10/12] Harden SSH: port 22 → 1022 only
# ===================================================================

echo -e "${YELLOW}[10/12] Hardening SSH: port 22 → 1022 only...${NC}"

# Already on 1022 only? Skip.
if ss -tlnp | grep -q ":1022 " && ! ss -tlnp | grep -q ":22 "; then
    ok "SSH already on port 1022 only."
else
    # ── Phase 1: Add port 1022 alongside current port ──

    # Back up sshd_config for rollback
    cp /etc/ssh/sshd_config /etc/ssh/sshd_config.bak_provision

    # Remove any manual socket override (conflicts with sshd-socket-generator on 24.04)
    rm -f /etc/systemd/system/ssh.socket.d/override.conf
    rmdir /etc/systemd/system/ssh.socket.d 2>/dev/null || true

    # Clean Port directives from drop-in configs (Ubuntu 24.04 has sshd_config.d/)
    for f in /etc/ssh/sshd_config.d/*.conf; do
        [ -f "$f" ] || continue
        sed -i '/^Port /d' "$f"
    done

    # Set both ports in sshd_config
    sed -i '/^Port /d' /etc/ssh/sshd_config
    sed -i '/^#Port /d' /etc/ssh/sshd_config
    # Append at end
    echo "" >> /etc/ssh/sshd_config
    echo "Port 22" >> /etc/ssh/sshd_config
    echo "Port 1022" >> /etc/ssh/sshd_config

    if restart_ssh; then
        sleep 2
        if ss -tlnp | grep -q ":1022 "; then
            echo -e "  Phase 1 OK: port 1022 listening"

            # ── Phase 2: Remove port 22 ──
            sed -i '/^Port 22$/d' /etc/ssh/sshd_config

            if restart_ssh; then
                sleep 2
                if ss -tlnp | grep -q ":1022 "; then
                    if ss -tlnp | grep -q ":22 "; then
                        ok "SSH port 1022 active. Port 22 still open (cloud firewall may control it)."
                    else
                        ok "SSH hardened: port 1022 only."
                    fi
                else
                    # Phase 2 restart killed SSH on 1022 — roll back to both ports
                    warn "Phase 2 failed, rolling back to both ports"
                    cp /etc/ssh/sshd_config.bak_provision /etc/ssh/sshd_config
                    # Re-set both ports
                    sed -i '/^Port /d' /etc/ssh/sshd_config
                    sed -i '/^#Port /d' /etc/ssh/sshd_config
                    echo "" >> /etc/ssh/sshd_config
                    echo "Port 22" >> /etc/ssh/sshd_config
                    echo "Port 1022" >> /etc/ssh/sshd_config
                    restart_ssh || true
                fi
            else
                # Phase 2 restart command failed — roll back to both ports
                warn "Phase 2 SSH restart failed, rolling back to both ports"
                cp /etc/ssh/sshd_config.bak_provision /etc/ssh/sshd_config
                sed -i '/^Port /d' /etc/ssh/sshd_config
                sed -i '/^#Port /d' /etc/ssh/sshd_config
                echo "" >> /etc/ssh/sshd_config
                echo "Port 22" >> /etc/ssh/sshd_config
                echo "Port 1022" >> /etc/ssh/sshd_config
                restart_ssh || true
            fi
        else
            # Phase 1 failed: 1022 not listening — full rollback
            warn "Phase 1 failed: port 1022 not listening. Rolling back."
            cp /etc/ssh/sshd_config.bak_provision /etc/ssh/sshd_config
            restart_ssh || true
        fi
    else
        # Phase 1 restart command failed — full rollback
        warn "Phase 1 SSH restart failed. Rolling back."
        cp /etc/ssh/sshd_config.bak_provision /etc/ssh/sshd_config
        restart_ssh || true
    fi
fi

# ===================================================================
# [11/12] Deploy auto-update cron
# ===================================================================

echo -e "${YELLOW}[11/12] Deploying auto-update cron...${NC}"

which crontab >/dev/null 2>&1 || apt-get install -y cron >/dev/null 2>&1 || true

if [ -f /apps/kaitu-slave/auto-update.sh ]; then
    CRON_EXISTS=$(crontab -l 2>/dev/null | grep -c 'auto-update.sh' || true)
    if [ "$CRON_EXISTS" = "0" ]; then
        (crontab -l 2>/dev/null; echo "0 20 * * * /apps/kaitu-slave/auto-update.sh") | crontab -
        ok "Cron entry added (20:00 UTC = 04:00 Beijing)."
    else
        ok "Cron entry already exists."
    fi
else
    warn "auto-update.sh not found yet. Deploy it with deploy-auto-update.sh after provisioning."
fi

# ===================================================================
# [12/12] Verify
# ===================================================================

echo -e "${YELLOW}[12/12] Verifying...${NC}"
echo -e "${BLUE}==================================================${NC}"
echo -e "Ubuntu:      ${UBUNTU_VERSION} (${UBUNTU_CODENAME})"
echo -e "Docker:      $(docker --version 2>/dev/null || echo 'NOT INSTALLED')"
echo -e "Compose:     $(docker compose version 2>/dev/null || echo 'NOT INSTALLED')"
echo -e "iptables:    $(iptables --version 2>/dev/null || echo 'NOT INSTALLED')"
echo -e "Docker IPv6: $(grep -q '"ipv6": true' /etc/docker/daemon.json 2>/dev/null && echo 'true' || echo 'false')"
echo -e "TCP CC:      $(sysctl -n net.ipv4.tcp_congestion_control 2>/dev/null || echo 'unknown')"
echo -e "SSH port:    $(ss -tlnp | grep -q ':1022 ' && echo '1022 OK' || echo 'NOT ON 1022')"
echo -e "Cron:        $(crontab -l 2>/dev/null | grep -c auto-update || echo 0) auto-update entry"

IPV6_ADDR=$(ip -6 addr show scope global 2>/dev/null | grep inet6 | awk '{print $2}' | head -n 1)
if [ -n "$IPV6_ADDR" ]; then
    echo -e "Host IPv6:   ${GREEN}${IPV6_ADDR}${NC}"
    if ping6 -c 2 -W 3 ipv6.google.com > /dev/null 2>&1; then
        echo -e "IPv6 conn:   ${GREEN}OK${NC}"
    else
        echo -e "IPv6 conn:   ${YELLOW}FAILED (check cloud firewall)${NC}"
    fi
else
    echo -e "Host IPv6:   ${YELLOW}not detected (check cloud console)${NC}"
fi

echo -e "${BLUE}==================================================${NC}"

if [ $ERRORS -gt 0 ]; then
    echo -e "${RED}   Provisioning completed with $ERRORS error(s), $WARNINGS warning(s).${NC}"
else
    echo -e "${GREEN}   Provisioning complete. $WARNINGS warning(s).${NC}"
fi

echo -e "${BLUE}==================================================${NC}"
echo ""
echo -e "${YELLOW}Next steps:${NC}"
echo -e "  1. Deploy docker-compose.yml + .env to /apps/kaitu-slave/"
echo -e "  2. Deploy auto-update.sh via deploy-auto-update.sh"
echo -e "  3. docker compose up -d && verify sidecar healthy"
echo -e "  4. Verify hop port DNAT: iptables -t nat -L PREROUTING -n | grep REDIRECT"
