#!/bin/bash
# Provision a fresh Ubuntu node for Kaitu VPN service
#
# Combines: Docker CE install + IPv6 + BBR + SSH hardening + auto-update cron
# Target: Ubuntu 20.04 / 22.04 / 24.04
#
# What it does (12 steps):
#   1. Clean old Docker versions
#   2. Ensure iptables is installed (do NOT switch backend)
#   3. Configure official Docker apt source
#   4. Install Docker CE + plugins + docker group + disable unattended-upgrades
#   5. Create docker-compose compatibility wrapper
#   6. Enable IPv6 kernel params (sysctl)
#   7. Enable BBR congestion control (sysctl)
#   8. Configure Docker daemon (IPv6 + log rotation)
#   9. Install UFW-Docker security patch (if UFW present)
#  10. Harden SSH: switch to port 1022 only (disable port 22)
#  11. Deploy auto-update cron (daily 04:00 Beijing = 20:00 UTC)
#  12. Verify everything works
#
# Usage:
#   sudo bash provision-node.sh

set -e
export DEBIAN_FRONTEND=noninteractive

GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}==================================================${NC}"
echo -e "${BLUE}   Kaitu Node Provisioning (Ubuntu 20/22/24)      ${NC}"
echo -e "${BLUE}==================================================${NC}"

# --- Prerequisites ---

if [[ $EUID -ne 0 ]]; then
   echo -e "${RED}ERROR: Must run as root (sudo bash $0)${NC}"
   exit 1
fi

if ! grep -q "Ubuntu" /etc/os-release; then
    echo -e "${RED}ERROR: Ubuntu only.${NC}"
    exit 1
fi

# Detect Ubuntu version
UBUNTU_VERSION=$(. /etc/os-release && echo "$VERSION_ID")
UBUNTU_CODENAME=$(. /etc/os-release && echo "$VERSION_CODENAME")
echo -e "Detected: Ubuntu ${UBUNTU_VERSION} (${UBUNTU_CODENAME})"

# --- [1/12] Clean old Docker versions ---

echo -e "${YELLOW}[1/12] Cleaning old Docker versions...${NC}"
systemctl stop docker >/dev/null 2>&1 || true
systemctl stop docker.socket >/dev/null 2>&1 || true

apt-get remove -y docker docker-engine docker.io containerd runc \
    docker-compose docker-compose-v2 podman-docker >/dev/null 2>&1 || true
apt-get autoremove -y >/dev/null 2>&1

rm -f /usr/local/bin/docker-compose
rm -f /usr/bin/docker-compose

echo -e "${GREEN}>>> Old versions cleaned.${NC}"

# --- [2/12] Ensure iptables is installed ---

echo -e "${YELLOW}[2/12] Ensuring iptables is installed...${NC}"
apt-get update >/dev/null
apt-get install -y iptables >/dev/null

# IMPORTANT: Do NOT switch iptables backend with update-alternatives.
#
# Why: Cloud providers (Lightsail/EC2/Aliyun) inject legacy iptables rules for
# internal networking. Switching to nftables backend creates two separate rule
# sets in two kernel modules → routing conflict → SSH and networking break.
#
# The OS default works for everything:
#   - Ubuntu 20.04: iptables-legacy (rules in legacy kernel module)
#   - Ubuntu 22.04/24.04: iptables-nft (translates iptables syntax to nftables kernel)
#   - Docker works with both backends transparently
#   - Hop port DNAT (PREROUTING REDIRECT) works identically on both backends
#   - Cloud provider networking stays intact

echo -e "${GREEN}>>> iptables ready (using OS default backend).${NC}"

# --- [3/12] Configure official Docker apt source ---

echo -e "${YELLOW}[3/12] Configuring official Docker apt source...${NC}"
apt-get install -y ca-certificates curl gnupg >/dev/null

install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | gpg --dearmor --yes -o /etc/apt/keyrings/docker.gpg
chmod a+r /etc/apt/keyrings/docker.gpg

