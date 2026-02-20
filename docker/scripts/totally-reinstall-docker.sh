#!/bin/bash

# ============================================================
# Docker 环境标准化全能脚本 (适用于 Ubuntu 20.04/22.04/24.04)
# 功能：清洗旧环境 -> 安装官方版本 -> 修正防火墙后端 -> 安全加固
# ============================================================

set -e # 遇到错误立即停止
export DEBIAN_FRONTEND=noninteractive

# 颜色定义
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}==================================================${NC}"
echo -e "${BLUE}   开始执行 Docker 标准化流程 (Ubuntu 20/22/24)   ${NC}"
echo -e "${BLUE}==================================================${NC}"

# 0. 权限与系统检查
if [[ $EUID -ne 0 ]]; then
   echo -e "${RED}错误: 必须使用 root 权限运行 (sudo bash $0)${NC}"
   exit 1
fi

if ! grep -q "Ubuntu" /etc/os-release; then
    echo -e "${RED}错误: 此脚本仅支持 Ubuntu 系统。${NC}"
    exit 1
fi

echo -e "${YELLOW}[1/7] 正在清理旧版本残留...${NC}"
# 停止服务
systemctl stop docker >/dev/null 2>&1 || true
systemctl stop docker.socket >/dev/null 2>&1 || true

# 卸载各类五花八门的 Docker 版本
# 注意：这不会删除 /var/lib/docker 下的容器数据，只卸载软件
apt-get remove -y docker docker-engine docker.io containerd runc docker-compose docker-compose-v2 podman-docker >/dev/null 2>&1 || true
apt-get autoremove -y >/dev/null 2>&1

# 清理可能存在的旧版 compose 二进制文件
rm -f /usr/local/bin/docker-compose
rm -f /usr/bin/docker-compose

echo -e "${GREEN}>>> 旧版本清理完成。${NC}"

echo -e "${YELLOW}[2/7] 统一底层防火墙后端为 nftables...${NC}"
# 这是一个关键步骤，解决 20.04/22.04/24.04 的 iptables 差异
apt-get update >/dev/null
apt-get install -y iptables >/dev/null

# 强制设置所有 alternatives 指向 nft
update-alternatives --set iptables /usr/sbin/iptables-nft >/dev/null 2>&1 || true
update-alternatives --set iptables-restore /usr/sbin/iptables-nft-restore >/dev/null 2>&1 || true
update-alternatives --set iptables-save /usr/sbin/iptables-nft-save >/dev/null 2>&1 || true
update-alternatives --set ip6tables /usr/sbin/ip6tables-nft >/dev/null 2>&1 || true
update-alternatives --set ip6tables-restore /usr/sbin/ip6tables-nft-restore >/dev/null 2>&1 || true
update-alternatives --set ip6tables-save /usr/sbin/ip6tables-nft-save >/dev/null 2>&1 || true

echo -e "${GREEN}>>> 防火墙后端已统一为 nftables。${NC}"

echo -e "${YELLOW}[3/7] 配置官方软件源...${NC}"
apt-get install -y ca-certificates curl gnupg >/dev/null

# 添加官方 GPG Key (覆盖模式，防止 key 过期)
install -m 0755 -d /etc/apt/keyrings
# 修复点：已将 -yes 改为 --yes
curl -fsSL https://download.docker.com/linux/ubuntu/gpg | gpg --dearmor --yes -o /etc/apt/keyrings/docker.gpg
chmod a+r /etc/apt/keyrings/docker.gpg

# 添加仓库
echo \
  "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu \
  $(. /etc/os-release && echo "$VERSION_CODENAME") stable" | \
  tee /etc/apt/sources.list.d/docker.list > /dev/null

apt-get update >/dev/null
echo -e "${GREEN}>>> 软件源配置完成。${NC}"

echo -e "${YELLOW}[4/7] 安装 Docker CE 及插件...${NC}"
apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin

echo -e "${GREEN}>>> 软件安装完成。${NC}"

echo -e "${YELLOW}[5/7] 修正兼容性 (docker-compose 命令)...${NC}"
# 创建 Wrapper 脚本，完美模拟旧版命令
cat > /usr/local/bin/docker-compose << 'EOF'
#!/bin/bash
# 这是一个兼容性 Wrapper，将 docker-compose 命令转发给 docker compose 插件
exec docker compose "$@"
EOF
chmod +x /usr/local/bin/docker-compose
# 确保 path 优先级
ln -sf /usr/local/bin/docker-compose /usr/bin/docker-compose

echo -e "${GREEN}>>> 兼容性修正完成 (现在可以使用 'docker-compose' 命令了)。${NC}"

echo -e "${YELLOW}[6/7] 配置 Daemon (IPv6 + 日志轮转)...${NC}"
# 备份旧配置
if [ -f /etc/docker/daemon.json ]; then
    cp /etc/docker/daemon.json /etc/docker/daemon.json.bak_$(date +%s)
fi

# 写入标准配置
# 注意：fixed-cidr-v6 使用 fd00::/80 是为了给容器分配私有 IPv6，防止启动报错
# 配合您之前的 enable_ipv6.sh，容器可以通过 NAT 访问外网 IPv6
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

# 重启 Docker 加载配置
systemctl restart docker

echo -e "${GREEN}>>> Daemon 配置已更新并重启。${NC}"

echo -e "${YELLOW}[7/7] 安装 UFW-Docker 安全补丁...${NC}"
# 只有在安装了 UFW 的情况下才配置
if command -v ufw >/dev/null; then
    wget -O /usr/local/bin/ufw-docker https://github.com/chaifeng/ufw-docker/raw/master/ufw-docker >/dev/null 2>&1
    chmod +x /usr/local/bin/ufw-docker
    ufw-docker install >/dev/null 2>&1
    ufw reload >/dev/null 2>&1
    echo -e "${GREEN}>>> UFW-Docker 安全补丁已应用。${NC}"
else
    echo -e "${YELLOW}>>> 未检测到 UFW，跳过安全补丁。${NC}"
fi

echo -e "${BLUE}==================================================${NC}"
echo -e "${GREEN}   ✅ 所有操作成功完成！环境已标准化。   ${NC}"
echo -e "${BLUE}==================================================${NC}"
echo -e "版本检查："
echo -e "Docker:  $(docker --version)"
echo -e "Compose: $(docker-compose version)"
echo -e "IPTable: $(iptables --version)"
echo -e "IPv6:    $(docker info --format '{{.IPv6}}')"