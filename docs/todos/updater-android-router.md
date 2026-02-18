设计 Android 侧载分发 + 路由器固件的自更新机制（IUpdater 扩展）。
当前 updater 仅 Tauri 桌面端实现（tauri-plugin-updater），Android 和 OpenWrt 路由器缺少更新方案。
Android: 非 Play Store 分发需要 APK 下载+安装流程。
Router: 固件 OTA 更新机制。
