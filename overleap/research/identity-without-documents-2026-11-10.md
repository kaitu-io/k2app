*调研日 2026-11-10。由 Claude 的调研子任务通过网页搜索完成，原文未改。标“我的判断”的是推断，标“未核实”的没有查到一手来源。这不是法律意见。*

**结论先行：美国联邦和怀俄明州都没有要求 VPN/代理服务商对客户做 KYC 的法律，唯一硬性义务是 OFAC 制裁合规。对过不了证件 KYC 的大陆访客，可行的是"护照 NFC 或低价证件核验 + 钱包押金 + 收紧出口"的分层，纯钱包不足以保护主机。** 下文未核实项和我的判断都已标注。

## 1. 法律义务

- **FinCEN/BSA**：为自己购买商品或服务而取得、使用虚拟货币的人是 "user"，不是 MSB。仅为销售自身商品或服务而收付资金另有豁免，即 31 CFR 1010.100(ff)(5)(ii)(F)，但 FinCEN 对豁免从严解释。来源：[FIN-2019-G001](https://www.fincen.gov/sites/default/files/2019-05/FinCEN%20Guidance%20CVC%20FINAL%20508.pdf)。
- **我的判断**：收加密货币卖自家订阅不构成 MSB。"访客付款、公司转付主机 USDC"若被看成代访客向主机转款则有风险，需律师确认。
- **怀俄明**：W.S. 40-22-104(a)(vi) 把虚拟货币的买卖、发行、托管和接收转移排除在货币转移牌照之外（[条文](https://codes.findlaw.com/wy/title-40-trade-and-commerce/wy-st-sect-40-22-104/)）。2026 年有修订草案，是否通过未核实（[草案](https://wyoleg.gov/InterimCommittee/2025/S19-2025092226LSO-0143v0.5.pdf)）。
- **IaaS 客户识别规则**：BIS 于 2024-01 提出，适用于美国 IaaS 提供商及其外国转售商（[Miller & Chevalier](https://www.millerchevalier.com/publication/trade-compliance-flash-bis-issues-notice-proposed-rulemaking-focusing-infrastructure)）。二手来源称截至 2026-05 仍未定稿（[来源](https://blog.voveid.com/kyc-for-cloud-and-ai-providers-what-the-us-iaas-rule-means-for-llm-gateways/)）；我在 Federal Register 检索未见最终规则，2026-11 的状态未核实。草案定义是否涵盖代理服务，我凭记忆认为涵盖，未核实。
- **CALEA**：义务针对电信运营商，"信息服务"被排除（[EDUCAUSE](https://er.educause.edu/~/media/files/articles/2005/1/epo0420.pdf?la=en)）。未找到专门针对 VPN 的权威来源；不适用是我的判断。
- **州法**：犹他州 SB73 是首个直接针对 VPN 的州法，据报道执行被冻结，内容未核实（[TechSpot](https://www.techspot.com/news/113725-utah-becomes-first-us-state-target-vpns-new.html)）。
- **OFAC（强制）**：所有美国实体必须遵守，民事处罚一般按严格责任，合规方案可按公司规模调整。建议控制包括按 IP 地理位置封锁受制裁地区、对客户和钱包地址筛查 SDN 名单（[OFAC 指南](https://ofac.treasury.gov/media/913571/download?inline)）。
- **小公司最低做法（我的判断）**：封锁受制裁地区 IP，注册和收款时筛查钱包地址，条款写明禁止，保留记录。

## 2. Stripe Identity 与替代商

- **覆盖**：证件核验名单含 CN、HK、TW，护照支持最广，但不支持提取中文字段（[Stripe 文档](https://docs.stripe.com/identity/verification-checks?type=document)）。
- **失败原因（我的判断，无一手数据）**：身份证是中文字段，多数人没有护照，不愿上传证件，页面在大陆加载差。
- **替代商价格**：

| 供应商 | 每次价格 | 备注 |
|---|---|---|
| Didit | 全套 $0.33，NFC $0.15 | 每月送 $10 额度，无月费（[定价](https://docs.didit.me/getting-started/pricing.md)） |
| Sumsub | $1.35–1.85 | 月最低 $149/$299（[定价](https://sumsub.com/pricing/)）；NFC 列表含中国护照（[文档](https://docs.sumsub.com/docs/nfc-supported-documents)） |
| Veriff | 约 $0.80–2 | 二手数据（[比较](https://apidog.com/blog/best-kyc-api/)） |
| Persona | 定制报价 | 二手数据，同上 |

Didit 对中国居民身份证的支持未核实。

## 3. 钱包与链上身份

- **a. 自托管钱包付 USDC**：只给出地址，能否找到人取决于资金是否经过做了 KYC 的交易所。Chainalysis 旧数据称约一半非法资金最终到交易所，直接转入的比例据报道从 40% 降到 15%（二手：[Unchained](https://unchainedcrypto.com/50-of-illicit-funds-end-up-at-centralized-crypto-exchanges-chainalysis/)、[Cointelegraph](https://cointelegraph.com/news/crypto-launderers-are-turning-away-from-centralized-exchanges-chainalysis)）。大陆用户多经场外换币，链条常断（我的判断）。发行方可冻结：2023–2025 年 Tether 冻结约 $3.29 亿×10（即 $32.9 亿），Circle 约 $1.09 亿（[AMLBot](https://blog.amlbot.com/stablecoin-freezes-2023-2025-a-data-backed-analysis-of-usdt-usdc-by-amlbot/)）。
- **b. 交易所支付产品**：Binance Pay 有付款人核验接口，商户提交姓名和生日由 Binance 比对，仅限白名单商户（[文档](https://developers.binance.com/en/docs/products/binance-pay-merchant/api-order-payer-verification)）。OKX Pay、Bybit Pay 是否向商户提供身份未找到来源。Binance 2021 年起停止大陆新注册（[SCMP](https://amp.scmp.com/business/companies/article/3150149/bitcoin-exchanges-binance-and-huobi-stop-taking-mainland-chinese)），现行规则未核实。
- **c. 链上凭证**：
  - Coinbase Verifications 证明钱包关联一个可交易的 Coinbase 账户，可选居住国，链上不含个人信息（[GitHub](https://github.com/coinbase/verifications)）。Coinbase 是美国公司，凭法律程序可向其调取（我的判断）。大陆居民能否开户未核实，我判断不能。
  - BABT 证明通过了 Binance KYC（[The Defiant](https://thedefiant.io/news/cefi/binance-launches-proof-of-kyc-token-that-can-be-used-across-platforms)）；Binance 在美国境外，调取路径弱（我的判断）。
  - World ID 在大陆无官方服务，有凭证黑市（[Nasdaq](https://www.nasdaq.com/articles/black-market-for-worldcoin-credentials-pops-up-in-china)、[Biometric Update](https://www.biometricupdate.com/202508/china-warns-against-submitting-biometric-data-in-return-for-crypto-hinting-at-world)）。
  - zkPassport 和 Self 只支持符合 ICAO 9303 且签发国公布签名证书的证件（[zkPassport](https://docs.zkpassport.id/limitations)、[Self](https://github.com/selfxyz/self)）。中国护照是否在列未能核实；居民身份证不是 ICAO 证件，应不支持（我的判断）。零知识证明本身不给出姓名，除非要求披露并自行加密保存。
  - Galxe、Gitcoin/Human Passport、Civic、Privado 本次未调研。
- **d. 押金**：未找到可比的先例数据。我的判断：能抑制批量滥用，不能识别人，对严重犯罪无威慑。
- **e. 手机号、卡指纹、Apple/Google 登录**：本次未调研。我的判断：大陆手机号实名，但美国法律程序够不到中国运营商；Apple/Google 可向美国公司调取，强度中等。

## 4. 同类做法

- **Bright Data**：住宅代理只对注册公司开放，人工审核，可要求联系人证件和视频通话；新规适用于 2026-07-07 之后新建的 zone（[文档](https://docs.brightdata.com/products/residential/network-access)）。
- **SOAX**：身份核验是付费使用的前提，要求可随支付方式变化（[政策](https://www.soax.com/legal/kyc)）。
- **IPRoyal**：购买阶段核验（[政策](https://iproyal.com/kyc-policy/)）。
- **Oxylabs**：只查到部分功能需 KYC（[指南](https://oxylabs.io/blog/residential-proxies-quick-start-guide)）；各家对加密付款人的专门规定未找到。
- **Mysterium**：默认白名单，计划只放行通过 KYC 的合作方（[说明](https://medium.com/mysterium-network/mysteriums-whitelisting-policy-and-what-it-means-for-you-e1ed245ef8e6)）。2020 年德国一名节点主机收到版权索赔，团队协助取日志后结案（[博文](https://www.mysterium.network/post/mysterium-node-runner-settles-claim-in-germany)），这是我找到的最接近"平台帮主机脱责"的案例。Orchid、Sentinel 未查到资料。
- **Tor**：EFF 建议不要在家里跑出口节点（[Vice](https://www.vice.com/en/article/the-operators/)）；家庭出口被搜查有先例（[Sophos](https://news.sophos.com/en-us/2016/04/07/couple-hosting-tor-exit-node-raided-by-cops-investigating-child-abuse/)）。
- 未找到"因平台指认真实用户而使住宅主机被正式排除嫌疑"的刑事案例。

## 5. 分层与推荐（我的判断）

交易所分级限额的具体做法本次未调研。按"识别强度 / 大陆访客摩擦 / 成本 / 开发量"排序：

1. **护照 NFC 或低价证件核验**（Didit $0.15–0.33）：强 / 中（需要护照）/ 低 / 低。身份自己加密保存，美国程序可直接调取。
2. **Coinbase 凭证加钱包**：中强 / 高（大陆基本拿不到）/ 零 / 低。适合海外访客。
3. **交易所付款人核验**（Binance Pay）：中 / 中 / 低 / 中。需申请白名单，调取路径在美国境外。
4. **钱包加押金，收紧出口**（封 SMTP 和 P2P、低流量、只出到明示接受此层的主机）：弱 / 最低 / 零 / 中。只有 1–2 层对主机有实质保护，这一层靠出口策略和完整连接日志兜底。

**需要一次性问律师的**：
- 访客付款转付主机的 MSB 定性；
- IaaS 规则若定稿是否涵盖本业务；
- 加密保存身份并"仅凭有效法律程序披露"的条款写法，以及数据泄露责任；
- 服务大陆用户的 OFAC 和出口管制边界。
