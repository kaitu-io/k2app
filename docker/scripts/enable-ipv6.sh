#!/bin/bash

# 定义颜色
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m'

# 检查 Root 权限
if [[ $EUID -ne 0 ]]; then
   echo -e "${RED}错误: 请使用 root 权限运行此脚本 (sudo bash enable_ipv6.sh)${NC}"
   exit 1
fi

echo -e "${YELLOW}>>> 开始配置 IPv6 内核参数...${NC}"

# 1. 备份 sysctl.conf
cp /etc/sysctl.conf /etc/sysctl.conf.bak_ipv6_script 2>/dev/null
echo -e "已备份配置文件"

# 2. 修改 /etc/sysctl.conf
# 删除旧配置
sed -i '/net.ipv6.conf.all.disable_ipv6/d' /etc/sysctl.conf
sed -i '/net.ipv6.conf.default.disable_ipv6/d' /etc/sysctl.conf
sed -i '/net.ipv6.conf.lo.disable_ipv6/d' /etc/sysctl.conf

# 添加新配置
echo "net.ipv6.conf.all.disable_ipv6 = 0" >> /etc/sysctl.conf
echo "net.ipv6.conf.default.disable_ipv6 = 0" >> /etc/sysctl.conf
echo "net.ipv6.conf.lo.disable_ipv6 = 0" >> /etc/sysctl.conf

echo -e "${GREEN}内核参数已修改。${NC}"

# 3. 应用更改
sysctl -p > /dev/null 2>&1

# 4. 重启网络
echo -e "${YELLOW}>>> 正在刷新网络状态...${NC}"
if systemctl list-units --full -all | grep -q "NetworkManager.service"; then
    systemctl restart NetworkManager
elif systemctl list-units --full -all | grep -q "networking.service"; then
    systemctl restart networking
fi

sleep 3

# 5. 打印结果
echo -e "----------------------------------------"
echo -e "${YELLOW}>>> IPv6 地址检测结果：${NC}"

IPV6_ADDR=$(ip -6 addr show scope global | grep inet6 | awk '{print $2}' | head -n 1)

if [ -z "$IPV6_ADDR" ]; then
    echo -e "${RED}警告: 未检测到 IPv6 地址。${NC}"
    echo "请检查云控制台是否已分配 IPv6，或检查 /etc/network/interfaces 配置。"
else
    echo -e "本机 IPv6 地址: ${GREEN}${IPV6_ADDR}${NC}"
    
    echo -e "${YELLOW}>>> 测试外部连通性...${NC}"
    if ping6 -c 3 ipv6.google.com > /dev/null 2>&1; then
        echo -e "外部连接: ${GREEN}成功${NC}"
    else
        echo -e "外部连接: ${RED}失败 (请检查防火墙)${NC}"
    fi
fi
echo -e "----------------------------------------"