echo \
  "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu \
  ${UBUNTU_CODENAME} stable" | \
  tee /etc/apt/sources.list.d/docker.list > /dev/null

apt-get update >/dev/null
echo -e "${GREEN}>>> Apt source configured.${NC}"

# --- [4/12] Install Docker CE + plugins ---

echo -e "${YELLOW}[4/12] Installing Docker CE + plugins...${NC}"
apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

echo -e "${GREEN}>>> Docker CE installed.${NC}"

# Add default user to docker group (avoids needing sudo for docker commands)
if id "ubuntu" &>/dev/null; then
    usermod -aG docker ubuntu
    echo -e "${GREEN}>>> User 'ubuntu' added to docker group.${NC}"
fi

# Disable unattended-upgrades (prevents surprise reboots that kill containers)
if dpkg -l unattended-upgrades &>/dev/null 2>&1; then
    apt-get remove -y unattended-upgrades >/dev/null 2>&1
    echo -e "${GREEN}>>> unattended-upgrades removed.${NC}"
fi

# --- [5/12] Create docker-compose compatibility wrapper ---

echo -e "${YELLOW}[5/12] Creating docker-compose compatibility wrapper...${NC}"
cat > /usr/local/bin/docker-compose << 'EOF'
#!/bin/bash
exec docker compose "$@"
EOF
chmod +x /usr/local/bin/docker-compose
ln -sf /usr/local/bin/docker-compose /usr/bin/docker-compose

echo -e "${GREEN}>>> docker-compose wrapper created.${NC}"

# --- [6/12] Enable IPv6 kernel params ---

echo -e "${YELLOW}[6/12] Enabling IPv6 kernel params...${NC}"
cp /etc/sysctl.conf /etc/sysctl.conf.bak_provision 2>/dev/null || true

sed -i '/net.ipv6.conf.all.disable_ipv6/d' /etc/sysctl.conf
sed -i '/net.ipv6.conf.default.disable_ipv6/d' /etc/sysctl.conf
sed -i '/net.ipv6.conf.lo.disable_ipv6/d' /etc/sysctl.conf

echo "net.ipv6.conf.all.disable_ipv6 = 0" >> /etc/sysctl.conf
echo "net.ipv6.conf.default.disable_ipv6 = 0" >> /etc/sysctl.conf
echo "net.ipv6.conf.lo.disable_ipv6 = 0" >> /etc/sysctl.conf

sysctl -p > /dev/null 2>&1

# Restart networking to pick up IPv6
if systemctl list-units --full -all | grep -q "NetworkManager.service"; then
    systemctl restart NetworkManager
elif systemctl list-units --full -all | grep -q "networking.service"; then
    systemctl restart networking
fi

sleep 3
echo -e "${GREEN}>>> IPv6 kernel params enabled.${NC}"

# --- [7/12] Enable BBR congestion control ---

echo -e "${YELLOW}[7/12] Enabling BBR congestion control...${NC}"

if sysctl net.ipv4.tcp_congestion_control 2>/dev/null | grep -q bbr; then
    echo -e "${GREEN}>>> BBR already active.${NC}"
else
    sed -i '/net.core.default_qdisc/d' /etc/sysctl.conf
    sed -i '/net.ipv4.tcp_congestion_control/d' /etc/sysctl.conf

    echo "net.core.default_qdisc = fq" >> /etc/sysctl.conf
    echo "net.ipv4.tcp_congestion_control = bbr" >> /etc/sysctl.conf

    sysctl -p > /dev/null 2>&1

    if sysctl net.ipv4.tcp_congestion_control 2>/dev/null | grep -q bbr; then
        echo -e "${GREEN}>>> BBR enabled.${NC}"
    else
        echo -e "${YELLOW}>>> BBR not available (kernel may not support it).${NC}"
    fi
fi

