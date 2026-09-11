# eSIM 商店调研摘要（原始沉淀，2026-09-02 至 09-03）

> 配套文档：设计 spec `2026-09-03-esim-store-design.md`（已封存）、市场评估 `docs/marketing/audits/2026-09-04-esim-store-market-assessment.md`。
> 本文是三轮调研的压缩沉淀，比 spec 附录 C 更完整；[M] 表示未验证（第三轮调研时本会话 WebSearch 配额已耗尽，只能直抓已知页面）。

# eSIM 产品调研摘要（2026-09-02）

## 供应商
### eSIM Access（首选）docs.esimaccess.com + github.com/esimaccess/esimaccess-api
- 自助开户 console.esimaccess.com，无 MOQ/无月费，预付余额（Stripe/PayPal/电汇），无沙箱（未安装可 cancel 退回余额）
- Base https://api.esimaccess.com/api/v1/open 全 POST；header RT-AccessCode；可选 HMAC：RT-Timestamp/RT-RequestID/RT-Signature=hmac_sha256(ts+reqId+accessCode+body)；8 rps
- /package/list {locationCode,type BASE|TOPUP,iccid,dataType} → name,packageCode,slug(稳定),price(USD×10000),volume(bytes),duration,activeType(1 安装时/2 首次联网 ~90%/3 购买即激活不可退),retailPrice,speed,supportTopUpType,fupPolicy,locationNetworkList{operator,networkType},dataType(1 固定/2 day pass)
- /esim/order {transactionId(幂等 ≤50),amount?,packageInfoList[{packageCode|slug,count,periodNum?}]} → {orderNo,transactionId}；异步 ≤30s，poll /esim/query 每 3s 或 webhook ORDER_STATUS=GOT_RESOURCE
- /esim/query {orderNo|iccid|esimTranNo,pager} → esimTranNo(主键,ICCID 可复用),iccid,ac(完整 LPA:1$…),qrCodeUrl,shortUrl,smdpStatus(RELEASED/ENABLED/DISABLED/DELETED),esimStatus(GOT_RESOURCE/IN_USE/USED_UP),activateTime,expiredTime,totalVolume,orderUsage,eid,packageList
- /esim/usage/query {esimTranNoList ≤10}（2-3h 延迟）；/esim/topup {esimTranNo|iccid,packageCode|slug,transactionId}（≤9 次，过期后不可）；/esim/cancel（仅 GOT_RESOURCE+RELEASED 退余额）；/esim/revoke 不退；/balance/query
- Webhook：/webhook/save；notifyType ORDER_STATUS/ESIM_STATUS/SMDP_EVENT/DATA_USAGE/VALIDITY_USAGE/CHECK_HEALTH；无签名，IP 白名单 3.1.131.226,54.254.74.88,18.136.190.97,18.136.60.197,18.136.19.137
- 覆盖 130-185 地区含中国大陆（中国联通 5G，香港出口→免翻墙）；样例 日本 1GB/7d $0.91，欧洲 5GB/30d $15.60，美国 1GB/7d $1.04
### eSIM Go（第二）docs.esim-go.com
- 自助注册需验证；首充 $1000（前 3 月）；无沙箱（UAT 第二账号 + POST /orders type:validate）；退款功能默认关闭
- X-API-Key；base https://api.esim-go.com/v2.5/；10 rps；模型=eSIM 档案(iccid,matchingId,smdpAddress) + 可排队 bundle（一档案多国套餐自动切换）
- GET /catalogue；POST /orders {type:transaction,assign:true,order:[{type:bundle,quantity,item,iccids?}]} 同步返回 esims[{iccid,matchingId,smdpAddress}]，bundle 生效 ≤10min poll bundleState；GET /esims/{iccid} → appleInstallUrl/androidInstallUrl；GET /esims/{iccid}/bundles 用量；DELETE …/bundles/{name} 未开始可退
- Webhook 门户配置，V3 HMAC-SHA256（header 名未公开）；Utilisation 1/50/80/100%、FirstAttachment、FirstUse、LowBalance；中国大陆覆盖未公开
### MegaeSIM（第三）docs 在登录墙后，注册即给沙箱 key，人工审核 <1 工作日；首充 $100；定价=零售价减 12-20%；同步下单返回 QR+激活码+ICCID；签名 webhook；含中国大陆；台湾只有日包不限量（贵）
### TelecomsXChange：聚合市场，eSIM API 仅见于博客/MCP README 且路径矛盾；$599.99/年会费；一个卖家起步；同步返回 lpa+qr_base64；用量/退款/webhook 未公开 → 以后比价用
### GloeSIM：零公开文档，API 需审批，不可退款，不提中国 → 放弃

