# Desktop brand notes — release tags, purity guard, icons, window shape, signing description, log prefixes

Moved verbatim from `desktop/CLAUDE.md` (2026-11-14) so the per-session doc stays small. **Read this before changing `scripts/build-macos.sh` artifact collection, `scripts/check-desktop-brand-purity.sh`, `scripts/publish-desktop.sh` release tags, brand icons, `window.rs` / `tauri.conf.overleap.json` window constants, `scripts/ci/sign-description.sh` / Windows signing scripts, or `log_upload.rs` brand prefixes / predicates** — each paragraph records the incident or contract behind the rule.

## Brand (双品牌: kaitu / overleap)

Build-time 烘焙，与 webapp 同一契约：`BRAND=overleap make build-macos`（Makefile
`BRAND ?= kaitu` → `export K2_BRAND`）。三层生效：webapp dist（`__K2_BRAND__`）、
Rust `cfg(brand_overleap)`（build.rs 发 cfg；updater 端点（channel.rs）/ 桌面日志目录（main.rs）/
updater 回退路径（updater.rs）编译期分叉——另一品牌 URL 不进二进制）、Tauri
`--config src-tauri/tauri.conf.overleap.json`（productName/identifier/icon/entitlements/
updater endpoints；**合并时数组整体替换，overlay 里数组字段必须写全量**）。
- 产物命名 `Overleap_{VERSION}_{ARCH}.{EXT}`；S3/CDN 路径段 `/overleap/desktop/`；
  latest.json 双份、独立 GitHub Release tag `overleap-v{VERSION}`
  （`scripts/publish-desktop.sh --brand=overleap`）。kaitu 自 0.4.13 起同理用
  `kaitu-v{VERSION}`：Release tag **绝不能以 `v` 开头**——`gh release create` 会推这个
  tag，`v*` 触发两品牌全平台重建并覆盖刚签进 latest.json 的 CDN 产物（0.4.12 踩过，及时取消）。
- **k2 daemon 品牌中立**：1777 端口、launchd label `kaitu`（`k2/cmd/k2/service_darwin.go`）、
  NSIS `SERVICE_NAME "kaitu"` 不随品牌变。两品牌同机共存 = last-install-wins 接管
  daemon，双方 app 都能控制 VPN。
- 纯度守卫 `scripts/check-desktop-brand-purity.sh <brand> <path>`：webapp 资源 +
  二进制 strings（k2 sidecar 与裸 `kaitu` 内部 token——`kaitu-icon://` scheme、HKDF
  盐、S3 桶、上面的 service name——豁免）；`.app.tar.gz` 目标额外按**内容**（`tar tzf`
  顶层 `.app` 目录名）而非文件名校验——两品牌共享同一 Tauri bundle 目录，naive
  `find *.app.tar.gz | head -1` 式收集会按字母序（Kaitu < Overleap）拿到上一次 kaitu
  构建残留的 tar.gz，产物文件名对但内容是另一品牌（曾实际发生，见 commit
  `813bf3f5`）。`build-macos.sh` 现在按品牌精确路径收集，且收集后立即跑这道门。webapp payload
  被 brotli（macOS 二进制）/ LZMA（NSIS）压缩后对 `strings` 不可见——webapp 品牌纯度真正的门是
  打包前的 `webapp/scripts/check-brand-purity.sh` dist 检查。
- 脚本里调 `make` 必须显式带 `BRAND=$BRAND`（递归 make 只透传 `K2_BRAND`）——见根 CLAUDE.md Cross-Layer。
- `desktop/src-tauri/installer-hooks.overleap.nsh` 是构建期由 Makefile 对
  `installer-hooks.nsh` 做 sed 替换（`io.kaitu.desktop` → `io.overleap.desktop`）生成的
  派生副本——仅当 `BRAND=overleap` 才生成，gitignored，永不手改。
- 签名主体（Developer ID / SimplySign / notarize 账号）与 updater minisign 密钥两品牌
  共用——已拍板的品牌泄漏点，不再讨论。
- kaitu 图标由 `webapp/brand-assets/kaitu/generate.sh` 从 `logo.svg`（≤32px 用 `logo-small.svg`）生成进 `icons/`；`.icns` 用带透明边距的 824/1024 版，其余为满幅圆角块。
- overleap 图标由 `webapp/brand-assets/overleap/generate.sh` 从 `logo.svg` 生成进 `icons-overleap/`
  （不要用 `yarn tauri icon`，它只认 PNG 源且会覆盖 `.icns/.ico` 的生成方式）；NSIS 语言 overleap 仅 English。
- 图像资产（logo PNG 等二进制）不在桌面纯度守卫覆盖范围内——`strings`/`grep` 抓不到位图里的文字；
  桌面侧靠发布前人工视觉 smoke 把关（原 `web/tests/brand-assets.test.ts` 哈希守卫随 overleap 站迁出 `web/` 已删除）。
- **窗口形态按品牌分叉**：kaitu 是竖版手机比例（`tauri.conf.json` 430×956、max 480，`window.rs` 9:20 锁比）；
  overleap 是横向桌面窗口（`tauri.conf.overleap.json` 1040×700、min 880×620、可最大化，`window.rs`
  `cfg(brand_overleap)` 分支按工作区 85% 夹紧、不锁比）。webapp 侧栏布局靠短边 ≥ 600 触发，所以
  overleap 的 minHeight 620 是布局契约，不是随手值。两套常量各有测试模块，`K2_BRAND=overleap cargo test`
  必须与默认 `cargo test` 各跑一次——`ci.yml` 的三个 Rust 测试 job（`test-linux-rust` / `test-macos` /
  `test-windows`）现在各自紧跟默认跑一步 `K2_BRAND=overleap cargo test`。
- **Windows Authenticode 描述随品牌**：`scripts/ci/sign-description.sh` 读 `K2_BRAND`（Makefile `export`
  → Tauri `bundle.windows.signCommand` 进程环境）→ `Overleap Desktop` / `Kaitu Desktop`，供
  `scripts/ci/macos/windows-sign.sh` 的 osslsigncode `-n`；`scripts/ci/windows/sign-binary.ps1` 用同一 env
  分叉 signtool `/d`（`Overleap` / `Kaitu`）。`src-tauri/windows-sign.sh` 的 journal 多记一行 `brand=`，
  dry-run（`K2_SIGN_LOG=… SKIP_WINDOWS_SIGNING=true`）即可证明 env 到达了签名器。测试：
  `bash scripts/test-sign-description.sh`。证书主体（Wordgate LLC）两品牌共用，见上一条。
- **日志上传按品牌分前缀**（`log_upload.rs`，前缀值见 "S3 Log Upload"）：`MACOS_LOG_PREDICATE` = `process CONTAINS "kaitu"` /
  `process CONTAINS "k2app"`。**两品牌的主二进制都叫 `k2app`**（crate 名；两份 tauri.conf 都没设
  `mainBinaryName`，Tauri v2 不会改名，已建 `Kaitu.app` 的 `CFBundleExecutable = k2app`）——所以 overleap
  谓词写进程实名，`"Overleap"` 匹配不到任何进程；kaitu 谓词按字节保留。`brand_prefix_tests` 每个 cfg
  各钉一条字面量。同一桶（`kaitu-service-logs`）两品牌共用，靠前缀区分。
