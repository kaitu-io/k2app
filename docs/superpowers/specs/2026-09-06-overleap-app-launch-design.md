# Overleap App 上架设计（iOS App Store 非中国区 / Google Play / Windows / macOS / API 收尾）

日期：2026-09-06。前序：`2026-07-14-brand-split-design.md`（双品牌拆分）、`2026-09-04-overleap-independent-release-design.md`（第一波：网站 + 桌面代码）、`2026-09-04-overleap-site-decoupling-and-uk-positioning-design.md`（站点解耦 + Stripe 多币种）。本 spec 是第二波：把 Overleap 客户端真正送进两个商店、出桌面产物、把 API 代码收到"可部署"。

## 背景（2026-09-06 现状核实）

四层只读探查 + ASC / CDN / 生产 DB 核实的结论，每条都有代码或线上证据：

| 层 | 已就绪 | 缺口 |
|---|---|---|
| 桌面 | Tauri overlay、`cfg(brand_overleap)`、签名/公证、`v*-overleap` tag 触发、S3 上传、纯度门、网站下载页 null-safe | 从未发布过（`/overleap/desktop/` 403）；Windows Authenticode 描述写死 "Kaitu Desktop"；日志上传 S3 key 无品牌段、`log show` 谓词写死 kaitu；`K2_BRAND=overleap cargo test` 不在 CI；`build-linux` 无品牌守卫 |
| 移动 | 双品牌 flavor / xcconfig / keystore / 纯度门 / 构建脚本对称；`IosMembershipPanel` 的 stripe_portal 分派已修 | **无 AAB**；iOS AppIcon 是 7 月旧稿、Android 老式图标是纯色方块、两端启动页都是 K2 标；`CFBundleDevelopmentRegion=zh-CN`；无出口合规声明；`Overleap.storekit` 价格 49 且未挂 app id；移动 CI 只有 `OVERLEAP_MOBILE_CI` 一个总开关，翻了会连 kaitu 一起构建并推 TestFlight；ASC 上传写死 kaitu；`OVERLEAP_APPSTORE_URL` 未设 |
| API | Stripe / Apple IAP / 节点可见性 / 契约门 / 六套英文邮件模板 | 系统邮件发件人不分品牌；设备踢出、工单回复通知给 Overleap 用户发中文开途文案；后台节点列表读不到 `brands` / `visibleOverleap`；antiblock seed 不按品牌过滤；**无 Google Play Billing** |
| ASC | 记录 6759199298 "Overleap VPN" / `io.overleap`；两个 bundle id 已开 NE + App Groups + IAP；订阅组 22253843 + 商品 `io.overleap.sub.basic.1y`（6793223106） | 版本 4.0 的描述 / URL / 版权 / 截图全是开途内容；zh-Hans 本地化仍在；订阅 `MISSING_METADATA`（缺审核截图）；年龄分级、隐私标签、服务器通知 URL 未设；无任何 `io.overleap` 描述文件（自动签名会建）；无 build |
| 生产 | Stripe live 已接通、Plan 行 13/14 已建、`VisibleOverleap` 默认 true | **27 个共享节点零个声明 overleap** → Overleap 用户和审核员看到空列表；`appstore.bundleIds.overleap` 未配；`support@overleap.io` 无发件域 |
| Play | — | 无开发者账号、无应用、无 AAB、无商店资产 |

## 已确认的决策（2026-09-06，用户拍板）

1. **Android 首发不带应用内购买**。登录即用；Play 版隐藏一切购买入口，不出现购买链接、价格、"去网站订阅"引导。Google Play Billing 作独立一期。
2. **Google Play 组织账号，主体 Wordgate LLC**（与 Stripe 同一实体）。对外 developer name 用 "Overleap"。
3. **macOS 只出 pkg**。daemon 安装依赖 pre/postinstall，dmg 做不到；与 kaitu 一致。
4. **移动 CI 改成与桌面同款 tag 解耦**：`-overleap-ios` / `-overleap-android` / `-overleap-mobile` 只构建 Overleap。
5. **iOS 只上一年期订阅** `io.overleap.sub.basic.1y`（$79 / £79 / €89，Apple 侧另有"年费按月付"选项）。月付留网站。
6. **继续用 ALL NATION CONNECT 的 Apple 开发者账号与共用签名证书**（卖家名会显示 ALL NATION CONNECT，用户接受）。
7. **桌面发布沿用 kaitu 的手动 `publish-desktop`**，不改 CI。
8. **Overleap Android 只走 Play**：不发 APK 到 CDN、关闭 APK 自更新（Play 禁止应用自行下载安装包）。Web OTA 保留（JS 资源更新 Play 允许）。
9. API 代码合入 main，**不部署**；部署清单落 `docs/ops/overleap-api-deploy.md`。
10. 版本号沿用根 `package.json`（0.4.10）。Overleap iOS 的 MARKETING_VERSION 直接用语义版本 `0.4.10`，不套 kaitu 的 `4.x` 遗留前缀。

