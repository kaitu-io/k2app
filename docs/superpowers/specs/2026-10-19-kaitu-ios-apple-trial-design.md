# 开途 iOS：Apple 订阅 7 天免费试用 — 设计 spec

日期：2026-10-19 · 基线：`main` @ 931583a2 · 决策人：David

## 0. 为什么做

- 10-09 ~ 10-19，开途 App 首次打开 971 → 登录 430 → **连上 45（4.6%）**。登录的 430 人里 **390 人是 iOS（91%）**。
- 原因：开途没有试用。没付费的人在节点列表就被 `tunnel_locked` 拦住（`webapp/src/components/CloudTunnelList.tsx:306`），从来没真正用过。同期 App 付费墙 597 人看过 → 8 人付款。
- 开途 iOS 已经是 Apple 自动续费年付订阅（`io.kaitu.sub.basic.1y`，StoreKit 2，整页 IAP 面板，`webapp/src/pages/Purchase.tsx:584-616`），所以加试用只需要 Apple 的 **Introductory Offer（首次优惠：免费 7 天）**，不需要换付款方式，也不用优惠码。
- David 已确认：「试用提升转化是真实的」，不做事前测试，直接做。

## 1. 决策（David 已定）

| # | 决策 |
|---|---|
| 1 | 只做 iOS 的 Apple 原生试用：在 `io.kaitu.sub.basic.1y` 上配置 Introductory Offer = **免费 7 天** |
| 2 | **不用** Apple 优惠码、推广优惠（promotional offer）、赢回优惠——复杂度太高 |
| 3 | 安卓 / 桌面 / 网页这次**不做**试用（它们是 NextPay 一次性付款，没有自动续费，做不了绑卡试用） |
| 4 | 三步严格按顺序：① 服务器 → ② iOS 原生版 + 付费墙 → ③ ASC 打开试用。①未部署前**禁止**在 ASC 打开试用 |
| 5 | 服务器入账时记录交易来自哪个国家的 App Store（storefront），用来看国区 / 美区占比 |

