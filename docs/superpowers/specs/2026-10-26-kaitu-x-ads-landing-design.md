# 开途 X 广告第一期：着陆页 + 归因码 + 漏斗 — 设计 spec

日期：2026-10-26 · 基线：`main` @ 02944031 · 决策人：David
相关：`2026-10-19-kaitu-ios-apple-trial-design.md`（iOS 试用，本期直接复用）、`2026-10-20-ios-device-account-design.md`、`2026-10-01-overleap-pricing-purchase-funnel-design.md` §2（统计平台）
模拟稿：https://claude.ai/artifact/5dc3gfQajSEWtjey3BtKGs（X 广告 A/B、iPhone 着陆页、Mac 着陆页、流程与埋点）

## 0. 为什么做

开途要在 X 上投广告。广告的钱花得值不值，取决于两件事：点进来的人有没有被转化，以及我们能不能量出转化。现状：
- 开途 iOS 已上线 Apple 7 天免费试用（服务端已部署），是现成的最短转化路径。
- 安卓 / 桌面 / 网页是 NextPay 一次性年付，没有自动续费，也没有试用；开途安卓不在 Google Play（只有 APK 侧载）。
- 统计平台（`/api/px` 像素 + `sid`、App 事件、事实投影、推断归因 `ip_os_v1`）已经覆盖网页 → App → 试用 → 付款，缺的是"广告着陆"这一段的入口和分组。

## 1. 决策（David 已定）

| # | 决策 |
|---|---|
| 1 | 推广品牌是**开途**（不是 Overleap），中文素材，面向全球说中文的人 |
| 2 | 人群：**高端用户、女性为主**，X 后台直接只投女性 |
| 3 | 第一期**只投 iPhone + Mac**（X 按设备定向）。安卓侧载门槛太高，第一期不投 |
| 4 | **Mac 访客引导到 iPhone 扫码，在 iPhone 上开 Apple 试用**；Mac 是"一份订阅 5 台设备"的附赠价值，不在 Mac 上单独做试用 |
| 5 | 定位：开途是**主角，主张"更稳定"**；进门方式是"零成本对比"（不用卸载现在的，并排用、自己比） |
| 6 | 试用期内**什么都不加**：不发提醒、不做总结页、不强调"试用"。忠实用户靠长期稳定留住 |
| 7 | 预算：第一期 4 天 × 25 美元/天。按"网站点击 / 着陆页访问"投放，**不接 X 像素、不做转化回传**（量太小，X 学不起来） |
| 8 | 素材只测 **A（稳定）/ B（并排对比）** 两套，同一张图、只变文案；C 已否 |
| 9 | **A / B 各用一个归因码**区分，码贯穿着陆页、App Store 活动参数、后台漏斗 |
| 10 | 全平台卖点：素材和着陆页写"一份订阅 · 5 台设备 · 手机、平板、电脑全平台"（iPhone、Mac、Windows、安卓、Linux） |
| 11 | 配图（2026-10-27 改定）：**明亮桌面生活方式**——清晨阳光、浅木书桌、打开的笔记本电脑与手机（均无 logo、无屏幕文字）、拿铁、翻开的书、一只女性的手（无脸），墨绿小花瓶呼应品牌色，上部约 40% 暖白墙留白压字。依据：Gemini 深度调研（`scratchpad/xad/research/x-ad-creative-women-2026-10-27.md`，结论均为未经 X 实测验证的假设）认为雨夜撑伞图与 VPN 关联弱、暗色易被划走、男女同框不合适；David 采纳调研、不再比图。Gemini（anc.info7）生成，3 张候选自评取最高分 `desk-chosen.png`；图上文字本地排版合成（不让 AI 写字），成品 `final-a.png` / `final-b.png`，1080×1080。原雨夜图 `rain3-3b.png` 弃用。风险：笔记本外形像 MacBook（无 logo），送审时留意 |
| 13 | 图上文案（2026-10-27 改定，A/B 共用主句）：主句「**别的断了，它还在。**」（取自调研方向一；去掉"身边"的情感隐喻）；副句 A「开途 · 一键连上，一直在线」/ B「开途 · 不用卸载，并排用 7 天自己比」。推文正文分别用 A（稳定）/ B（并排对比）文案（见模拟稿 ①） |
| 12 | 着陆页方案 A：放在 `web/`（kaitu.io）里新建独立广告着陆页（见 §3 Approaches） |

## 2. 目标 / 非目标 / 反目标

**目标**
- X 广告点进来的 iPhone 用户：着陆页 → App Store → Apple 试用，一个按钮。
- Mac 用户：着陆页显示二维码 → iPhone 扫码 → 同上。
- 4 天后能回答："A 和 B 各带来多少访问、点击、App 首开、试用、首购"，并能和 X 后台（曝光 / 点击）及 App Store Connect（下载）对得上。
- 以后加素材或换平台（Google Ads），**只发新码 + 加配置，不改后端**。