## §1 移动端

### 1.1 品牌资产：`generate.sh` 成为唯一来源

`webapp/brand-assets/overleap/generate.sh` 扩展为同时产出移动端资产（现只产 webapp / web / desktop）：

- iOS：`mobile/ios/App/App/brand/overleap/AppIcon.appiconset/AppIcon-512@2x.png`（1024，**无 alpha**，App Store 拒绝带透明通道的图标：`-background <bg> -alpha remove`），`mobile/ios/App/App/brand/overleap/Splash.imageset/splash-2732x2732{,-1,-2}.png`（母版 2732 方形，品牌底色，logomark 居中 30% 宽）。
- Android：`mobile/android/app/src/overleap/res/mipmap-{m,h,xh,xxh,xxxh}dpi/ic_launcher_foreground.png`（108dp 自适应画布 = 108/162/216/324/432 px，logomark 占安全区 66%，透明底）、`ic_launcher.png` / `ic_launcher_round.png`（48dp = 48/72/96/144/192 px；legacy 圆角方形 / 圆形遮罩，供 API 24-25）、`res/values/ic_launcher_background.xml`（品牌底色，覆盖 `main/` 的 `#0F0F13`）、`res/drawable-{port,land}-{m,h,xh,xxh,xxxh}dpi/splash.png`（沿用 Capacitor splash 尺寸表，与 `main/` 同名覆盖）。
- 底色取 `logo.svg` 的背景 rect fill，脚本用 `grep` 从 SVG 读，不硬编码第二份。
- 守卫：`mobile/__tests__` 没有 JS 测试基建，用 `scripts/check-mobile-brand-assets.sh`（新）做哈希核对：对 iOS AppIcon、Android xxxhdpi 三张、splash 各一张，断言与 `generate.sh` 从当前 `logo.svg` 现算的输出字节相同（脚本内 `generate.sh --stdout <target>` 模式）。挂进 `ci.yml` 的 `test-webapp` 之后的一个轻量 job（macOS runner，需要 qlmanage）。反向验证：把 AppIcon 换回 kaitu 图标必须红。
- `apply-ios-brand.sh` 增加 Splash.imageset 的 rsync（现只换 AppIcon）；kaitu 的 `brand/kaitu/Splash.imageset` 从现有 `Assets.xcassets/Splash.imageset` 原样复制过去，字节不变。

### 1.2 iOS 工程

- `App/Info.plist` 的 `CFBundleDevelopmentRegion` 改为 `$(K2_DEVELOPMENT_REGION)`；`brand-kaitu.xcconfig` 设 `zh-CN`，`brand-overleap.xcconfig` 设 `en`。kaitu 产物字节不变。
- `brand-overleap.xcconfig` 加 `INFOPLIST_KEY_ITSAppUsesNonExemptEncryption = NO`（Xcode 13+ 的 `INFOPLIST_KEY_*` 注入，不动共享 plist；kaitu 维持现状，继续在 ASC 手答）。依据：k2 只用标准算法（TLS / 标准对称加密），属豁免类。
- `scripts/build-mobile-ios.sh`：`MARKETING_VERSION` 按品牌——kaitu 保持 `4.${MINOR}.${PATCH}`，overleap 用 `${VERSION}` 原值。`BUILD_NUMBER` 公式两品牌共用（不同 app，各自单调即可）。`scripts/test-ios-build-number.sh` 补 overleap 断言。
- `Overleap.storekit`：`_applicationInternalID` = `6759199298`，`_developerTeamID` = `NJT954Q3RH`，`displayPrice` = `79.00`，`displayName` = `Overleap Basic`，`description` ≤ 55 字符与 ASC 一致。仍是本地测试配置，不进构建。
- `TARGETED_DEVICE_FAMILY` 保持 `1,2`（与 kaitu 一致，iPad 截图一并出）。

