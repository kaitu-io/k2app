# Linux 安装指南

## 系统要求

- 支持 Ubuntu、Fedora、Arch Linux 等主流 Linux 发行版
- 仅支持 amd64（x86_64）架构
- 需要系统已安装 webkit2gtk-4.1（图形界面依赖，一键脚本会自动安装）

## 方式一：一键脚本安装（推荐）

这是最简单的安装方式，脚本会自动完成下载、安装依赖和配置。

打开终端，运行以下命令：

```
curl -fsSL https://kaitu.io/i/k2 | sudo bash
```

脚本会自动完成以下操作：
- 检测您的系统环境
- 安装所需的依赖（如 webkit2gtk-4.1）
- 下载并安装最新版开途
- 配置系统服务

安装完成后，您可以在应用菜单中找到开途，或在终端中运行 `kaitu` 启动。

## 方式二：手动下载 AppImage

如果您更习惯手动安装，可以下载 AppImage 文件：

1. 在浏览器中访问 [kaitu.io/install](https://kaitu.io/install)
2. 下载 `Kaitu_版本号_amd64.AppImage` 文件
3. 打开终端，进入下载目录，赋予文件执行权限：
   ```
   chmod +x Kaitu_*.AppImage
   ```
4. 双击运行，或在终端中执行：
   ```
   ./Kaitu_*.AppImage
   ```

## 首次运行授权

首次运行开途时，需要授权安装系统服务：

- **有图形密码弹窗的系统**：系统会自动弹出 pkexec 授权窗口，输入您的用户密码即可
- **没有 pkexec 的系统**：需要在终端中手动运行以下命令来安装系统服务：
  ```
  sudo k2 service install
  ```

完成授权后，使用您的账号登录，即可开始使用。

## 自动更新

开途 Linux 版（AppImage 格式）支持自动更新。当有新版本发布时，应用会自动提示您更新，无需手动重新下载。
