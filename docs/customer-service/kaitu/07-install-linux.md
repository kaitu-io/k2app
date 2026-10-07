# Linux 安装指南

## 系统要求

- 使用 systemd 的主流发行版（Ubuntu、Debian、Fedora 等）；不支持 OpenRC / sysvinit
- 架构：amd64 或 arm64
- 需要 root 权限

Linux 版是一个后台服务 + 浏览器界面，不需要安装图形依赖。

## 一条命令安装（推荐）

打开终端，执行：

`curl -fsSL https://kaitu.io/i/k2 | sudo bash`

默认安装测试版（beta）。想装稳定版：

`curl -fsSL https://kaitu.io/i/k2 | sudo bash -s -- --channel=stable`

脚本会自动下载、校验并安装，然后在浏览器里打开开途界面（地址 http://127.0.0.1:1777）。用邮箱登录（验证码或密码），点击连接即可。

## 手动安装

1. 下载安装包：`https://dl.kaitu.io/kaitu/desktop/版本号/Kaitu_版本号_linux_amd64.tar.gz`（arm64 机器把 amd64 换成 arm64）
2. 新建一个空文件夹，把安装包解压到里面（文件直接在根目录，没有子文件夹）
3. 在该文件夹里执行：`sudo ./install.sh`

## 常用命令

- 查看状态：`systemctl status kaitu`
- 重启：`sudo systemctl restart kaitu`
- 查看日志：`journalctl -u kaitu -f`

## 升级

重新执行一次安装命令即可，设置会保留。界面内容会自动更新。

## 卸载

在安装目录执行 `sudo ./uninstall.sh`；加 `--purge` 会同时删除配置。

## 远程使用

界面只监听本机 127.0.0.1:1777。如果要从别的电脑访问，请用 SSH 隧道：
`ssh -L 1777:127.0.0.1:1777 用户名@服务器`
然后在本地浏览器打开 http://127.0.0.1:1777。