## 平台能力
- iOS：CTCellularPlanProvisioning 需 com.apple.CommCenter.fine-grained（仅 MNO）→ 不可用；supportsCellularPlan 恒 false。**通用链接 https://esimsetup.apple.com/esim_qrcode_provisioning?carddata=LPA:1$…（iOS 17.4+）**，必须从原生 UIApplication.open 打开（不能 WKWebView/SFSafariVC），$ 不编码；首次点击偶发 DNS 失败需重试；<17.4 只能 QR(需另一设备)/手动输入 SM-DP+ 与激活码；17.4+ 长按邮件/浏览器里的 QR 图可 Add eSIM
- Android：EuiccManager.downloadSubscription 非运营商 app 可用（API 28+）→ RESOLVABLE_ERROR → startResolutionActivity 用户同意；PendingIntent 需 FLAG_MUTABLE；Android 通用链接 https://esimsetup.android.com/esim_qrcode_provisioning?carddata=…（GMS ≥25.14.34，2025-04）；兜底 剪贴板 + ACTION_MANAGE_EMBEDDED_SUBSCRIPTIONS；Samsung 可选 ACTION_START_EUICC_ACTIVATION + 导出 CarrierEuiccProvisioningService；Pixel 最干净
- 中国设备：国行 iPhone 仅 17e/Air 有 eSIM；Apple 明文「非大陆运营商 eSIM 在大陆境内无法安装」→ 出境后装；国行安卓 2025-10 起少数机型（华为 Mate80RS/Pura90ProMax、三星 S26、OPPO FindX9 卫星版…），小米/vivo 全无；无 GMS。港版 iPhone eSIM 机型有限（17 系/16e/mini/SE/XS）
- 商店政策：Apple 3.1.3(e) eSIM=外部消费服务 → **禁止用 IAP**（2025-12 拒审先例），网页/Stripe/支付宝可；与 VPN IAP 结账分离避免 anti-steering；Google Play 未明文，Airalo/Holafly 无 IAP 标签直接卡/PayPal
- 无现成 Capacitor eSIM 插件；参考 expo-esim-utils（Kotlin 级联）移植进 K2Plugin

