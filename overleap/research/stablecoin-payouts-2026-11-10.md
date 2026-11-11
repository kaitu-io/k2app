*调研日 2026-11-10。由 Claude 的调研子任务通过网页搜索完成，原文未改。多数数字来自二手来源；标“未核实”“判断”的不能当依据。*

# 稳定币结算调研（2026-11-10）

多数数字来自二手或厂商博客，已逐条标注；标"未核实"的请勿直接写入决策。

## 1. 同类项目怎么付
- **Honeygain**：最低 $20，PayPal 或自家 JMPT 代币，不付 USDT/USDC（[官方](https://www.honeygain.com/honeygain-payout)）。
- **Pawns**：最低 $5，PayPal、BTC、礼品卡；**EarnApp**：门槛有 $2.5 和 $10 两种说法。两者均只有第三方来源，未核实（[来源](https://www.honeygain.com/blog/bandwidth-sharing-apps/)）。
- **Mysterium**：付自家 MYST（Polygon），满 5 MYST 自动结算，网络抽 20%，链上费另计（[官方 FAQ](https://docs.mysterium.network/faq)）。
- **Grass / Nodepay / Helium**：都是积分或自家代币，在 Solana 上领取。Grass 按季空投、兑换率不固定（[来源](https://ownyourmind.pages.dev/use/how-to-earn-with-grass/)）；Nodepay 发 NC（[来源](https://coinmarketcap.com/academy/article/nodepay-airdrop-guide-everything-you-need-to-know)）；Helium 发 HNT（[来源](https://millionminer.com/news/helium-mining-2026-complete-operator-guide)）。
- **结论**：没查到哪家用稳定币作为主结算。DePIN 付自家代币，Web2 类付 PayPal。

## 2. 币和链
- **USDC on Solana**：每笔约 $0.0001–0.01。收款方首次收 USDC 需建代币账户，押金约 0.00204 SOL，通常由发送方付（[费用](https://www.spark.money/tools/stablecoin-fee-calculator)、[押金](https://cobo.com/developers/v2/guides/transactions/solana-token-account-rent)）。
- **USDC on Base / Polygon**：约 $0.01–0.05（[来源](https://www.spark.money/tools/stablecoin-fee-calculator)）。Arbitrum 数字互相矛盾，未核实。
- **USDT on Tron**：租能量 $0.20–0.96，直接烧 TRX $1–5（[来源](https://eco.com/support/en/articles/15247716)）。对 $5–30 的付款占比太高，但新兴市场小额转账用它最多，占 52–65%（[来源](https://news.bitcoin.com/stablecoins/tron-captures-52-of-low-value-usdt-transfers-globally/)）。
- **交易所充值支持**：Binance 的 USDC 网络据第三方称含 Solana、Polygon、Arbitrum，是否支持 Base 充值未核实（[来源](https://www.bitget.cloud/academy/circle-usdc-network)）。
- **批量工具**：Streamflow 支持 CSV 批量付款（[文档](https://docs.streamflow.finance/en/articles/12639121-payouts)）；Solana 官方有批量支付文档（[文档](https://solana.com/it/docs/payments/send-payments/payment-processing/batch-payments)）。

## 3. 没用过钱包的人
- **嵌入式钱包**：Coinbase CDP 每月 5,000 次操作免费，支持邮箱、短信登录（[定价](https://www.coinbase.com/developer-platform/pricing)）；Privy 起步含 5 万次免费签名（[定价](https://privy.io/pricing)）。其余厂商价格只有第三方对比，未核实。
- **最省事的路径**：主机在本国交易所注册（交易所做 KYC），把 USDC 充值地址贴给我们，我们打款，他在交易所卖出提现到银行。
- **嵌入式钱包的短板**：只解决"收"，出金仍要走交易所或出金商。建议作为没有交易所账户者的备选。

## 4. 代付处理商
- **Stripe Global Payouts**：付到钱包（公开预览），USDC 支持 Base、Solana、Polygon、Arbitrum 等，覆盖 160+ 市场，可用 USD 余额出资。**仅限美国企业**（[文档](https://docs.stripe.com/global-payouts/send-money/stablecoins)）。费率未查到。
- **Stripe 稳定币余额转本地银行**：私测，9 种货币（[同上](https://docs.stripe.com/global-payouts/send-money/stablecoins)）。
- **Stripe Connect 稳定币付款**：私测，仅美国平台，约 67 国（[文档](https://docs.stripe.com/connect/stablecoin-payouts)）。
- **Rise**：$49/承包商/月（[来源](https://www.riseworks.io/usdc-payroll)）。**Request Finance**：$600/月起（[定价](https://www.request.finance/pricing)）。两者对 $5–30 的付款都不划算。
- **Bridge**：无公开价目，非美企业能否开户未核实（[费用披露](https://www.bridge.xyz/legal/fee-disclosure-statement/overview)）。
- **Circle Mint**：对合格机构免费，审核需数周（[官方](https://www.circle.com/circle-mint)）。

## 5. 收订阅
- **卡进、稳定币出**：可行。Stripe 用 USD 余额直接付 USDC，但限美国实体；否则要自己把法币在交易所或 Circle 换成 USDC。换汇成本和美国卡费率均未核实。
- **稳定币订阅**：Stripe Billing 支持 USDC 订阅（Base、Polygon、Solana 等），客户全球可付，无拒付，结算为本地法币。商户限美国；欧盟、香港、墨西哥、瑞士为私测（[文档](https://docs.stripe.com/payments/stablecoin-payments)）。1.5% 费率仅见于单一二手来源，未核实（[来源](https://stablecoininsider.org/stripe-global-usdc-stablecoin-payments/)）。
- **判断**：现实做法是卡为主、稳定币为可选。

## 6. 要建的合规件
- **制裁筛查**：OFAC 2021 指引要求筛查名单上的钱包地址，名单更新后重筛，并做 IP 筛查（[解读](https://www.whitecase.com/publications/alert/ofac-sanctions-compliance-guidance-virtual-currency-industry)）。
- **受限国家**：中国、埃及、阿尔及利亚、孟加拉、尼泊尔、摩洛哥等。各榜单不一致，且多数限制的是银行和交易所；"个人收款是否违法"未核实（[来源](https://www.cloudwards.net/where-is-crypto-illegal)）。
- **税务申报**：DAC8 自 2026-01-01 生效，义务在加密服务商，首次交换在 2027-09 前（[RSM](https://rsmus.com/insights/tax-alerts/2025/dac8-and-carf-present-extensive-reporting-challenges-for-crypto-platforms.html)）。只付自己账款的公司是否算申报主体，未查到依据。
- **货币转移商**：FinCEN 2019 指引按事实认定。"付自己的应付款不算"这一点没能从原文确认，需律师看（[原文](https://www.fincen.gov/sites/default/files/2019-05/FinCEN%20CVC%20Guidance%20FINAL.pdf)）。

## 建议
**只付 USDC，主机自己提供收款地址，在 Solana 或 Base 里二选一；满 $10 按月批量付。**
- 费用是分级的，$5 的付款也不被吃掉。
- 这两条链 Stripe 都支持：有美国实体就用它代付，自己不持币；没有就自己从交易所买 USDC 批量发。
- 主机用交易所充值地址即可，KYC 和出金由交易所承担。
- 需要自建：地址收集与小额验证、制裁筛查、付款台账。
- 不建议把 Tron-USDT 作为主通道，手续费太高；如新兴市场主机强烈要求，可作为第二选项，费用由主机承担。

**仍不确定**
- 公司实体在哪国，这决定 Stripe 能不能用。
- Stripe 稳定币代付的费率。
- 各大交易所对 Base 充值的支持。
- 货币转移商和 DAC8 的定性。
- 法币换 USDC 的实际成本。
