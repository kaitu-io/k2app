# Linux 安装指南

## 系统要求

- 支持 Ubuntu、Debian、Fedora、Arch Linux 等主流带 systemd 的 Linux 发行版
- 需要 systemd（`systemctl` 命令）— 非 systemd 系统（如 OpenRC、sysvinit）暂不支持
- 仅支持 amd64（x86_64）架构
- 需要 root 权限（一键脚本会自动通过 sudo 提升）

## 架构说明

自 v0.4.2 起，Linux 版本不再采用图形外壳（Tauri），改为「Go 守护进程 + 嵌入式 Web UI」的方案：

- 安装后系统中只有一个可执行文件 `/usr/local/bin/k2`
- systemd 会把 `k2 run` 作为后台服务运行
- 用户在浏览器中打开 `http://127.0.0.1:1777` 使用开途，界面与 Windows/macOS 桌面端完全一致（同一份 React webapp）
- 不再需要 webkit2gtk、libfuse、AppImage 或 pkexec 等依赖

这种方式带来三个好处：

1. 无任何图形库依赖，在最小化服务器、远程 SSH 环境也能跑
2. 关闭浏览器不会断开 VPN — 守护进程独立运行，systemd 管理生命周期
3. 升级、启动、停止全部走 `systemctl`，与标准 Linux 运维习惯一致

## 方式一：一键脚本安装（推荐）

打开终端，运行：

```
curl -fsSL https://kaitu.io/i/k2 | sudo bash
```

脚本会自动完成：

- 从 CDN 下载 `Kaitu_<版本号>_linux_amd64.tar.gz`
- 解压到临时目录并执行其中的 `install.sh`
- 清理 v0.4.2 之前老版本（`/opt/kaitu` + `k2.service`）如果存在
- 把 `k2` 二进制安装到 `/usr/local/bin/k2`
- 写入 `/etc/systemd/system/kaitu.service` 并 `enable --now`
- 如果检测到图形环境，自动用 `xdg-open` 打开 `http://127.0.0.1:1777`

安装完成后，在浏览器中访问 `http://127.0.0.1:1777` 即可登录使用。

## 方式二：手动下载 tarball

如果您习惯手动安装：

1. 在浏览器中访问 [kaitu.io/install](https://kaitu.io/install)，下载 `Kaitu_<版本号>_linux_amd64.tar.gz`
2. 打开终端，切换到下载目录
3. 解压并运行其中的 `install.sh`：
   ```
   tar xzf Kaitu_*.tar.gz
   cd Kaitu_*  # 或直接进入解压出的目录
   sudo ./install.sh
   ```
4. 安装完成后，在浏览器中访问 `http://127.0.0.1:1777`

tarball 内容说明：

| 文件 | 用途 |
|------|------|
| `k2` | 守护进程二进制，包含 VPN 核心 + 嵌入式 Web UI |
| `install.sh` | 安装脚本（支持升级，会先停旧服务再覆盖） |
| `uninstall.sh` | 卸载脚本 |
| `kaitu.service` | systemd unit 文件 |

## 首次使用

1. 浏览器访问 `http://127.0.0.1:1777`
2. 使用您的账号登录（手机号验证码或邮箱）
3. 选择节点并连接

**重要**：关闭浏览器不会断开 VPN。守护进程由 systemd 管理，会在后台持续运行。若要真正停止 VPN，请运行 `sudo systemctl stop kaitu`。

## 常用命令

| 操作 | 命令 |
|------|------|
| 查看服务状态 | `sudo systemctl status kaitu` |
| 停止服务 | `sudo systemctl stop kaitu` |
| 启动服务 | `sudo systemctl start kaitu` |
| 重启服务 | `sudo systemctl restart kaitu` |
| 查看日志 | `sudo journalctl -u kaitu -f` |
| 开机自启（默认已启用） | `sudo systemctl enable kaitu` |
| 禁用开机自启 | `sudo systemctl disable kaitu` |

## 自动更新

开途 Linux 版支持从 Web UI 内触发自动更新：

1. 浏览器中访问 `http://127.0.0.1:1777`，进入设置页
2. 点击「检查更新」
3. 如有新版本，点击「下载并安装」
4. 守护进程会下载新版 `k2` 二进制、校验 SHA-256、原子替换，然后调用 `systemctl restart kaitu`
5. 几秒钟后刷新浏览器即可看到新版本

升级过程中 VPN 会短暂中断（systemd 重启），登录状态会保留（`/etc/kaitu/storage.json` 不会被清空）。

## 卸载

tarball 里已经提供 `uninstall.sh`：

```
cd <tarball 解压目录>
sudo ./uninstall.sh
```

默认会保留 `/etc/kaitu`（登录状态和加密存储）。如需同时清除所有数据，追加 `--purge`：

```
sudo ./uninstall.sh --purge
```

若已经丢失 tarball，也可以手动卸载：

```
sudo systemctl disable --now kaitu
sudo rm /etc/systemd/system/kaitu.service
sudo rm /usr/local/bin/k2
sudo systemctl daemon-reload
# 可选：清除数据
sudo rm -rf /etc/kaitu
```

## 常见问题

**Q：浏览器访问 127.0.0.1:1777 显示「无法连接」？**

A：检查 systemd 服务状态：
```
sudo systemctl status kaitu
sudo journalctl -u kaitu -n 50
```

常见原因：
- 服务未启动 → `sudo systemctl start kaitu`
- 端口被占用 → 检查是否有残留旧版本 `k2.service`（v0.4.2 前的 Tauri 时代命名）
- 守护进程崩溃 → 查看 journal 报错，一般 systemd 会自动重启

**Q：从老版本（v0.4.1 或更早）升级时浏览器打不开？**

A：老版本可能遗留了 `/opt/kaitu/` 目录和 `k2.service`（非 `kaitu.service`）。一键脚本会自动清理，但手动安装的用户需要先：
```
sudo systemctl stop k2 2>/dev/null || true
sudo systemctl disable k2 2>/dev/null || true
sudo rm -f /etc/systemd/system/k2.service
sudo rm -rf /opt/kaitu
sudo systemctl daemon-reload
```
然后再重新运行 install.sh。

**Q：我在无图形环境的服务器上安装，`xdg-open` 报错？**

A：不影响使用。脚本把 `xdg-open` 放在 best-effort 容错里，失败不会让安装失败。服务器上直接在本地或远程浏览器访问对应机器的 `http://<服务器IP>:1777` 即可（注意 1777 默认只绑定 127.0.0.1，如需远程访问需要 SSH 端口转发：`ssh -L 1777:127.0.0.1:1777 user@server`）。

**Q：如何完全停掉 VPN？**

A：`sudo systemctl stop kaitu`。只关浏览器不会停守护进程。