### 1.3 Android 工程

- **AAB**：`scripts/build-mobile-android.sh` 在 release 模式追加 `bundle${BRAND_PRODUCT}Release`，产物 `release/$VER/${BRAND_PRODUCT}-${VER}.aab`；`Makefile build-android` 同步。kaitu 也会多出一个 AAB 文件，不上传、不影响现有 APK 流程。
- **纯度门**：`scripts/check-mobile-brand-purity.sh` 的 `case` 增加 `*.aab`（zip 解包，`base/dex/`、`base/res/`、`base/assets/`）。CI 对 overleap 的 AAB 跑一次。
- **versionCode**：`scripts/sync-version.sh` 改为 `MAJOR*1000000 + MINOR*10000 + PATCH*100 + REV`，`REV=${ANDROID_BUILD_REV:-0}`（0-99）。0.4.10 → 41000（> 现 410，kaitu 侧照常升级）。CI `check-versions` 只核 `versionName`，不受影响；`build-mobile.yml` 传 `vars.ANDROID_BUILD_REV`（与 `IOS_BUILD_REV` 同款：重传同版才改，用完清零）。
- **APK 自更新关闭**：`brand.xml` 新增 `<bool name="k2_apk_updates">`——kaitu `true`、overleap `false`。`K2Plugin.performAutoUpdateCheck` 在 `false` 时 `nativeManifest = { null }`（web OTA 车道照常）；`checkNativeUpdate` 直接 `available=false`。`K2PluginUtils.brandBool()` 与 `brandString()` 同款 `require(id != 0)`。
- **不发 CDN**：`scripts/publish-mobile.sh` 对 `overleap` + android 打印 `Play-only, skipping android manifest` 并 `exit 0`（与现有 iOS 未上架的处理同形）；`build-mobile.yml` 的 S3 上传步骤对 overleap android 跳过，AAB + APK 只作 workflow artifact。网站下载页在 `storeLinks.android` 填入 Play URL 前保持 "Coming soon"。
- `mobile/android/app/src/main/assets/capacitor.config.json` 是构建残留（gitignored），不动。

### 1.4 webapp：Play 合规门

新增品牌开关 `features.androidPurchase`（kaitu `true`：APK 渠道可跳 WordGate；overleap `false`）与 `storeUrls: { ios: string; android: string }`（kaitu `ios` = `https://apps.apple.com/app/id6448744655`、`android` 空；overleap `ios` 空（上架后回填）、`android` = `https://play.google.com/store/apps/details?id=io.overleap`）。

新增 `webapp/src/utils/purchase-surface.ts`：

```ts
/** 本平台是否允许展示任何购买/订阅管理入口。
 *  iOS 无 IAP 桥 → 否（3.1.1）；Android 且品牌关闭 androidPurchase → 否（Play 支付政策）。 */
export function purchaseSurfaceAvailable(): boolean {
  const os = window._platform?.os;
  if (os === 'ios' && !window._platform?.iap) return false;
  if (os === 'android' && !brandConfig.features.androidPurchase) return false;
  return true;
}
```

五处既有 `os === 'ios' && !iap` 判断（`App.tsx:69`、`SideNavigation.tsx:146`、`BottomNavigation.tsx:169`、`CloudTunnelList.tsx:296`、`Account.tsx:406/571`）统一改调用该函数——kaitu 行为逐字等价（有测试锁：kaitu 品牌下三平台各断言一次）。Android + overleap 下：`/purchase` 路由不注册、导航项过滤、续费 CTA 隐藏；`Account` 的会员区改渲染 `SubscriptionStatusOnly`（新，只显示到期日与状态，无按钮无链接；文案 `account.subscription.managedElsewhere`："Your subscription is managed from your Overleap account."，七语言）。`StripePurchasePanel` / `SubscriptionManagePanel` 在该平台永不挂载。`ForceUpgradeDialog` 的下载链接：`storeUrls[os]` 非空则用之，否则维持 `/install`。

守卫：`K2_BRAND=overleap` 测试矩阵新增 `purchase-surface.android.test.tsx`——模拟 `_platform.os='android'`，断言 `Account` 渲染 0 个 `openExternal` 调用、0 个 `/purchase` 导航、页面文本不含 `$`/`£`/`€`/`Subscribe`/`Stripe`；反向验证：把 `androidPurchase` 临时改 `true` 必须红。

