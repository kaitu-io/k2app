# v0.4.8 发布验证记录

> 这是一份**账本**，不是计划：每一行都带证据，能复现。目标是把发布信心从
> 4.5/10 一阶阶抬到 10/10，而 10/10 只在灰度遥测干净之后才成立——不在发版前。
> 更新规则：验证过的事实进「已验证」，带命令/运行号/数字；只读代码推出来的
> 结论不进「已验证」。

## 0. 候选与基线

| 项 | 值 |
|---|---|
| 上一发布 | v0.4.7（gitlink `e6bab379`），最后一次全平台产物 2026-08-02 `757467a5` |
| 候选 (RC) | `bb44c18b` = 本地 main `9f5c7940`（14 个未推送提交，钱路）⊕ origin/main `1c1dfe4a`；合并基 `f1847ab7`；干净合并，无冲突 |
| RC 分支 | `rc/0.4.8`（origin） |
| 门分支 | `ci/close-verification-gates`（基于 RC，本文所在提交） |
| k2 gitlink | `4f85812`（并集自动取新侧；节点车队 k2s 已是同世代 `6bc70b02`/`ef6c0b2f`，晚于 kuic 迁移与 auth 合并） |
| 版本源 | root `package.json` = `0.4.8` |

## 1. 已验证（带证据）

### 1.1 候选代码自身

| 项 | 结果 | 证据 |
|---|---|---|
| 并集合并 | 干净 | `git merge` 无冲突；`ci.yml +422`、`installer-hooks.nsh +23`、`installed_apps.rs +206`、gitlink 自动解到 `4f85812` |
| webapp `tsc --noEmit` | 干净 | 本机 exit 0（先前一次 TS2339 是本机 `node_modules/k2-plugin` 陈旧副本，变异验证 red→green 归因，见 §2.4） |
| webapp 全量 vitest | **126 文件 / 1384 通过 / 9 跳过 / 0 失败** | 本机 load 55 下 517s（正常膨胀，非回归） |
| api mock 套件（无库） | 10 次重复：9 绿 / 1 次未归因失败 | 失败发生在 load ≈32，`--- FAIL` 行未被捕获，**不作为发现**；CI `test-macos` 复跑观察 |
| **api 集成套件（真库）** | **空库 migrate 成功 → `ok` ×2 包 / 顶层 PASS 687 / config-skip 0 / exit 0** | `scripts/ci/api-db-test.sh` 本机对空库 `kaitu_ci`（MariaDB 10.6.28）干跑，38.7s |
| web message 既有测试 | 2424 通过 | `messages-integrity` 2219 + `homepage-content` 57 + 新 `messages-parity` 148 |
| OSV | 残留 = 记录在案（image-size ×2 无修复版 + esbuild 2.5 低危） | 与 `project_osv_accepted_residual_vulns` 一致，非新增 |

### 1.2 线上/车队侧（只读观测）

| 项 | 结果 |
|---|---|
| 节点 k2s 世代 | 三节点 gitlink 晚于 kuic 迁移与 k2v5 auth 合并 → 客户端/服务端 wire 不匹配风险≈0 |
| `enforce_auth` | 三节点均 `false`（permissive）→ 客户端 metadata 缺陷降级为"照常服务"，不是断连 |
| `DIAG: auth-rollout` | 842 连接，`authed=0`，全部 `legacy_no_metadata` → **k2v5 设备认证零生产曝光**，首次真实运行=发版当天 |
| Windows 品牌分叉 | 既有 kaitu 构建 `k2app.exe`：`strings` 命中 overleap 模式 **0** / kaitu 模式 4 → `cfg(brand_overleap)` 编译期分叉在 Windows 上成立（单点数据，Makefile 门每次构建重验，见 §2.6） |
| `webapp-support-floor.json` | `native: 0.4.8` → 已发布的 0.4.7 移动壳被 web OTA 门挡在外面（0.4.7 壳不验 minisign、不看 min_bridge，这是唯一防线，已核实由 CI 从契约推导） |

### 1.3 查过、确认不是问题

OSV 红 · web i18n 11 处差异全是死键（现已删除，见 §2.3）· tessera 零依赖 · `installer-hooks.overleap.nsh` 正确 gitignore+生成 · `overleap` 分支完全被 main 包含 · CDN 无 0.4.8 占位 · #3051 已在 v0.4.7 发过。

## 2. 本次关掉的门（每道都做过变异验证：改坏必红，改回必绿）