**非目标（第一期不做）**
- 非苹果端试用（安卓 / 桌面 / 网页）——见 §9 第二期备忘。
- X 像素、X Conversions API、Google 离线转化回传。
- 试用期内的提醒、引导、总结页。
- 改官网首页 / 其它页面的视觉。
- 安卓投放。

**反目标（能跑但算失败）**
- 广告链接在任何情况下 404 或报错（码写错、码过期、参数被平台改写）。
- 着陆页上出现无法核实的数字或"保证"类说法。
- 广告页、素材里出现开途以外的品牌名，或中文用户面出现 "Kaitu" 裸词（必须是「开途」）。
- 统计故障影响页面打开或跳转 App Store。

## 3. Approaches（已选 A）

| 方案 | 做法 | 取舍 |
|---|---|---|
| **A. `web/` 内独立着陆页**（选定） | 新路由，去掉导航、单一 CTA；复用像素、`sid`、品牌配置；照常 `git push origin main:website` 部署 | 唯一能直接接上现有统计平台、把网页访客与 App 漏斗连起来的方案；以后 Google 加一个变体即可 |
| B. 直接用首页 + utm | 零开发 | 首页给自然流量 / SEO，出口多、分散广告流量；为广告改首页伤 SEO |
| C. 独立静态页（单独域名 / CDN） | 最轻最快 | 脱离 kaitu.io 的 cookie 与统计，访客无法接上 App 漏斗，统计要重做 |

## 4. 用户流程

```
X 推文（女性 · 中文 · iOS/macOS）── 点卡片
  └─ kaitu.io/zh-CN/go/<码>?utm_source=x&utm_medium=paid_social&utm_campaign=x_2026q4_w1&utm_content=<码>
       ├─ iPhone / iPad：主按钮「在 App Store 免费获取」→ App Store（带 pt / ct=<码>）→ Apple 7 天试用 → 第 7 天自动转正
       ├─ Mac / 其它桌面：主区域二维码（= 同一着陆页，码加 q 后缀）→ iPhone 扫码 → 走 iPhone 分支
       └─ 安卓（自然转发进来，非投放目标）：按钮去 /install（现有下载页），不做专门处理
```

中国区：开途在中国区 App Store 上架（iOS 试用 spec §1 已核实 `country=cn` 有结果；2026-10-26 首笔真实 Apple 首购即为 storefront=CHN），所以大陆 Apple ID 用户也走同一个 App Store 按钮，不做外区账号兜底。

试用开始后要在 Mac 上用：按设备账号设计，需先在 iPhone App 里绑定邮箱，再在 Mac 用邮箱登录。本期不改这条路径。

## 5. 着陆页（`web/`）

### 5.1 路由与归因码

- 路由：`web/src/app/[locale]/go/[code]/page.tsx`。`/s/[code]` 已被邀请占用，故用 `/go/`。
- 码注册表：`web/src/lib/ad-codes.ts`（静态配置，构建期打包）：

  ```ts
  // 码 → 素材变体。码出现在广告链接、App Store ct、漏斗 landing_path 里；码语义不可变，换素材就发新码。
  export const AD_CODES = {
    xa1:  { variant: 'stable',  channel: 'x' },
    xb1:  { variant: 'compare', channel: 'x' },
    xa1q: { variant: 'stable',  channel: 'x', via: 'qr' },   // Mac 二维码 → 手机
    xb1q: { variant: 'compare', channel: 'x', via: 'qr' },
  } as const;
  ```
- **未知码不 404**：按默认变体 `stable` 渲染，码照常进统计（便于发现拼错的链接）。
- 码只做归因，不做门禁、不过期（与第二期的"试用活动码"是两回事）。
- 品牌：`web/` 只构建 kaitu.io（见根 `CLAUDE.md`），页面里不出现品牌字面量以外的东西；文案走 next-intl `messages/zh-CN/*`（zh-TW / zh-HK 可先回落 zh-CN 或同步翻译，Builder 定）。
- `noindex`：广告着陆页不进搜索引擎（`robots: { index: false }`），避免与 SEO 页互相稀释。

### 5.2 设备分流

客户端判断（`navigator.userAgent` + `maxTouchPoints`，iPadOS 桌面 UA 要识别为 iPad）：

| 设备 | 主 CTA | 事件 |
|---|---|---|
| iPhone / iPad | 「在 App Store 免费获取」→ App Store 链接 | `install_click`，`p=ios` |
| Mac / Windows / Linux 桌面 | 二维码卡片「用 iPhone 扫码开始」+ 小字链接"已经在用 iPhone 看？直接打开" | `qr_view`（新增，二维码渲染时发一次） |
| 安卓 | 「下载开途」→ `/install` | `install_click`，`p=android` |