### 1.5 移动 CI：品牌与平台从 tag 派生

`build-mobile.yml` 新增 `plan` job（与 `release-desktop.yml:109-119` 同形），输出 `ios_brands` / `android_brands` 两个 JSON 数组：

| ref / dispatch | iOS | Android |
|---|---|---|
| `v*`（裸） | kaitu（+overleap 当 `vars.OVERLEAP_MOBILE_CI == 'true'`） | 同左 |
| `v*-ios` / `v*-android` | kaitu | kaitu |
| `v*-overleap-ios` | overleap | — |
| `v*-overleap-android` | — | overleap |
| `v*-overleap-mobile` | overleap | overleap |
| dispatch `platform` + 新输入 `brand`（kaitu / overleap / both，默认 kaitu） | 按输入 | 按输入 |

`build-ios` / `build-android` 的 `if` 改为 `needs.plan.outputs.<x>_brands != '[]'`，matrix 用 `fromJSON`。`OVERLEAP_MOBILE_CI` 语义收窄为"裸 tag 是否顺带构建 overleap"，不再是 overleap 的唯一入口。`test-webapp` 的 `if` 同步认识新后缀。`publish-web-ota` 尾 job 维持 `-beta` 排除、加 `-overleap-*` 也跑（web OTA 恒双品牌，无害）。

ASC 上传步骤去掉 `matrix.brand == 'kaitu'`（同一 team、同一 API key，`altool` 按 IPA 的 bundle id 归档到对应 app）。Slack 成功消息：overleap iOS 写 "uploaded to TestFlight (Overleap VPN)"，overleap Android 写 "AAB artifact only (Play-only brand)"。

### 1.6 网站与 webapp 的商店链接回填（上架后）

`web/src/lib/brands.ts` `OVERLEAP.storeLinks`、`webapp` `storeUrls.ios`、`brand-overleap.xcconfig` `K2_APP_STORE_URL`、CI env `OVERLEAP_APPSTORE_URL` 四处在 App Store 链接产生后一次性回填（一个 commit）。Play URL 由包名确定，可提前写。

## §2 桌面与构建

- `scripts/ci/macos/windows-sign.sh` 的 `-n` 与 `scripts/ci/windows/sign-binary.ps1` 的 `/d` 改读 `K2_BRAND`（`overleap` → "Overleap Desktop" / "Overleap"，其余 → 现值）。`desktop/src-tauri/windows-sign.sh` 包装层透传 `K2_BRAND`（Tauri 调 signCommand 时环境已含 Makefile 的 export，plan 里实测确认）。
- `desktop/src-tauri/src/log_upload.rs`：新增 `const S3_PREFIX_DESKTOP` / `S3_PREFIX_AUTO` 按 `cfg(brand_overleap)` 取 `desktop-overleap` / `auto-overleap`，kaitu 维持 `desktop` / `auto` 字节不变；`log show` 谓词 overleap 用 `process CONTAINS "Overleap"`。两处各加 cfg 测试。
- `.github/workflows/ci.yml` 的 Rust 测试 job 追加一步 `K2_BRAND=overleap cargo test`（`desktop/src-tauri`），与 `desktop/CLAUDE.md` 的既定要求对齐。
- `Makefile build-linux` 首行加 `@[ "$(BRAND)" = kaitu ] || { echo "Linux desktop is kaitu-only"; exit 1; }`。
- ASC 注册 bundle id `io.overleap.desktop`（API，一次性），消除 `entitlements.overleap.plist` 指向未注册标识的公证风险。
- 桌面发布链（§4）不改代码。

## §3 API 收尾（合入 main，不部署）

### 3.1 系统邮件发件人按品牌

- 新增 viper 前缀 `mail_overleap`（与 `edm_overleap` 同形：`provider` / `send_from` / `smtp_*` 或 SES）。`logic_email.go` 新增 `systemSenderForBrand(b Brand) *mail.Sender`：overleap 且 `mail_overleap.send_from` 非空 → `mail.Config("mail_overleap")`；否则 nil = 走全局 `mail.Send`（fail-open + `log.Warnf` 一次性提示，保证 SES 域验证前注册仍能发码）。
- `sendSystemEmail(ctx, to, subject, body)` → `sendSystemEmailAs(ctx, b Brand, to, subject, body)`；`emailTo` / `emailToUser` 增加 `b Brand` 参数，`emailToUser` 可用 `brandOfUser`。全部调用点（验证码、登录提醒、转移、密码、踢出、工单）改传品牌。kaitu 路径：`b=BrandKaitu` → `MailSend` 原样，字节与行为不变（`TestKaituTemplateBytesUnchanged` 继续锁模板；新增 `TestSystemSenderForBrand` 锁选择逻辑）。
- `mail.dev_mode` 对两个 sender 一视同仁（`MailSend` 已在 sender 之前拦截）。