# --- [8/12] Configure Docker daemon (IPv6 + log rotation) ---

echo -e "${YELLOW}[8/12] Configuring Docker daemon...${NC}"
if [ -f /etc/docker/daemon.json ]; then
    cp /etc/docker/daemon.json /etc/docker/daemon.json.bak_$(date +%s)
fi

cat > /etc/docker/daemon.json <<EOF
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
EOF

systemctl restart docker
echo -e "${GREEN}>>> Docker daemon configured (IPv6 + log rotation).${NC}"

# --- [9/12] Install UFW-Docker security patch ---

echo -e "${YELLOW}[9/12] Installing UFW-Docker security patch...${NC}"
if command -v ufw >/dev/null; then
    wget -O /usr/local/bin/ufw-docker https://github.com/chaifeng/ufw-docker/raw/master/ufw-docker >/dev/null 2>&1
    chmod +x /usr/local/bin/ufw-docker
    ufw-docker install >/dev/null 2>&1
    ufw reload >/dev/null 2>&1
    echo -e "${GREEN}>>> UFW-Docker patch applied.${NC}"
else
    echo -e "${YELLOW}>>> UFW not found, skipping.${NC}"
fi

# --- [10/12] Harden SSH: port 22 → 1022 only ---

echo -e "${YELLOW}[10/12] Hardening SSH: port 22 → 1022 only...${NC}"

# Two-phase approach to prevent lockout:
#   Phase 1: Add port 1022 alongside 22 → restart → verify 1022 listening
#   Phase 2: Remove port 22 → restart → verify only 1022 remains
# If phase 1 fails, port 22 is still open as fallback and script aborts.

# ── Phase 1: Add port 1022 alongside 22 ──

HAS_SOCKET=false
if systemctl list-unit-files ssh.socket 2>/dev/null | grep -q ssh.socket; then
    HAS_SOCKET=true
    echo -e "  Detected: systemd socket activation (Ubuntu 24.04+)"

    mkdir -p /etc/systemd/system/ssh.socket.d
    cat > /etc/systemd/system/ssh.socket.d/override.conf << 'SSHEOF'
[Socket]
ListenStream=
ListenStream=22
ListenStream=[::]:22
ListenStream=1022
ListenStream=[::]:1022
SSHEOF
    systemctl daemon-reload
    systemctl restart ssh.socket
fi

# Update sshd_config: set both ports
sed -i '/^Port /d' /etc/ssh/sshd_config
sed -i 's/^#Port 22//' /etc/ssh/sshd_config
echo -e "\nPort 22\nPort 1022" >> /etc/ssh/sshd_config

