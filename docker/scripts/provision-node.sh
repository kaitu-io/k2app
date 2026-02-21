#!/bin/bash
# Provision a fresh Ubuntu node for Kaitu VPN service
#
# Combines: Docker CE install + IPv6 kernel enablement + security hardening
# Target: Ubuntu 20.04 / 22.04 / 24.04
#
# What it does (9 steps):
#   1. Clean old Docker versions
#   2. Unify firewall backend to nftables
#   3. Configure official Docker apt source
#   4. Install Docker CE + plugins
#   5. Create docker-compose compatibility wrapper
#   6. Enable IPv6 kernel params (sysctl)
#   7. Configure Docker daemon (IPv6 + log rotation)
#   8. Install UFW-Docker security patch (if UFW present)
#   9. Verify everything works
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

# --- [1/9] Clean old Docker versions ---

echo -e "${YELLOW}[1/9] Cleaning old Docker versions...${NC}"
systemctl stop docker >/dev/null 2>&1 || true
systemctl stop docker.socket >/dev/null 2>&1 || true

apt-get remove -y docker docker-engine docker.io containerd runc \
    docker-compose docker-compose-v2 podman-docker >/dev/null 2>&1 || true
apt-get autoremove -y >/dev/null 2>&1

rm -f /usr/local/bin/docker-compose
rm -f /usr/bin/docker-compose

echo -e "${GREEN}>>> Old versions cleaned.${NC}"

# --- [2/9] Unify firewall backend to nftables ---

echo -e "${YELLOW}[2/9] Unifying firewall backend to nftables...${NC}"
apt-get update >/dev/null
apt-get install -y iptables >/dev/null

update-alternatives --set iptables /usr/sbin/iptables-nft >/dev/null 2>&1 || true
update-alternatives --set iptables-restore /usr/sbin/iptables-nft-restore >/dev/null 2>&1 || true
update-alternatives --set iptables-save /usr/sbin/iptables-nft-save >/dev/null 2>&1 || true
update-alternatives --set ip6tables /usr/sbin/ip6tables-nft >/dev/null 2>&1 || true
update-alternatives --set ip6tables-restore /usr/sbin/ip6tables-nft-restore >/dev/null 2>&1 || true
update-alternatives --set ip6tables-save /usr/sbin/ip6tables-nft-save >/dev/null 2>&1 || true

echo -e "${GREEN}>>> Firewall backend unified to nftables.${NC}"

# --- [3/9] Configure official Docker apt source ---

echo -e "${YELLOW}[3/9] Configuring official Docker apt source...${NC}"
apt-get install -y ca-certificates curl gnupg >/dev/null

install -m 0755 -d /etc/apt/keyrings
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | gpg --dearmor --yes -o /etc/apt/keyrings/docker.gpg
chmod a+r /etc/apt/keyrings/docker.gpg

echo \
  "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu \
  $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | \
  tee /etc/apt/sources.list.d/docker.list > /dev/null

apt-get update >/dev/null
echo -e "${GREEN}>>> Apt source configured.${NC}"

# --- [4/9] Install Docker CE + plugins ---

echo -e "${YELLOW}[4/9] Installing Docker CE + plugins...${NC}"
apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

echo -e "${GREEN}>>> Docker CE installed.${NC}"

# --- [5/9] Create docker-compose compatibility wrapper ---

echo -e "${YELLOW}[5/9] Creating docker-compose compatibility wrapper...${NC}"
cat > /usr/local/bin/docker-compose << 'EOF'
#!/bin/bash
exec docker compose "$@"
EOF
chmod +x /usr/local/bin/docker-compose
ln -sf /usr/local/bin/docker-compose /usr/bin/docker-compose

echo -e "${GREEN}>>> docker-compose wrapper created.${NC}"

# --- [6/9] Enable IPv6 kernel params ---

echo -e "${YELLOW}[6/9] Enabling IPv6 kernel params...${NC}"
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

# --- [7/9] Configure Docker daemon (IPv6 + log rotation) ---

echo -e "${YELLOW}[7/9] Configuring Docker daemon...${NC}"
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

# --- [8/9] Install UFW-Docker security patch ---

echo -e "${YELLOW}[8/9] Installing UFW-Docker security patch...${NC}"
if command -v ufw >/dev/null; then
    wget -O /usr/local/bin/ufw-docker https://github.com/chaifeng/ufw-docker/raw/master/ufw-docker >/dev/null 2>&1
    chmod +x /usr/local/bin/ufw-docker
    ufw-docker install >/dev/null 2>&1
    ufw reload >/dev/null 2>&1
    echo -e "${GREEN}>>> UFW-Docker patch applied.${NC}"
else
    echo -e "${YELLOW}>>> UFW not found, skipping.${NC}"
fi

# --- [9/9] Verify ---

echo -e "${YELLOW}[9/9] Verifying...${NC}"
echo -e "${BLUE}==================================================${NC}"
echo -e "Docker:   $(docker --version)"
echo -e "Compose:  $(docker-compose version)"
echo -e "iptables: $(iptables --version)"
echo -e "Docker IPv6: $(docker info --format '{{.IPv6}}')"

IPV6_ADDR=$(ip -6 addr show scope global | grep inet6 | awk '{print $2}' | head -n 1)
if [ -n "$IPV6_ADDR" ]; then
    echo -e "Host IPv6: ${GREEN}${IPV6_ADDR}${NC}"
    if ping6 -c 2 -W 3 ipv6.google.com > /dev/null 2>&1; then
        echo -e "IPv6 connectivity: ${GREEN}OK${NC}"
    else
        echo -e "IPv6 connectivity: ${RED}FAILED (check firewall)${NC}"
    fi
else
    echo -e "Host IPv6: ${RED}not detected (check cloud console)${NC}"
fi

echo -e "${BLUE}==================================================${NC}"
echo -e "${GREEN}   Provisioning complete.${NC}"
echo -e "${BLUE}==================================================${NC}"