- SSR 先渲染不依赖设备的首屏（标题、副标题），CTA 区在客户端挂载后再定形，避免闪烁可用骨架占位（Builder 定）。
- App Store 链接：`brand.storeLinks.ios` + `?pt=<provider token>&ct=<码>&mt=8`。`pt` 是 David 在 App Store Connect（App Analytics → Campaigns）里的 provider token，写进配置。
- 二维码：前端本地生成（不调第三方服务），内容 = `https://kaitu.io/zh-CN/go/<码>q?` + 同样的 `utm_*`。

### 5.3 页面内容（见模拟稿 ②③）

单列、手机优先、一屏一件事，无顶部导航和多余出口：
1. **首屏**：与广告图同一张图（无字原图，裁切适配手机竖屏，标题以网页文字压在上部留白区）、同一句主标题「别的断了，它还在。」——点进来看到的第一眼和广告一致（message match）；副标题随变体——A「开途一键连上，一直在线。」/ B「不用卸载你现在的，并排用 7 天，哪个更稳你自己比。」；主 CTA；一行"一份订阅 5 台设备 · iPhone、Mac、Windows、安卓全平台"。图片走 Next `<Image>` 压缩（首屏加载要快，X 内置浏览器网速不一定好）。（原"一键连接，不用任何设置"一行与 A 副标题重复，已删。）
2. **稳定的证据**：只放可核实的事实（如近 30 天连接成功率、某次大面积断线当天的表现）。**数据由 David 提供，写死在文案里**（不做实时接口，不改后端；Claude 的决定）；**拿不到可核实的数据就整段删掉**，不得编造。
3. **为什么是开途**：打开就连上 / 一份订阅 5 台设备全平台 / 不用先卸载现在的。
4. **底部重复 CTA** + 试用披露（一行小字）：「新用户可免费试用 7 天，之后按 App Store 显示的年费自动续订；试用结束前在 iPhone 设置中取消，不会收费。」**不写具体金额**（Claude 的决定）：受众的 Apple ID 分散在各区，价格和币种随区变化，固定金额对很多人是错的；具体价格由 App 内付费墙按 StoreKit 显示（Apple 3.1.2 的披露在那里完成）。
5. 页脚：隐私政策、服务条款。

视觉：广告页独立一套"安静高级"风格——米白底 `#f4f1ea`、深墨绿 `#14231c`、苹方 / 系统字体、无霓虹绿；只作用于 `/go/` 路由，不动官网其它页面。

### 5.4 统计

- `page_view`：现有 `FunnelPageView` 自动上报，`u` 带路径 `/go/<码>` 和 `utm_*`（`pixelLocation` 只保留 `utm_*`，`c`/其它参数不外发）。
- `install_click`：沿用现有事件与 `p` 参数。
- **新增事件 `qr_view`**（surface `web`，kind **`action`**——不能登记成 `view`：`funnelViewsWeb()` 会把所有 web view 事件自动并入 `web_purchase` / `web_install` / `app_to_web` 的第 1 步，悄悄改变现有路径口径）：注册到 `api/logic_funnel_events.go` 和 `web/src/lib/funnel.ts` 的 `WEB_FUNNEL_EVENTS`，并**重生成契约**：`cd api && UPDATE_CONTRACT=1 go test -count=1 -run TestExportContract ./...`，`contracts/api-contract.json` 一并提交（`web` 的 funnel-contract 测试会锁一致）。
- Cookie 横幅：现有"默认开启、可退出"模型不变。

## 6. 后端（Center，`api/`）

只加一条漏斗路径，不改表：

```go
{
  Key: "ad_landing", Title: "广告着陆", Question: "广告点进来的人里，有多少开始试用、最终付款？按 landing_path（= 归因码）分组看 A / B。",
  Lane: funnelLaneWeb, Hop: "网页 → App 依赖推断归因（ip_os_v1，仅开途）；Mac 扫码者在手机上是新的 sid，以 <码>q 路径单独成组。",
  Window: 14 * funnelDay,
  Steps: []funnelStep{
    {Label: "访问", Events: []string{"page_view"}, Surface: FunnelSurfaceWeb, PathPrefix: "/go/"},
    {Label: "点获取 / 看到二维码", Events: []string{"install_click", "qr_view"}},
    {Label: "首次打开", Events: []string{"app_first_open"}},
    {Label: "开始试用", Events: []string{"trial_start"}},
    {Label: "付款", Events: funnelPaidFacts},
  },
},
```