# Clean Port directives from drop-in configs (Ubuntu 24.04)
for f in /etc/ssh/sshd_config.d/*.conf; do
    [ -f "$f" ] || continue
    sed -i '/^Port /d' "$f"
done

# Restart SSH (non-socket path: Ubuntu 20.04/22.04)
if [ "$HAS_SOCKET" = false ]; then
    if systemctl list-unit-files sshd.service 2>/dev/null | grep -q sshd; then
        systemctl restart sshd
    else
        systemctl restart ssh
    fi
fi

sleep 1
if ! ss -tlnp | grep -q ":1022 "; then
    echo -e "${RED}>>> ABORT: Port 1022 not listening. Port 22 still active. Fix manually!${NC}"
    exit 1
fi
echo -e "  Phase 1 OK: port 1022 listening"

# ── Phase 2: Remove port 22 ──

sed -i '/^Port 22$/d' /etc/ssh/sshd_config

if [ "$HAS_SOCKET" = true ]; then
    cat > /etc/systemd/system/ssh.socket.d/override.conf << 'SSHEOF'
[Socket]
ListenStream=
ListenStream=1022
ListenStream=[::]:1022
SSHEOF
    systemctl daemon-reload
    systemctl restart ssh.socket
else
    if systemctl list-unit-files sshd.service 2>/dev/null | grep -q sshd; then
        systemctl restart sshd
    else
        systemctl restart ssh
    fi
fi

sleep 1
if ss -tlnp | grep -q ":1022 " && ! ss -tlnp | grep -q ":22 "; then
    echo -e "${GREEN}>>> SSH hardened: port 1022 only.${NC}"
elif ss -tlnp | grep -q ":1022 "; then
    echo -e "${YELLOW}>>> SSH port 1022 active. Port 22 still open (check cloud firewall).${NC}"
else
    echo -e "${RED}>>> WARNING: SSH port 1022 not detected after phase 2!${NC}"
fi

# --- [11/12] Deploy auto-update cron ---

echo -e "${YELLOW}[11/12] Deploying auto-update cron...${NC}"

which crontab >/dev/null 2>&1 || apt-get install -y cron >/dev/null 2>&1

if [ -f /apps/kaitu-slave/auto-update.sh ]; then
    CRON_EXISTS=$(crontab -l 2>/dev/null | grep -c 'auto-update.sh' || true)
    if [ "$CRON_EXISTS" = "0" ]; then
        (crontab -l 2>/dev/null; echo "0 20 * * * /apps/kaitu-slave/auto-update.sh") | crontab -
        echo -e "${GREEN}>>> Cron entry added (20:00 UTC = 04:00 Beijing).${NC}"
    else
        echo -e "${GREEN}>>> Cron entry already exists.${NC}"
    fi
else
    echo -e "${YELLOW}>>> auto-update.sh not found yet. Deploy it with deploy-auto-update.sh after provisioning.${NC}"
fi

# --- [12/12] Verify ---

echo -e "${YELLOW}[12/12] Verifying...${NC}"
echo -e "${BLUE}==================================================${NC}"
echo -e "Ubuntu:     ${UBUNTU_VERSION} (${UBUNTU_CODENAME})"
echo -e "Docker:     $(docker --version)"
echo -e "Compose:    $(docker-compose version)"
echo -e "iptables:   $(iptables --version) ($(readlink -f /usr/sbin/iptables 2>/dev/null | grep -q nft && echo 'nftables backend' || echo 'legacy backend'))"
echo -e "Docker IPv6: $(docker info --format '{{.IPv6}}')"
echo -e "TCP CC:     $(sysctl -n net.ipv4.tcp_congestion_control 2>/dev/null || echo 'unknown')"
echo -e "SSH port:   $(ss -tlnp | grep -q ':1022 ' && echo '1022 OK' || echo 'NOT LISTENING')"
echo -e "Cron:       $(crontab -l 2>/dev/null | grep -c auto-update) auto-update entry"

IPV6_ADDR=$(ip -6 addr show scope global | grep inet6 | awk '{print $2}' | head -n 1)
if [ -n "$IPV6_ADDR" ]; then
    echo -e "Host IPv6:  ${GREEN}${IPV6_ADDR}${NC}"
    if ping6 -c 2 -W 3 ipv6.google.com > /dev/null 2>&1; then
        echo -e "IPv6 conn:  ${GREEN}OK${NC}"
    else
        echo -e "IPv6 conn:  ${RED}FAILED (check firewall)${NC}"
    fi
else
    echo -e "Host IPv6:  ${RED}not detected (check cloud console)${NC}"
fi

echo -e "${BLUE}==================================================${NC}"
echo -e "${GREEN}   Provisioning complete.${NC}"
echo -e "${BLUE}==================================================${NC}"
echo ""
echo -e "${YELLOW}Next steps:${NC}"
echo -e "  1. Deploy docker-compose.yml + .env to /apps/kaitu-slave/"
echo -e "  2. Deploy auto-update.sh via deploy-auto-update.sh"
echo -e "  3. docker compose up -d && verify sidecar healthy"
echo -e "  4. Verify hop port DNAT: iptables -t nat -L PREROUTING -n | grep REDIRECT"