### 3.2 两处中文泄漏补英文

- `deviceKickTemplate` → `brandedDeviceKickTemplate`（Overleap 英文版："Your device {{.Remark}} was signed out"），`logic_auth.go:561` 改 `For(brandOfUser)`。
- `worker_ticket_notify.go`：主题 / 正文按 `Ticket` 的品牌选（`Ticket.Brand` 列若不存在则由 `ticket.UserID → brandOfUser`，匿名工单按 `ticket.Email` 反查 `LoginIdentify.brand`；都查不到回退 kaitu）。Overleap 版："[Overleap] New reply on your ticket (#N)" / "Open the Overleap app to view the full conversation."。
- `TestOverleapTemplatesNoChineseBrandLeak` 扩展覆盖这两个模板。

### 3.3 后台节点列表可读品牌字段

`AdminNodeItem` 增加 `brands []string`、`visibleKaitu bool`、`visibleOverleap bool`（`omitempty` 不用，布尔要可见）；`api_admin_list_nodes` 支持 `?brand=overleap` 过滤（`VisibleTo`）。`web/` 管理端节点表加两列（kaitu 专属页面 `page.kaitu.tsx`，不进 overleap 构建）。

### 3.4 antiblock seed 按品牌过滤

`api_antiblock.go` 候选节点加 `node.VisibleTo(BrandKaitu)`（seed 是 kaitu 反封锁通道；overleap 的 `antiblockCdnSources` 为空）。一行 + 一个测试。

### 3.5 不做

- Google Play Billing（决策 1）。
- 私有节点 / 代付 / 分销相关模板（数据门控，overleap 无对应套餐）。
- `min_client_version` 按品牌（无需求）。
- `edm_overleap` 生命周期模板（内容创作，独立排期）。

### 3.6 部署清单（`docs/ops/overleap-api-deploy.md`，新）

部署时逐条打钩：生产 `config.yml` 加 `appstore.bundleIds.overleap: io.overleap`、`mail_overleap.*`（SES，`send_from: support@overleap.io`）；两台 center 滚动重启 + `/version` 健康；`POST /api/user/apple-iap/verify` 用沙盒交易验一笔；`?brand=overleap` 节点列表非空。

## §4 发布链

### 4.1 桌面（可先行，不依赖商店）

1. S3 写探针：用 CI 同一 IAM 身份 `aws s3 cp` 一个 1 字节对象到 `s3://d0.all7.cc/overleap/desktop/.probe` 再删。
2. `workflow_dispatch release-desktop.yml target=overleap dry_run=true` → 核对签名、纯度门、`Overleap_0.4.10_{x64.exe,universal.pkg}` 命名、Authenticode 描述 = "Overleap Desktop"、pkg 公证已 staple。
3. 本机装 pkg 实测：窗口 1040×700、标题 Overleap、图标正确、与已装开途共存、`~/Library/Logs/overleap` 生成。
4. 打 `v0.4.10-overleap` → S3 `/overleap/desktop/0.4.10/`。
5. `BRAND=overleap make publish-desktop` → `cloudfront.latest.json` / `d0.latest.json` + GitHub Release `overleap-v0.4.10`。
6. `curl` 验 overleap.io/en-GB/install 两个下载 URL 200；自更新端点 200。

### 4.2 移动

1. 设 `vars.OVERLEAP_MOBILE_CI` 不动（保持未设），改用新 tag。
2. `workflow_dispatch build-mobile.yml platform=both brand=overleap dry_run=true` → IPA + AAB + APK artifact，纯度门过。
3. AAB 本机 `bundletool build-apks --connected-device` 装真机（Android）；IPA 装 iPhone 15（`xcrun devicectl`）真机 smoke：登录、取节点、连接、断开、与开途共存、Android 版无购买入口、iOS 版 IAP 面板显示 $79 沙盒商品。
4. 打 `v0.4.10-overleap-mobile` → iOS 自动上传 TestFlight；AAB 作 artifact 下载后手工上传 Play（首版必须手工：Play App Signing 首次登记上传密钥）。