已核实的前提：
- 开途在中国区 App Store 也上架（iTunes lookup `country=cn` 有结果）。国区 Apple ID 可绑支付宝 / 微信。
- Apple 订阅（含免费试用）要求账户里至少有一种付款方式（[Apple 支持 118291](https://support.apple.com/en-us/118291)）。付款方式为「无」的美区账号开始试用前会被要求绑付款方式——这和他们现在直接订阅时的门槛一样，试用不新增门槛。

## 2. 目标 / 非目标 / 反目标

**目标**
- 有资格的 iOS 用户在付费墙上看到「免费试用 7 天」，点一下 → Apple 支付表单 → 立即可连。
- 试用在账务上**不是付款**：不建订单、不返佣、不发邀请奖励、不算首单；试用转付费的第一笔扣款才是首购。
- 能量化：漏斗出 `trial_start`；试用转付费出 `purchase`；能按 storefront 拆分。

**非目标**
- 安卓 / 桌面 / 网页试用；Apple 优惠码 / 推广优惠 / 赢回优惠的正确记账（只告警，见 §3.4）。
- 试用到期提醒邮件（Apple 自己会处理订阅续费通知；以后再说）。
- 改退款政策文案（iOS 退款由 Apple 处理；试用没有收钱）。
- Overleap 的 ASC 配置（服务器改动对 overleap 同样生效，但 overleap 何时打开试用另行决定）。

**反目标**（技术上能跑但算失败）
- 任何一次免费试用产生了订单、返佣、邀请奖励，或让用户被判为「已付费」。
- 现有付费用户的续费入账、到期时间、退款收回出现任何变化。
- 旧版 iOS App（新原生版发布前的版本）无法购买，或 web OTA 在旧壳上报错。

## 3. 第 ① 步：服务器（Center，`api/`）

### 3.1 现状（为什么现在不能开试用）

调查结论（全部 file:line 见附录 A）：服务器不读 `offerType` / `price` / 通知 subtype。一笔 $0 试用交易今天会：
1. 走首购分支，`subscription_credits.kind=purchase`（`logic_apple_iap.go:255-276,332-341`）
2. **发邀请奖励**——双方各 30 天（`:232-247`，`logic_invite.go:159-176`）
3. 置 `IsFirstOrderDone=true`、设 Tier（`:305-312`）→ 首单优惠码对他失效
4. 推进 `paid_through`（`:321-326`）
5. **按套餐标价建已付款订单**（`createAppleIAPOrderInTx`，`:537-564`）
6. **分销商首单返佣**，90 天后可提现，取消试用后无任何收回机制（`logic_retailer.go:371-380,447-489`）
7. 试用转付费那笔真钱走续费分支（`kind=renewal`，分销商拿续费比例，漏斗记 `renewal`）

### 3.2 识别免费试用交易

- 判定：交易 `offerType == 1`（Introductory）且 `offerDiscountType == "FREE_TRIAL"`。
- qtoolkit appstore v1.5.32 的 `TransactionInfo` 有 `OfferType`、`Price`，但**没有** `offerDiscountType` / `offerPeriod`（`~/go/pkg/mod/github.com/wordgate/qtoolkit/appstore@v1.5.32/types.go:123-149`）。**不改 qtoolkit**（别的仓库）：在 k2app 里解码已验签 JWS 的 payload 段读取这两个字段。拿不到原始 JWS 的路径，退化判定为 `OfferType == 1 && Price == 0`。
- 判定函数做成纯函数（输入：交易字段；输出：`trial | paid | other_offer`），放在 `creditAppleTransaction` 内部调用，这样三个入账入口（客户端 verify `api_apple_iap.go:31`、webhook `api_apple_webhook.go:105`、对账 worker `worker_subscription_reconcile.go:129`）都覆盖到。
- 实际情况（builder check 核实）：三个入口拿到的都是 qtoolkit `GetTransaction` 已解析的结构体，只有 webhook 有原始 JWS。所以 **`OfferType == 1 && Price == 0` 是主判定**，webhook 路径可再用 JWS 里的 `offerDiscountType` 交叉核对、不一致就告警。（我们不发付费的 intro offer，所以 `Price` 缺失被误判成试用的情况不会出现在真实交易里。）

### 3.3 入账规则

| 交易 | 会员时长 | `subscription_credits.kind` | 订单 / 返佣 | 邀请奖励 | `IsFirstOrderDone` | `paid_through` | 漏斗 |
|---|---|---|---|---|---|---|---|
| 免费试用 | 照常给到 `expiresDate` | **`trial`**（新值） | 不建 / 不返 | 不发 | 不动 | 不动 | `trial_start` |
| 试用后**第一笔付费**交易 | 照常（从试用结束续到新周期末） | `purchase` | 建单，**首单比例**返佣 | 发 | 置 true，设 Tier | 推进 | `purchase` |
| 之后的付费续费 | 不变 | `renewal` | 不变 | — | — | 不变 | `renewal` |
| 没有试用的普通首购 / 续费 | **完全不变** | 不变 | 不变 | 不变 | 不变 | 不变 | 不变 |

要点：
- 「首购」的判定从「订阅链上第一笔交易」改成「订阅链上**第一笔付费**交易」。
- 会员时长的计算（`applyRenewalCredit` 等，`logic_entitlement.go`）对**有过付费的订阅链**不改，只改记账标签与副作用。
- **例外（我的决定）：试用链上的第一笔付费按「首购公式」给时长**——一个周期（`expiresDate − purchaseDate`），从 `max(now, 当前到期)` 起算。原因：Apple 对同一订阅组的重新订阅沿用同一个 `originalTransactionId`，用户取消试用、几周后再买时，这笔会走续费分支，`applyRenewalCredit`（`logic_entitlement.go:35-41`）会给「一年 + 整个空档」。试用直接转付费时两种公式结果相同，只有「试用失效后再买」才有区别。
- **已有问题，本次不修**：付过费的老用户中断一段时间后再订阅，同样会多给空档天数（只有退款恢复走了按周期计算，`logic_apple_iap.go:194-202,256-277`）。记录在案，单独处理。
- **宽限期（我的决定）**：从没付过费的订阅链收到 `DID_FAIL_TO_RENEW`（试用转付费扣款失败）时，**不做宽限期延长**（`computeRenewalState` / `applyRenewalInfo` 的 grace cover-through 只对有过付费的链生效），会员在试用结束时到期。`is_trial` 在试用结束或首笔付费入账时清掉。国区支付宝 / 微信扣款失败可能较多，不能变成免费延长。
- **换账号重新订阅（我的决定）**：现在订阅链会绑定到第一个账号，另一个账号来认领会被拒（`logic_apple_iap.go:134-137`），结果是 Apple 收了钱、我们什么都没给。试用会让很多 Apple ID 先绑到一个没付过钱的账号上。规则：**从没有过付费交易的链**，在第一笔付费交易入账时允许改绑到当前登录的账号，原账号不受影响（它的试用早已结束）。有过付费的链仍然拒绝。
- 试用用户的设备上限：沿用首购会设的套餐 Tier（basic），不额外限制。
- 订阅行需要能表达「当前在试用」：给 `Subscription` 加字段（如 `is_trial` / `trial_ends_at`），首笔付费入账时清掉。**不新增 `status` 枚举值**——新增状态必须审计所有读取点（见记忆 feedback_new_state_audit_all_readers），加独立字段风险更小。builder 若坚持加状态值，必须列出并修改所有读 `status` 的地方。
- `DataSubscription`（`api/type.go:105-111`）加 `isTrial`、`trialEndsAt`，给客户端显示「试用中」。
- 记录 storefront：`subscription_credits` 加 `storefront` 列（交易里的 `storefront`，ISO 3 位国家码）。**只记以后入账的交易**（包括老订阅以后的续费），不回填历史（回填要对每笔老交易调一次 Apple API，不值得）。

### 3.4 其他 offer 类型

`offerType` 为 2 / 3，或 Introductory 但不是 FREE_TRIAL（付费折扣）：**按现状入账**，同时 `log.Errorf` + Slack 告警「出现未支持的 Apple 优惠交易」。我们不发这类优惠，出现即说明有人在 ASC 里配了东西。

### 3.5 连带修改

| 位置 | 改动 |
|---|---|
| 漏斗 `logic_funnel_facts.go:173-255` | 从 `kind=trial` 产出 `trial_start` 事实（事件已注册 `logic_funnel_events.go:56`，当前无生产者，`logic_funnel_facts_test.go:343-345` 断言为空——要改测试）。`purchase` 事实只认 `kind=purchase`，试用天数不进付费留存（`logic_funnel_retention.go:98` 核对） |
| 续费提醒 / 召回 worker `worker_renewal_reminder.go` | 查询已经过滤了 `is_first_order_done = true`，试用用户不会收到——**无需改代码**，加一个测试锁住这个过滤条件 |
| 退款收回 `logic_apple_iap.go:449-465` | 试用不再建订单后，「还有别的有效付款订单」的误判自然消失；加测试锁住：试用转付费后该笔被 Apple 退款 → `IsFirstOrderDone` 回到 false、返佣撤销、会员按该笔交易收回 |
| 后台统计 `api_admin_stats.go` | `user_statistics` 增加「试用中」人数；`activePro` 口径注明含试用（或拆开） |
| 沙盒 | 沙盒交易仍不建单（`:365`），试用判定对沙盒同样生效，方便 TestFlight 验证 |

### 3.6 测试（必须）

- 判定纯函数：FREE_TRIAL / 付费 intro / promotional / 普通首购 / 字段缺失。
- 入账端到端（DB 测试，**`-v` 下 0 SKIP**，见 api/CLAUDE.md）：试用 → 取消 → EXPIRED；试用 → DID_RENEW 转付费；试用 → DID_FAIL_TO_RENEW（无宽限延长）；试用 → 失效 → 数周后重新订阅（只给一个周期）；试用失效 → 换账号重新订阅（改绑成功）；有付费历史的链换账号（仍拒绝）；试用 → 转付费 → REFUND；普通首购 → 续费（回归，断言与改前完全一致）；客户端 verify 与 webhook 先后到达两种顺序；同一试用交易重复投递幂等。
- 分销商：试用 0 返佣；转付费按首单比例；邀请奖励只在转付费时发一次。
- 变异验证：把判定函数改成恒返回 `paid`，上述试用用例必须变红。
- 跨品牌：overleap 用户的试用交易走同一规则（brand-agnostic），不破坏 `logic_apple_iap_brand_test.go`。

### 3.7 体验线路隔离（2026-10-19 David 追加：「按最好的转化收益推进」）

目的：试用用户只能用单独的「体验线路」，拿不到会员节点地址；会员线路保持私密，付费页用「体验线路 vs 会员私密线路」的对比制造升级理由。

**谁算试用用户**：`users.in_trial = true`（新列，bool）。
- 免费试用交易入账时置 true，**但用户已付过费（`IsFirstOrderDone=true`）时不置**——付过费的人换个 Apple ID 开试用，不能被降级。
- 首笔付费入账、或该试用订阅 `EXPIRED` / `REVOKE` 时置 false。
- 放在 User 上而不是每次查 Subscription：节点鉴权是热路径，`AuthorizeNodeAccess` 手里已经有 user。

**节点**：`slave_nodes` 加 `pool` 列（varchar，默认 `""` = 会员节点，`"trial"` = 体验线路）。后台节点编辑（`api_admin_node.go:129-173`）与 MCP `update_node` 能设置它。
- 试用用户：只看到、只能连 `pool="trial"` 的节点。
- 付费用户：看到全部节点（含体验线路，容量不浪费）。
- 体验线路同样要按品牌可见（`VisibleTo`），否则另一个品牌的试用用户列表为空。

**一个判定函数，五处调用**：`nodeAllowedForUser(node *SlaveNode, user *User) bool`，加在所有下发节点地址的地方：`api_tunnel.go:137`（含 MCP 走的 `/api/tunnels/k2v5`）、`api_tunnel_v20260717.go:78`、`api_relay.go:76`、`api_subs.go:362`（`fetchK2V5Tunnels`）。
- **节点鉴权是真正的门**：`AuthorizeNodeAccess`（`entitlement_authorize.go:11-22`）shared 分支加「试用用户只能连 `pool=trial`」。列表过滤只防新地址下发；客户端缓存里已有的地址（webapp 缓存、k2subs 30 分钟刷新）只能靠节点鉴权挡。k2s 只缓存成功鉴权 15 分钟，所以「付费 → 试用」方向最多 15 分钟延迟（实际不会发生：付过费的人不会被置试用），「试用 → 付费」不受影响。
- **不改**：antiblock 公开种子（`api_antiblock.go:122-131`）照旧；`/slave/accelerate-tunnels` 照旧（节点间中继，用户看不到）；路由器只拿私有节点，不涉及。
- ⚠️ 已知事实：antiblock 种子本来就把健康的共享节点公开发到 CDN（无需登录），所以「会员节点地址完全不外泄」现在并不成立；体验线路隔离挡住的是「试用用户直接连会员节点」。

**锁定的会员线路展示**：`/api/v20260717/tunnels` 对试用用户额外返回 `lockedSummary: [{country, count}]`（被过滤掉的会员节点按国家计数），**不含域名 / IP / serverUrl**。客户端渲染见 §4.2。

**体验线路选哪几台**：上线第 ③ 步前从现有共享节点里指定 2–3 台（不同地区，负载低的），不新开机器；试用量起来后再按负载加。具体选哪台在 ③ 步按节点数据定。

**测试**：判定函数的表驱动测试（试用 / 付费 / 付过费又开试用 / 试用过期后有赠送天数）；五个列表接口各一个「试用用户拿不到会员节点」的测试；`AuthorizeNodeAccess` 试用用户连会员节点被拒、连体验线路通过。

### 3.8 上线

`make deploy-api` 双机滚动。部署后、ASC 未开试用前，线上行为应与现在**完全一致**（除新增 storefront 列有值）——用最近的真实续费通知核对一遍。

## 4. 第 ② 步：iOS 原生 + 付费墙（webapp）

### 4.1 原生插件（需要新 App Store 版本）

- `iapGetProducts` 的商品对象**加可选字段**（不新增方法，旧壳自然没有）：`introOffer?: { paymentMode, periodUnit, periodValue, displayPrice }`、`isEligibleForIntroOffer?: boolean`（来自 `product.subscription?.introductoryOffer` 与 `await product.subscription?.isEligibleForIntroOffer`）。
- 位置：`mobile/plugins/k2-plugin/ios/Plugin/K2Plugin+Iap.swift:32-74`、`IapHelpers.swift:27-45`、`src/definitions.ts:104-122`、`webapp/src/types/kaitu-core.ts:128-138`。
- 按 webapp/CLAUDE.md「兼容模型」：bump `BRIDGE_API_VERSION`（`types/bridge-version.ts:81`，当前 4）与 iOS `k2BridgeApiVersion`（`K2Helpers.swift:86`），重生成 `contracts/bridge-api.json`；能力探测放在 bridge 模块、暴露语义 flag，组件不直接探测。

### 4.2 付费墙与会员页（web OTA 可下发）

- `IosSubscribePanel.tsx`：
  - **有资格**（`isEligibleForIntroOffer === true` 且 `introOffer.paymentMode` 为免费）：主按钮「免费试用 7 天」；按钮下写「7 天后按 {displayPrice}/年 自动续订，可随时在系统设置中取消」（Apple 3.1.2 要求写清试用时长与之后价格）。试用天数从 `introOffer` 读，不写死。
  - **无资格或字段不存在**（旧壳）：保持现在的「立即订阅」。
  - 删掉对所有非会员都显示的 `purchase.currentlyInTrial`（zh-CN 现为「目前您正在试用，完成首次充值后即可使用云端节点服务。」，`zh-CN/purchase.json:48-49`）和不再准确的 `completeFirstPurchase` 表述，换成中性的「订阅后即可使用云端节点」。
  - `autoRenewalDisclosure`（`purchase.json:6`）补试用条款。
- 节点列表（`CloudTunnelList.tsx`）：试用用户在体验线路下方显示「会员私密线路」区块（按 `lockedSummary` 列国家和数量，不可选，点击进付费页）。
- 付费页整体优化（David：「在引导付费的 UI/UX 上花大功夫」）：先讲稳定（晚高峰、丢包），再给可信证据（2023 年起持续运营），再讲「体验线路 vs 会员私密线路」，最后讲清取消方式。具体设计在第 ② 步单独出稿给 David 看。
- `IosMembershipPanel`：`isTrial` 时显示「免费试用中 · {trialEndsAt} 到期后自动续订」，并保留「管理订阅」入口。
- 漏斗：有资格时点击记 `checkout_start` 并带 `trial=1`（或等价维度），以区分试用开始与直接购买。
- 所有 webapp 语言包同步（webapp 现有全部 locale）；品牌字面量规则照旧（文案里不写品牌名）。

### 4.3 发布

- 新原生版本走正常 iOS 发版流程（`make build-ios` → ASC 提交）；webapp 改动随该版本打包，另推一次 `webapp/x.y.z-kaitu` tag 让 web OTA 也到位（旧壳上表现为「无资格」分支，无副作用）。
- 审核备注写明：订阅含 7 天免费试用，付费墙已披露试用后价格与取消方式。

## 5. 第 ③ 步：App Store Connect

- 前置：第 ① 步已部署并核对；体验线路节点已在后台标记 `pool=trial` 并确认试用测试账号只能看到它们；第 ② 步的版本**已提交审核**（审核员需要在沙盒里看到试用按钮能用）。
- 在 `io.kaitu.sub.basic.1y` 的订阅价格 → Introductory Offers：Free，1 week，全部已上架地区。
- 打开后，旧版 App 的用户点「立即订阅」时，Apple 支付表单会自己显示「1 周免费，之后 $X/年」——可接受（Apple 表单本身有披露），只是我们的旧付费墙文案没写试用。
- 打开后第一周：每天看 Slack 告警、`trial_start` / `purchase` 漏斗、storefront 分布、有无试用产生的订单或返佣（应为 0）。

## 6. 衡量

- 主指标：App 激活漏斗「登录 → 连上」（基线 iOS 0.4.12：391 → 20，约 5%）。
- 试用开始率：付费墙 → 试用开始；试用转付费率（7 天后 `trial_start` → `purchase`）；按 storefront 拆。
- 对照基线：付费墙 597 → 付款 8（10-09 ~ 10-19）。

## 7. 留给 builder

- JWS payload 解码放哪个文件、函数名；`Subscription` 新字段的具体命名与迁移方式（GORM AutoMigrate 现状即可）。
- `trial=1` 漏斗维度的具体实现（新维度或复用现有 `channel` / `plan` 维度）。
- 付费墙文案的最终措辞（遵守 4.2 的披露要求）。
- 后台统计「试用中」的展示方式。

## 附录 A：调查引用

- Apple 入账：`api/api_apple_iap.go:31-38`、`api/logic_apple_iap.go:120-402,529-564,568-587,606-634`、`api/api_apple_webhook.go:68,105-129,164-166`
- 返佣：`api/logic_retailer.go:371-380,447-489`；邀请：`api/logic_invite.go:159-176`
- 漏斗：`api/logic_funnel_events.go:56`、`api/logic_funnel_facts.go:173-255`
- 订阅状态：`api/logic_stripe.go:23`（仅 Stripe 有 trialing）、`api/type.go:105-111`
- iOS：`mobile/plugins/k2-plugin/ios/Plugin/StoreKitManager.swift:5`、`K2Plugin+Iap.swift:32-74`、`IapHelpers.swift:27-45`、`webapp/src/components/ios/IosSubscribePanel.tsx:110-117,147-150,228-229,254,280-284`、`webapp/src/hooks/useIapPurchase.ts:100,141-186,223-231`
- 相关设计：`docs/superpowers/specs/2026-10-07-overleap-trial-payment-design.md` §2.2、§4（Apple 部分，Phase C）——本 spec 的服务器部分与之等价且品牌无关，overleap 以后打开试用时直接复用。
