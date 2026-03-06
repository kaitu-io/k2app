#!/bin/bash
#
# Kaitu Center 服务一键部署脚本 (使用 devops server 工具 + systemd)
#
set -e

# 配置参数
APP_NAME=kaitu-center
BINARY_PATH="./release/$APP_NAME"
BASE_PATH="/apps/kaitu"
CENTER_HOSTS=(center-1 center-2)

# 检查必需的工具
if ! command -v devops &> /dev/null; then
    echo "错误: devops 命令未找到，请先安装 devops 工具"
    exit 1
fi

# 检查二进制文件是否存在
if [ ! -f "$BINARY_PATH" ]; then
    echo "错误: 二进制文件 $BINARY_PATH 不存在，请先运行 make build-center"
    exit 1
fi

echo "================================================"
echo "部署到 center 集群 (${CENTER_HOSTS[*]})..."
echo "================================================"

# 1. 上传新版本
echo ""
echo "步骤 1/3: 上传新版本..."
for host in "${CENTER_HOSTS[@]}"; do
    devops server upload-file --host "$host" --remote-path $BASE_PATH/ $BINARY_PATH
done

# 2. 安装/更新 systemd 服务
echo ""
echo "步骤 2/3: 安装/更新 systemd 服务..."
for host in "${CENTER_HOSTS[@]}"; do
    devops server run-cmd --host "$host" "cd $BASE_PATH && sudo ./$APP_NAME install -c $BASE_PATH/config.yml"
done

# 暂停等待管理员操作
echo ""
echo "================================================"
echo "⚠️  部署已暂停，等待管理员操作"
echo "================================================"
echo ""
echo "现在可以执行以下操作："
echo ""
echo "1. 运行数据库迁移（如有需要）："
for host in "${CENTER_HOSTS[@]}"; do
    echo "   devops server run-cmd --host $host 'cd $BASE_PATH && sudo ./$APP_NAME migrate'"
done
echo ""
echo "2. 检查配置文件："
for host in "${CENTER_HOSTS[@]}"; do
    echo "   devops server run-cmd --host $host 'cat $BASE_PATH/config.yml'"
done
echo ""
echo "3. 执行其他必要的操作..."
echo ""
echo "================================================"
echo ""
read -p "完成必要操作后，按回车键继续重启服务... " -r
echo ""

# 3. 重启服务
echo ""
echo "步骤 3/3: 重启服务..."
for host in "${CENTER_HOSTS[@]}"; do
    devops server run-cmd --host "$host" "sudo systemctl restart kaitu-center && sleep 2 && sudo systemctl status kaitu-center --no-pager"
done

echo ""
echo "================================================"
echo "✅ 部署完成！"
echo "================================================"
echo ""
echo "查看服务状态:"
for host in "${CENTER_HOSTS[@]}"; do
    echo "  devops server run-cmd --host $host 'systemctl status kaitu-center'"
done
echo ""
echo "查看服务日志:"
for host in "${CENTER_HOSTS[@]}"; do
    echo "  devops server run-cmd --host $host 'journalctl -u kaitu-center -f'"
done
echo ""