- 分组用现有维度 `landing_path`（A/B = `/go/xa1` vs `/go/xb1`，扫码 = `/go/xa1q`），也可按 `utm_campaign`。无需存 `utm_content`。**不要把 `/go/` 加进 `funnelCodePathPrefixes`**，否则码会被折叠成 `*`、A/B 分不开。
- 写死路径表的测试（`TestFunnelPaths_LaneAndHop`、`funnelStepEmitSites`）要手动加 `ad_landing`。
- **口径（Claude 的决定）**：
  - Mac 访客在 Mac 上停在第 2 步，扫码后在手机上以 `/go/xa1q` 另起一行。第一期**人工把 `xa1` 与 `xa1q` 相加**看变体 A（B 同理），不新增"按变体合并、按人去重"的分组维度。同一人会在"访问"里被计两次，量级上可接受。
  - "首次打开 / 开始试用 / 付款"只能靠推断归因（同出口 IP + 同平台 + 24h）接上。这批用户多半开着别家 VPN 刷 X，出口一变就接不上，所以这几步**只是下限**。**A/B 的胜负按确定性的上半段判断**：X 后台点击率 → 着陆页 CTA 点击率（`install_click`/访问）→ App Store Connect 按 `ct` 的下载数（Apple 只统计同意共享分析的用户，同样是抽样但两组同口径）。后半段只用来看整体试用率。不为第一期做确定性的网页 → App 链路（App 读不到 `ct`；剪贴板传码之类属于额外工程，等预算放大再议）。
- "开始试用"之后的"付款"在第 7 天才发生；4 天测试期内看到的是"开始试用"，首购在 14 天窗口内补齐。
- 管理后台 / MCP `funnel` 工具按路径注册表自动出现新路径（路径数量测试跟注册表走，见 `d49bc8ff`）。

## 7. X 广告设置（David 在 X 后台操作）

- 目标：网站流量 / 着陆页访问。出价按默认自动。
- 定向：性别女；语言中文；设备 iOS + macOS；地区不限（或排除无关地区，David 定）；年龄 25–45（建议，David 可改）。
- 两条推文广告（网站卡片，1:1 图）：A 链 `…/go/xa1?utm_…&utm_content=xa1`，B 链 `…/go/xb1?…&utm_content=xb1`。同一张图，只变文案（文案见模拟稿 ①）。
- 每天 25 美元 × 4 天，两条同一广告组平分（关掉 X 的自动选优，保证两条都拿到量）。
- 4 天后对账：X 后台（曝光 / 点击 / CTR）↔ 后台 `ad_landing` 漏斗（访问 → 试用）↔ App Store Connect 按 `ct` 的下载数。

## 8. 上线前必须核实（阻塞投放，不阻塞开发）

1. **X 对 VPN 类广告的政策**：是否需预审 / 限制地区 / 禁止某些说法。未核实前不投。
2. **App Store Connect 里开途 `io.kaitu.sub.basic.1y` 的 7 天免费 Introductory Offer 已生效**（服务端已部署 ≠ ASC 已打开），且线上 iOS 版本含试用付费墙（iOS spec 第 ② 步）。
3. 着陆页上的稳定性数据：David 提供可核实来源，否则删段。
4. App Store `pt` provider token 由 David 从 ASC 取得。
5. 配图：AI 生成的桌面图（`desk-chosen.png`），人工再确认一次无品牌 logo、无乱码文字、手部正常；确认 X 对 AI 生成图片是否要求标注（调研未找到官方出处）。

## 9. 第二期备忘（非苹果端试用，未纳入本期）

本次讨论已定的方向，留给投安卓 / 桌面直装时再立项：
- **试用活动码**：每条广告一个码，一码多人、24 小时有效、只给新用户、后台可一键作废；着陆页链接自动带码，App 首启自动填，手输兜底。现有 `LicenseKey` 是一码一人一次性，形态不同，需新增码类型（批次 / 有效期 / 品牌 / 后台可沿用）。
- **一机一次**：资格绑硬件 ID 哈希（安卓 `ANDROID_ID`、Mac `IOPlatformUUID`、Windows `MachineGuid`；桌面 `storage_crypto.rs` 已在读），不按账号。现有 `device-udid.ts` 是随机 UUID，重装即变，不可用于此。
- 邮箱规范化（去 `+` 后缀、Gmail 去点）。David 认为白嫖风险有上限（码 24h、试用 7 天、后台可见流量），**不加流量上限**。
- 试用 7 天，用会员节点；到期 App 内引导 NextPay 一次性年付。

## 10. 留给 Builder 的

- 设备判定的具体实现与首屏防闪烁方式。
- 二维码生成库的选择（须本地生成、体积小）。
- zh-TW / zh-HK 文案是回落还是同步翻译。
- 页面组件拆分、测试形态（码注册表、设备分流、未知码回落、pixel 参数需有单测；`qr_view` 契约测试必须过）。
- 不在范围内的改动一律不做（不动首页、Header/Footer、购买页）。
