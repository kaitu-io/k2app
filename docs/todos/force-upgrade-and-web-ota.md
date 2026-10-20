# 强制升级旧版 + 正式启用 Web OTA

## 优先级: P1

## 背景（2026-10-20）

- **流量统计分界 = 0.4.9**：≤0.4.7 客户端不发 metadata 流（k2 `8fc4e12` / `c368a1d` 之后才发），流量全部落「未识别」桶；0.4.8 从未发布。metadata 由原生 Go 引擎发送，web OTA 救不了这批版本。
- **已上线**：Center 共享配置 `/data/app-configs/kaitu/config.yml` 加了 `frontend_config.min_client_version: "0.4.9"`，两台已重启；kaitu 公告 #6（`max_version=0.4.7`）已激活。
- **已真机验证**（PVE 安卓，线上 CDN APK 全新安装）：0.4.1 / 0.4.4 / 0.4.5 / 0.4.7 弹出不可关闭的「版本过低」对话框，「立即更新」打开 kaitu.io/install 并自动下载最新 APK；0.4.9 / 0.4.11 / 0.4.13 不弹。
- **Web OTA 现状**：两品牌 `web/latest.json`（stable + beta）因 OTA 缺陷被有意删除，出处 `latest-source.json` 停在 `0.4.11.22325782 @ eb922361`。最低原生版本门 `contracts/webapp-support-floor.json` 维持 0.4.8 不变（0.4.8 未发布，效果等同 0.4.9）。

## 行动清单

### 强制升级
- [ ] **约 2026-11-03**：看 k2s 日志 `DIAG: auth-rollout ... legacy_no_metadata` 占比；降下来后先一台节点开 `K2_ENFORCE_AUTH=1`，再分批。回滚要靠 sidecar 重新生成 yaml，只改 env 不够。老客户端被拒时的症状是「显示已连接但上不了网」。
- [ ] `ForceUpgradeDialog` 缺 `setOpen(false)`：最低版本调低后，本次会话里对话框也不会关闭。另外 k2r 路由器 webui 也渲染该对话框，11-11 路由器版发售前确认是否要排除 gateway。

### Web OTA 补洞（改移动端插件，需 worktree，随下一个原生版本生效）
- [ ] 移动端原生升级后清掉旧磁盘包：桌面已有 `discard_stale_disk_ui`，iOS / Android 没有。疑似 iOS 0.4.12 / 0.4.13 用户仍在跑 9 月的旧 OTA webapp，需真机核实。
- [ ] 移动端 minisign 签名改为强制：现在是「manifest 有 sig 才验」（`K2Plugin.swift` / `K2Plugin.kt`）。
- [ ] 顺带修：iOS `resolveDownloadURL` 强制解包可能崩溃；Android 自动路径解压失败后留下半成品目录；包里缺 `index.html` 时被清掉但不隔离，导致反复下载；桌面 gate 跳过原因只打 debug 日志。
- [ ] Android JVM 单测（AutoUpdatePlan / WebBootDecision / Minisign）与 iOS K2Tests 接入 CI 和 labtest；K2Plugin 的 I/O 路径（load / scheduleWebBoot / 下载解压）目前零覆盖。

### Web OTA 真机验收（Android / iOS / macOS / Windows / Linux）
- [ ] 每个平台：装线上原生版 → 发 beta 或 namespace 包 → 冷启动 ×3，确认 applied → served from disk → confirmed，且无回滚。
- [ ] 坏包测试：第一次冷启动坏、第二次自动回滚，`quarantined-version.txt` 写入，不再重复下载；再发一个更新的好包能解除隔离。
- [ ] 额外用例：原生升级后跑的是哪个 UI、会话中途 chunk-reload 只重载一次、剥掉 sig（移动端预期仍接受，桌面 / Linux 预期拒绝）。
- [ ] 移动端坏包用改了 CDN 地址的 debug 包，别碰真实 beta 用户（beta 开关普通用户可见，且任何 stable 发布都会覆盖 beta）；验收期冻结 `webapp/*` tag。

### 首发
- [ ] 补洞与验收完成后，先推 `webapp/x.y.z-kaitu`，观察一天日志和工单，再发 overleap。
- [ ] 建议让客户端上报当前 web 版本，以便观测 OTA 覆盖率和回滚情况。

## 前置条件
- 首发依赖「补洞」中已进入新原生版本的修复，以及全部平台验收通过。
