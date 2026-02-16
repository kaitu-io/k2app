#!/bin/bash
#
# Kaitu Center 服务一键部署脚本 (使用 devops server 工具 + systemd)
#
set -e

# 配置参数
APP_NAME=kaitu-center
BINARY_PATH="./release/$APP_NAME"
BASE_PATH="/apps/kaitu"
CENTER_CLUSTER="center"

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
echo "部署到 $CENTER_CLUSTER 集群..."
echo "================================================"

# 1. 上传新版本
echo ""
echo "步骤 1/3: 上传新版本..."
devops server upload-file --cluster $CENTER_CLUSTER --remote-path $BASE_PATH/ $BINARY_PATH

# 3. 安装/更新 systemd 服务
echo ""
echo "步骤 2/3: 安装/更新 systemd 服务..."
devops server run-cmd --cluster $CENTER_CLUSTER "cd $BASE_PATH && sudo ./$APP_NAME install -c $BASE_PATH/config.yml"

# 暂停等待管理员操作
echo ""
echo "================================================"
echo "⚠️  部署已暂停，等待管理员操作"
echo "================================================"
echo ""
echo "现在可以执行以下操作："
echo ""
echo "1. 运行数据库迁移（如有需要）："
echo "   devops server run-cmd --cluster $CENTER_CLUSTER 'cd $BASE_PATH && sudo ./$APP_NAME migrate'"
echo ""
echo "2. 检查配置文件："
echo "   devops server run-cmd --cluster $CENTER_CLUSTER 'cat $BASE_PATH/config.yml'"
echo ""
echo "3. 执行其他必要的操作..."
echo ""
echo "================================================"
echo ""
read -p "完成必要操作后，按回车键继续重启服务... " -r
echo ""

# 4. 重启服务
echo ""
echo "步骤 3/3: 重启服务..."
devops server run-cmd --cluster $CENTER_CLUSTER "sudo systemctl restart kaitu-center && sleep 2 && sudo systemctl status kaitu-center --no-pager"

echo ""
echo "================================================"
echo "✅ 部署完成！"
echo "================================================"
echo ""
echo "查看服务状态:"
echo "  devops server run-cmd --cluster $CENTER_CLUSTER 'systemctl status kaitu-center'"
echo ""
echo "查看服务日志:"
echo "  devops server run-cmd --cluster $CENTER_CLUSTER 'journalctl -u kaitu-center -f'"
echo ""