## 仓库
- Discover = 全屏 iframe 嵌网站 /discovery?embed=true（kaitu features.discover=true，overleap=false）；DISCOVERY_DATA 静态在 web/src/app/[locale]/discovery/page.tsx；embed-interceptor.js 拦截所有点击→postMessage external-link；`bridge_navigate {path,params}` 可唤起 app 路由（AndroidInstall.tsx 先例，params 不可信）；OTT `/api/user/ott` 换 web session 供 openExternal 免登录
- 商务：Plan.Product 判别串（app|private_node）；模板=private_node：PrivateNodeSubscription 权益表(OrderID uniq)、PrivateNodePlanSpec 1:1 Plan、applyOrderToBuyer(logic_member.go:89) 分支、post-commit enqueue、worker_private_node.go(provision+timeout sweep+SkipRetry)、onPrivateNodeOrderOnboarding 邮件入队；cloudprovider/ Provider 接口+工厂；Plan/Order 无币种字段；kaitu=WordGate+IAP，overleap=Stripe+IAP；api_order.go:113 活跃订阅拦一次性购买需豁免；api_plan.go:77 产品白名单；admin 无 Product 字段；GORM AutoMigrate；错误码入 response.go + errorCode.ts + 契约 -count=1
- 原生：只加 K2Plugin 方法（Swift pluginMethods 表+@objc；Kotlin @PluginMethod；startActivityForResult+@ActivityCallback 先例）；BRIDGE_API_VERSION 3→4 三处 + contracts/bridge-api.json；dist/ 提交；IPlatform 可选能力 esim?；必须新建 services/capabilities.ts；qrcode@1.5.4 已有（DeviceInstall.tsx 先例）；iOS 无 URL scheme/associated domains；PrivacyInfo 需更新
- webapp：ProHistory.tsx 是「我的订单」模板；Router feature 是 page/store/service/route/nav/i18n 六点接线模板；keep-alive tab 轮询按 pathname 门控；i18n 7 locale + namespaces.ts 生成
- web：purchase/ 分品牌 PurchaseClient(WordGate payUrl)/OverleapPurchaseClient(Stripe)；account/ 只做"买了什么到期何时"；manager-sidebar.tsx 菜单；orders/plans 页模板；messages/{locale}/{ns}.json 手动注册 namespaces.ts；brand features 门控 page+nav+sitemap 三处

## 号码型 eSIM 调研（2026-09-02 第二轮，需求转为 number-first）
### 五家现状
- eSIM Access：文档三处明写 data-only；msisdn 是赞助 SIM 内部号（例 +43），sendSms 是平台→SIM 的 A2P；无号码产品
- eSIM Go：唯一真号码，**仅英国**（Vodafone UK MVNE）；msisdn 仅在 voice/SMS bundle 有效期内存在（MSISDN Enabled/Disabled webhook，bundle 到期/耗尽即失号）；出境只能被叫/收短信；AUP 禁 SIM gateway/bulk；无 KYC（伙伴自担）；零售底价 ZIM £3.50/月、SecondSim £5.99；批发未公开；$1k/月门槛；CDR SFTP 可见 MT SMS 元数据无内容
- TCXC：目录有 sms/voice 字段但例子全 0；MegaeSIM：FAQ 明写 data-only；GloeSIM：无
### 市场三形态
- 形态1 真 MSISDN 在 eSIM 上 + API：esim.tech（25+ 国 US/UK/JP/KR/AU/TH/VN/DE/NL/FR/ES/IT，无 HK/SG；POST /v1/esims/local 同步返 LPA+号码；无最低、无合同；价格不公开需联系）；Telnyx（自助；eSIM $0.70 一次，$2/月激活 $0.20 待机，中国本地数据 $0.0325/MB；enable_voice 给 +E.164 手机号，US 确认 UK 上线；SMS 到设备未验证；估 $36–60/号/年）；eSIM Go UK；Airalo Partner API（Discover+ 含美国 +1 号，收 SMS 免费，覆盖中国，零售 $9 起；号码是否随套餐过期未知）；Gigs（企业合同，内置 KYC，US/UK/EU）；1GLOBAL（唯一 HK 号路径，企业报价，10 国）；eSIM.net（UK 四网、no KYC、批发合作）；BICS（重）
- 形态2 App 托管虚拟号：Holafly(+1/+44 仅收)、Saily(真 +1 $0.99/月 需 ID)、Roamless、Yesim、Numero、Textr —— Google/Telegram/银行常拒
- 形态3 IoT 无公网号：Twilio Super SIM、Ubigi、Keepgo、Monogoto、emnify、Soracom、Onomondo、Simbase、Telna —— 排除
### 消费级保号基准（年成本）
giffgaff UK ≈£0.6/年（半年一次计费动作）+£10 激活；Ultra PayGo US $36/年（$3/月 100min/100SMS/100MB，淘宝 ¥130–180）；Tello $60/年；SmarTone HK$118 全年卡（充 ≥HK$50 续 365 天，海外免费收 HK 短信 OTP）；3HK 国际万能卡 HK$198/395 天；csl 年卡 HK$3/30 天管理费；CMHK 鸭聊佳 无 eSIM 且 2026-06-17 起禁大陆激活；povo JP ¥660/年（需日本证件）；SG 非居民 30 天有效不可行
### 监管
HK 2023-02-24 起实名（护照可），2025-07 拟每人每商 3 张上限；SG 3 张 + 非居民 30 天；JP 语音/SMS 需证件；UK/US 无需
### 待实测缺口
1 号码段是否被 Google/Telegram/WhatsApp 判为 mobile；2 大陆漫游时能否收 MT SMS（所有形态1都没写）；3 esim.tech/eSIM Go/Gigs/1GLOBAL 每号月批发价；4 Airalo Discover+ 号码续存