## §5 商店

### 5.1 App Store Connect（记录 6759199298）

能用 API / asc MCP 做的我做；只有网页 UI 才能做的用 chrome MCP 在用户登录后做。

| 项 | 方式 | 内容 |
|---|---|---|
| 版本号 4.0 → `0.4.10` | API PATCH appStoreVersions | 与构建 MARKETING_VERSION 一致 |
| en-US 描述 / 关键词 / 支持 URL / 营销 URL / 版权 / 推广文本 | asc MCP `update_version_localization` | 隐私优先英文文案（源自 `web/messages/en-GB/landing.json` 的叙事，Overleap LLC 版权，URL `https://overleap.io/en-GB/support` / `https://overleap.io`） |
| zh-Hans 版本本地化与 App Info 本地化 | API DELETE | 非中国区上架，删 |
| App Info：name "Overleap VPN"、subtitle、privacyPolicyUrl `https://overleap.io/en-GB/privacy` | asc MCP `update_app_info_localization` | 现指向 kaitu.io/privacy，必改 |
| 类别 | API PATCH appInfos（primary `UTILITIES`，secondary `PRODUCTIVITY`） | |
| 年龄分级 | API PATCH ageRatingDeclarations 全 NONE | 4+ |
| 截图 | 模拟器 iPhone 16 Pro Max（6.9"）+ iPad Pro 13" 各 5 张：首页（已连接态用 dev 构建的假桥）、节点列表、账户、设置、订阅面板 | asc MCP `upload_screenshot` |
| 订阅 6793223106 审核截图 + 审核备注 | API `subscriptionAppStoreReviewScreenshots` | 状态 MISSING_METADATA → READY_TO_SUBMIT |
| 审核信息：演示账号 / 备注 | API PATCH appStoreReviewDetails | 演示账号由 §6 建 |
| 隐私标签（App Privacy） | **网页 UI**（无公开 API） | 收集：邮箱（账户）、设备 ID（分析/功能）、崩溃数据；不追踪；与 `PrivacyInfo.xcprivacy` 一致 |
| App Store 服务器通知 V2 URL | **网页 UI** | 生产 + 沙盒均 `https://k2.52j.me/webhook/appstore` |
| 出口合规 | `INFOPLIST_KEY_ITSAppUsesNonExemptEncryption = NO` | 每个 build 不再卡问 |
| 定价 / 供应 | 免费 app + 174 国（排除中国大陆） | 已设，核一次 |
| 提交审核 | 用户确认后我按下 | 首个订阅随版本一起提交 |

App Review 常见拒因预防：4.2 最小功能（VPN 主功能真实可用 → 节点必须有）、5.1.1 登录必须性（VPN 需账户，审核备注说明）、2.1 演示账号可登录、3.1.2 订阅条款（IAP 面板已含自动续订披露 / 恢复购买 / 条款隐私链接）、5.4 VPN 需用 NEVPNManager（是）。

### 5.2 Google Play

| 阶段 | 内容 |
|---|---|
| 账号 | play.google.com/console 注册组织账号：Wordgate LLC，D-U-N-S、公司官网 / 邮箱域验证、$25、身份验证。需要用户：Google 账号登录、支付、法务信息。developer name 填 "Overleap"。 |
| 应用 | 创建 "Overleap VPN"（包名 `io.overleap`，免费）。 |
| 商店页 | 简短描述 80 字、完整描述 4000 字（隐私优先叙事）、图标 512、置顶大图 1024×500（`generate.sh` 产）、手机截图 ≥ 4（模拟器 Pixel 9 Pro 1080×2424 → 取自 §5.1 同一套界面）、隐私政策 URL。 |
| 政策表 | 数据安全（邮箱、设备 ID、崩溃日志；传输加密；可删除账户：app 内 Account 页已有删号入口（`DELETE /api/user/delete-account`），据实填写）、内容分级问卷（工具类）、目标受众 18+、VPNService 声明表（核心功能 = VPN，说明用途）、前台服务 `specialUse` 说明（VPN 隧道保活）、广告声明（无）。 |
| 发布 | 上传 AAB 到内部测试轨道 → 封闭测试 → 生产。组织账号无 14 天 20 人门槛；审核通常 1-7 天。首个 AAB 登记上传密钥（`overleap-release.jks` 即上传密钥，Google 生成应用签名密钥）。 |
| 回填 | 生产可见后回填 `storeLinks.android`（web）与 `storeUrls.android`（webapp 已预填）。 |

