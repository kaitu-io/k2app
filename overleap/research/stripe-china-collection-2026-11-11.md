# Stripe 能否向中国大陆付款人收款（2026-11-11）

调研人：Claude（直接打开 Stripe 官方文档核对）。目的：决定大陆使用方怎么付款。

## 结论
1. **支付宝和微信支付都明文禁止 VPN 类业务。** 不能用。
2. **两者都不支持 Stripe Billing 的订阅扣款**，只支持单次付款；连续扣款是需要单独申请的内测。
3. **银行卡可以**：美国 Stripe 账户默认可收 Visa、Mastercard 和中国银联（China UnionPay）卡，付款人所在地不限。
4. Stripe 自己的受限业务清单里没有看到 VPN 或代理，但这一条只看了一页，**未向 Stripe 确认**。

## 逐条依据

| 说法 | 结果 | 出处 |
|---|---|---|
| 美国 Stripe 账户可开通支付宝 | ✅ 商户所在国含 US；付款人所在地 CN；可用 USD 或 CNY 标价 | https://docs.stripe.com/payments/alipay |
| 支付宝支持订阅 | ❌ "Recurring payments: Not supported"；Billing 不支持；连续扣款为 invite-only 内测，需支付宝逐个批准 | 同上 |
| 支付宝允许 VPN 业务 | ❌ 禁止清单第 62 项："VPN and other illegal proxy services"；第 79 项：其他违反中国法律法规的商品或服务 | https://stripe.com/legal/alipay |
| 美国 Stripe 账户可开通微信支付 | ✅ 商户所在国含 US；仅 B2C；付款人所在地 CN | https://docs.stripe.com/payments/wechat-pay |
| 微信支付支持订阅 | ❌ 不支持；Billing 不支持；Checkout 订阅模式不支持；连续扣款为内测 | 同上 |
| 微信支付允许 VPN 业务 | ❌ 禁止类目第 38 项："Virtual Private Network, Virtual Private Server"；第 82 项：违反中国法律法规 | https://pay.weixin.qq.com/index.php/public/wechatpay_en/proper_rule |
| 美国 Stripe 账户可收银联卡 | ✅ China UnionPay：账户国家含 United States，持卡人国家 Global，支持 3D Secure | https://docs.stripe.com/payments/cards/supported-card-brands |
| Stripe 本身禁止 VPN/代理 | ❓ 受限业务页未列出 VPN 或代理（该页由工具摘要，未逐字通读）；未向 Stripe 确认 | https://stripe.com/legal/restricted-businesses |
| 商户能拿到支付宝/微信付款人的真实姓名 | ❓ 文档只提到可读取对方的 transaction_id，未见姓名字段 | 两个付款方式页面 |

## 其他要点
- 开通支付宝或微信支付后，账户可能被复核；网站必须公开展示所售服务和价格。也就是说对方会看到我们卖的是什么。
- 支付宝和微信支付都没有拒付（chargeback）流程。
- D-14 早已定过"不用中国支付渠道"，本次结果与之一致。

## 对方案的含义
- 大陆使用方用 Stripe 付款只能刷卡：境外卡，或境内银行发的 Visa/Mastercard/银联卡。境内卡用于这类境外商户的实际成功率未查到数据。
- 没有可用银行卡的大陆使用方需要另一种付款方式；D-37 提到过加密货币钱包。Stripe 的稳定币收款是否对我们开放未查。

---

# 补充：Stripe 能否给中国大陆的主人打款（2026-11-11）

调研人：Claude（直接打开 Stripe 官方文档核对）。目的：决定大陆主人怎么拿到钱。

## 结论
**Stripe 不能把 USDC 打给中国大陆的收款人。** 两条稳定币打款产品的收款国家清单里都没有 CN。Stripe 对大陆个人只有一条路：美元电汇到银行账户，而且是内测。

| 说法 | 结果 | 出处 |
|---|---|---|
| Global Payouts 打到加密钱包，支持大陆收款人 | ❌ 收款国家清单（160 多个）含 HK、TH、SG、JP、GB、US 等，**不含 CN**，也不含 TW | https://docs.stripe.com/global-payouts/send-money/stablecoins |
| Connect 稳定币打款，支持大陆收款人 | ❌ 约 67 国清单，不含 CN、HK；且为内测，仅个人 | https://docs.stripe.com/connect/stablecoin-payouts |
| Stripe 能以别的方式付给大陆个人 | ⚠️ 仅美元电汇（private preview），需收款人邮箱、姓名、地址、出生日期、电话 | https://docs.stripe.com/global-payouts/recipient-requirements |
| 钱包打款需要收款人提供什么 | ✅ 邮箱、姓名、地址；仅美国企业可用（纽约除外）；公开预览；USDC 支持 Base、Solana 等 | 前两个链接 |
| 普通跨境打款（Connect cross-border）支持大陆 | ❌ 仅美、英、欧洲经济区、加拿大、瑞士之间 | https://docs.stripe.com/connect/cross-border-payouts |

费率仍未查到。电汇的手续费和最低金额未查到。