## 第三轮（亚洲护照办号 + 低资费国家 + 漫游卡商）——WebSearch 配额已耗尽，多数为直抓/记忆，[M]=未验证
### 亚洲
- 日本 [V]：不正利用防止法只管语音 SIM；Mobal 明确接受护照办语音 eSIM（Voice-Only ¥1,430/月 ≈ $115/年 + SIM ¥4,455），漫游名单含中国、收短信免费、无数据漫游、有漫游激活费（金额不明）；HanaCell $69+$12/年但仅限日本境内；esim.tech 列 JP 号码 API
- 韩国：护照可办但须入境后激活、出境自动封号、不能做本人认证 → 排除；esim.tech 列 KR 需核实
- 泰国/马来西亚 [M]：护照可办、年保号 $10–30、收短信免费；MY 每人 5 张上限 [V]；无 API 路径
- 越南（Decree 163/2024 绑签证）、菲律宾（需本地地址+回程票 [V DITO]）、印尼（Telkomsel 漫游收短信收费 [V]）、台湾（双证件）→ 排除
### 低资费国家
- 捷克 +420 [V]：Vodafone CZ 充 200 CZK ≈ $9 保 13 个月，eSIM 有；无实名 [M]；无 API
- 荷兰 +31 [V]：Lebara NL 每 90 天一次动作、最低充 €5 → ~$22/年（余额保留）；非欧盟漫游需特定套餐；无实名；esim.tech 列 NL
- 瑞典（2023 起实名，Comviq 年充一次）、爱沙尼亚 +372（MNO ~$6–12/年 [M]；但 TravelSIM/OneSimCard 的 +372 全球卡号段常被判非手机号 [M]）、芬兰、乌克兰、格鲁吉亚（护照实名，Magti eSIM [V]）、波兰、土耳其
### 漫游卡商 / API 号码商
- esim.tech [V]：唯一 API-first「真本地号」供应商，25+ 国（DE/NL/UK/US/JP/KR/AU…），无最低无合同；价格页 404 → 必须询价（info@esim.tech）
- Telnyx [V]：SIM $1 + $2/月激活；号码定价页只有 local/toll-free（$1/月起），SIM 附着手机号未在定价页出现 → pilot 验证
- WorldSIM [V]：+44 真号，US +1 加 £10/年，每年一次充值保号（~£15/年），有 Reseller/White-label/Affiliate（代理最高 50% 毛利），无 API
- TravelSIM/Top Connect +372：12 个月有效；OneSimCard 站点被封无法验证
- Yesim 虚拟号 $7.20/月 VoIP → 排除；Telna/BICS 企业合同；KnowRoaming/Keepgo/Ubigi/Flexiroam 仅流量；Mobal World SIM 已停
### 结论
- v1 号码线：美国 +1、英国 +44（无实名、渠道最多）；荷兰 +31 可作低价第三选（无实名、esim.tech 有）
- v2：日本（护照 KYC，~$115/年 成本高）、泰国/马来西亚（护照 KYC，待验证渠道）、香港（1GLOBAL，监管收紧）
- 必做 pilot：esim.tech 询价；Telnyx 自助开 1 张测 enable_voice+SMS；买 Ultra PayGo / giffgaff eSIM 做对照；测 Google/Telegram/WhatsApp/微信 接码 + 大陆漫游收码 + 闲置保号
