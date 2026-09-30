OpenWrt（k2r）自动更新：补齐「无人值守」的两块

已完成（2026-10-01 核实）：手动升级链路已经有了——`k2r upgrade [--check]`（`k2/cmd/k2r/upgrade.go`）和网关面板（`k2/gateway/gateway.go` `newK2rUpgrader`）共用 `webui.Upgrader`：CDN manifest 检查 → 下载 → SHA256 校验 → 原子替换 → procd 重启。

剩余：
1. 定时自动检查 + 更新窗口（如凌晨 2–4 点），默认开关待定
2. 回滚：新版本启动失败 / 起来后隧道起不来时恢复旧二进制（替换前保留备份）
3. 重启后配置与连接状态保持的验证（真路由器上测，`k2/test/k2r-lifecycle/router-check.sh` 加步骤）