| # | 门 | 之前 | 现在 | 变异证据 |
|---|---|---|---|---|
| 2.1 | webapp ⇄ `k2/engine/error.go` 错误码 | `describe.skipIf(!available)`，CI 从不初始化 k2 → **从未执行过** | fail-closed；`test-webapp-reusable.yml` 初始化 k2，三个调用方 `secrets: inherit` | k2 在：3/3 绿；`Timeout=408`：2 失败（"expected 408 to be 108"）；文件缺失：`Failed Suites 1` |
| 2.2 | api 集成测试进 CI | `center/config.yml` gitignored → 256 个 `skipIfNoConfig` 测试 CI 全跳 | 新 job `test-api-db`：MariaDB 10.6 service + 真 `migrate` + **0 config-skip 判别 + 顶层 PASS 下限 620** | 本机空库干跑 687/0/exit 0；`migrate.go` legacy purge 加表+列存在守卫，否则空库 1146 直接炸（`Migrate()` 还被 17 个测试点调用，AutoMigrate 之后的库没有 `deleted_at` 列，守卫是必需的） |
| 2.3 | web messages 跨 locale key 平价 | 无门 | `web/tests/messages-parity.test.ts`（148 用例） | 写门先红：8 文件/11 键；删死键后绿；独立检查器 `i18ncheck.mjs` 0 差异 |
| 2.4 | k2-plugin 新鲜度 | `file:` 依赖 yarn 拷贝不刷新，本机副本 6 天陈旧、缺 `confirmWebBootOk` | `scripts/check-k2-plugin-fresh.sh`：dist == tsc(src)（CI）+ node_modules 副本 == 插件目录（webapp `pretest`） | 绿；dist 追加一行 → 红并指出修法 |
| 2.5 | 子模块初始化 | 无脚本初始化 k2 / 嵌套 kuic，新机器 `go build` 必炸 | Makefile `init-submodules`（哨兵文件守卫，不碰已填充的 checkout）挂进 `pre-build` | `make -n` 语法通过 |
| 2.6 | Windows 品牌纯净（本地） | 只在 `release-desktop.yml`，`make build-windows` 不跑；且脚本 `! -name 'k2*'` 把 `k2app.exe`（应用本体）也排除了；NSIS 安装包是 LZMA 压缩，`strings` 只看到 stub | `make build-windows` 内置：暂存 `k2app.exe` + 安装包做 strings 门；排除项改为精确 sidecar 名；CI 步改品牌精确文件（自建 runner 持久化 `release/` 会攒两品牌产物，目录扫描会误报） | 待 dry-run 构建验证（§3） |
| 2.7 | 发布工作流 dry-run | 任何 `v*` tag = 上 S3 + Slack + **自动发布 web OTA stable**；没有"只出产物不发布"的路径 | `release-desktop.yml` / `build-mobile.yml` 增加 `workflow_dispatch` `dry_run`（不上 S3/ASC、不发 Slack、产物挂 run）与 `target`（分支构建） | actionlint 4 文件 0 报错；待 dry-run 实跑验证 |

工具自检：`actionlint` 1.7.x 本机 0 报错（与 CI 同版本系）；`shellcheck` 3 个脚本 0 报错。

## 3. 进行中 / 待办

| 阶段 | 项 | 状态 |
|---|---|---|
| 0 | `rc/0.4.8` 推上 origin | ✅ 2026-08-27 |
| 0 | PR：`rc/0.4.8` → main（旧门下的 CI 基线） | 见 PR 列表 |
| 1 | PR：`ci/close-verification-gates` → main（新门首跑，含 `test-api-db`） | 见 PR 列表 |
| 2 | desktop dry-run：`gh workflow run release-desktop.yml --ref ci/close-verification-gates -f target=desktop -f dry_run=true` | 待 PR CI 绿后触发；Windows 腿依赖 SimplySign 会话（§5） |
| 2 | mobile dry-run：`gh workflow run build-mobile.yml --ref ci/close-verification-gates -f platform=both -f dry_run=true` | 同上 |
| 3 | 真机项（§4） | 需要人 |
| 4 | 灰度遥测 | 发版后 |

## 4. 需要真机 / 人（不能桌面验证的）

1. **0.4.7 → 0.4.8 升级路径**，五平台各一次（不是全新安装）。桌面重点：`kaitu-ui://` 源迁移一屏（`?migrate=export` → 导出 → 导航），20s watchdog 不触发。
2. 每平台连一次真节点：新 QUIC 腿（kuic + Chrome-146 指纹 + 固定 443）握手成功；节点 `DIAG: auth-rollout` 出现 `authed>0`。
3. Windows：UAC 拒绝分支（sentinel 997）、启动黑框闪现。
4. Web OTA：先 `publish-web-ota.yml` dispatch 到 `namespace` UAT，配 `K2_WEB_OTA_BASE` 走通全链，再碰生产 manifest。
5. 通道选择器回滚阀 `K2_CHANNEL_SELECTOR_LEGACY` 在桌面/移动壳**拧不动**（`os.Getenv` 读一次、无壳透出）——灰度前决定是否补一个可拧的入口。

## 5. 待决策（与 app 发版解耦，但时间敏感）

- **au-1**（`au-sydney.aws.wm04`）：2026-08-27 15:30 +07 读数 **999.70 / 1024 GB = 97.6%**，周期 84.5%（09-01 重置）。按本周期均速 ≈38 GB/天，**约 0.6 天触 100%**。`cloud_instance.aws_overage_autostop` 默认开 → `StopInstance`。Center 是否已部署 `worker_cloud_overage.go` 未能从 API 判断（响应不含相关字段）。选项：提前关阀 / 接受 AU 停机到重置 / 迁流量。
- **SimplySign**：云会话 2–3 小时掉一次，runner LaunchAgent `SessionCreate=true` 结构上无法自愈；`build-windows`（含 dry-run 的 Windows 腿）依赖它。选项：GUI session 加周期 LaunchAgent 跑 `simplisign-login.sh` / 发版前手动跑。

## 6. 信心刻度（诚实版）

| 时点 | 代码信心 | 发布信心 | 靠什么 |
|---|---|---|---|
| 首次 review（2026-08-26） | 8.5 | 4.5 | 桌面观测 |
| 本文提交时 | 9.0 | 5.0 | §1 + §2 的变异验证；CI 尚未在 RC 上跑过 |
| 阶段 1 PR 全绿 | 9.5 | 5.5 | 新门在 CI 真跑 |
| 阶段 2 五平台 dry-run 产物 | 9.5 | 6.5 | "能构建"从未知变已知 |
| 阶段 3 真机 | 9.5 | 8 | 升级路径 + 握手 + authed>0 |
| 阶段 4 灰度 72h 干净 | 10 | 9.5 | 遥测 |
| 全量后一个完整计费周期 | 10 | 10 | 无新工单模式 + 计量新口径过 09-01 |