## §6 上线前 ops（不属代码，按序）

1. **节点声明 overleap**：目标集 = 27 个共享节点里排除 SNI 伪装为 `www.<省份>.people.cn` 的节点（plan 里先 SQL 列出 `tunnels.sni`）。方式：kaitu-node-ops 的 canary + batch 脚本，在每台 `.env` 加 `K2_NODE_BRANDS=kaitu,overleap` 并重启 k2s（触发重注册）。先 1 台 canary 验 `slave_nodes.brands`，再批量；选低峰时段（对该节点上的开途用户是一次重连）。验收：`?brand=overleap` 节点列表 ≥ 15。
2. **生产 config**：`appstore.bundleIds.overleap`、`mail_overleap.*`（见 §3.6）。config 改动需重启，与 Stripe 那次同款滚动。
3. **SES 发件域 `overleap.io`**：Route 53 加 SES DKIM 三条 CNAME + SPF 合并进现有 Zoho 的 TXT（`include:amazonses.com`）+ DMARC 维持；`support@overleap.io` 验证。用 AWS CLI 做，DNS 生效后 `aws ses get-identity-verification-attributes` 绿。
4. **审核演示账号**：`review@overleap.io`（Zoho 收件）注册 Overleap 用户 + admin `add_user_membership` 给 1 年权益；写进 ASC 审核信息与 Play 的"应用访问权限"表。
5. **Slack `alert` 频道**确认在收（支付哨兵）。
6. 上架后：`storeLinks` / `storeUrls` / `K2_APP_STORE_URL` / `OVERLEAP_APPSTORE_URL` 回填（§1.6）。

## §7 验证

### 7.1 代码门（每个 worktree 合并前）

- webapp：`npx vitest run` 与 `K2_BRAND=overleap npx vitest run` 全绿；`yarn build` 双品牌 + `check-brand-purity.sh`。
- mobile：`scripts/check-mobile-brand-assets.sh overleap` 绿且反向红；`bash scripts/test-ios-build-number.sh`；本机 `BRAND=overleap make build-android`（APK + AAB）+ 纯度门（APK 与 AAB 各一次）；`BRAND=kaitu make build-android` 产物 MD5 与改动前（同 k2 pin）逐文件相同，除 `versionCode`。
- desktop：`cargo test` 与 `K2_BRAND=overleap cargo test`；`clippy`。
- api：`go test ./... -count=1 -v | grep -c -- '--- SKIP'` ≈ 0（`center/config.yml` 到位）；`UPDATE_CONTRACT=1 go test -count=1 -run TestExportContract` 后 `git diff contracts/` 为空（本波不改品牌注册表）。
- CI：`release-desktop` dry-run、`build-mobile` dry-run 各一次全绿。

### 7.2 真机 smoke（release 信心门）

iPhone 15 装 Overleap IPA、Android 真机装 AAB 派生 APK，各跑：注册 / 登录 → 节点列表非空 → 连接 → 访问 `ifconfig.me` 显示节点 IP → 断开 → 与开途 app 同机共存。iOS 额外：IAP 面板显示沙盒商品 $79、购买 → Center 入账 → 恢复购买。Android 额外：全程无购买入口。

### 7.3 信心口径

代码信心目标 9/10（差的一分是 staging 双 host 冒烟，沿用前两波口径）。业务信心 = §5 + §6 打钩数，分开报。上架放行本身由 Apple / Google 审核决定，不打分。

## 文档同步（列入验收）

- `mobile/CLAUDE.md` Brand 段：AAB、`k2_apk_updates`、tag 后缀、`generate.sh` 覆盖移动资产、`storeUrls`。
- `webapp/CLAUDE.md`：`purchaseSurfaceAvailable` 是唯一购买入口门，`androidPurchase` / `storeUrls`。
- `desktop/CLAUDE.md`：签名描述随品牌、日志上传前缀。
- `api/CLAUDE.md`：`mail_overleap` 前缀、`sendSystemEmailAs`、节点列表品牌字段。
- `scripts/CLAUDE.md`：移动 tag 矩阵。
- 根 `CLAUDE.md` Artifact naming 段补 `Overleap-{VERSION}.aab`。
- `docs/ops/overleap-api-deploy.md`（新）。
