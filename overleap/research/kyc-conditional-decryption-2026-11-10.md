*调研日 2026-11-10。由 Claude 的调研子任务通过网页搜索完成，原文未改。多数数字来自二手来源；标“未核实”“判断”的不能当依据。*

# 调研报告：加密身份托管与有条件解密（2026-11-10）

**结论：最可行的是「独立 KYC 托管方 + 具名委员会门限密钥」，链只用来存证，不用代币投票。** 这基本就是 Concordium 的模型。标「未核实」的是没找到一手来源的；标「我的估计/判断」的不是查证事实。

## 1. 验证真实身份，但平台不持有明文

| 方案 | 要钱包吗 | 覆盖 | 价格 | 能否产出加密身份托管 |
|---|---|---|---|---|
| zkPassport | 用手机 App 扫 NFC 芯片，文档未提钱包 | 「大多数」符合 ICAO 9303 的护照、身份证、居留许可；无国家数；有用户报告个别证件失败 | 未核实 | 可选择性披露姓名，并给每个服务一个唯一标识；未见现成的「加密给第三方」功能 |
| Self（原 OpenPassport） | App 免费；是否要钱包未核实 | 电子护照、ICAO 生物识别身份证、Aadhaar；国家数说法不一（60+ / 174+） | 按 credit 计，美元价未核实 | 同上，未见托管功能 |
| Human ID「Proof of Clean Hands」（原 Holonym） | 文档没说；凭证是链上 attestation | 护照、驾照等；国家清单未给 | 未核实 | **最接近现成品**：姓名和生日在证明内加密给门限网络，链上 `canDecrypt` 放行才解密 |
| World ID 护照凭证 | 需 World App | 试点为智利、哥伦比亚、马来西亚、韩国，后有扩展；当前清单未核实 | 未核实 | 无 |
| Rarimo | App 本身是自托管钱包 | 未找到 2026 年国家清单 | 未核实 | 未核实 |
| Privado ID、zkMe | 未核实 | 未核实 | 未核实 | 未核实 |
| 传统 KYC（Stripe Identity、Sumsub、Veriff、Persona） | 不要 | Persona 的 NFC 流程称 120+ 国 | 约 $1.35–1.89/次（第三方汇总）；Persona 不公开单价 | 无；数据留在厂商处 |

- **Human ID 的限制**：当前部署的放行条件是单一授权方，每 24 小时最多 10 次；多签、DAO、法院令哈希都只是规划。
- **传统 KYC 的漏洞**（我的判断，未查厂商文档）：厂商账户若在公司名下，公司能用 API 读明文，「公司单独读不到」就不成立。账户必须由独立托管方持有。

来源：
- https://docs.zkpassport.id/faq ，https://zkpassport.featurebase.app
- https://docs.self.xyz/
- https://docs.human.tech/proof-of-clean-hands
- https://www.biometricupdate.com/202512/world-id-keeps-concert-tickets-from-scalpers-and-bots-as-credentials-expand
- https://www.coinspeaker.com/rarimo-rarime-passport-verification-web3
- https://withpersona.com/product/verifications/nfc
- https://costbench.com/software/kyc-aml/sumsub/ ，https://www.xpay.sh/saas-pricing/veriff/

## 2. 有条件解密

- **Lit Protocol**：风险的实例。v1 主网 Naga 在 2026-03-25 停止、04-01 完全下线，转向 v3。
  - v3 是基于 TEE（Intel TDX）的架构，不再明确是门限网络。
  - 支持按链上合约状态放行解密，价格 $0.01/秒。
  - 旧密文如何迁移，公告没写。
  - 来源：https://spark.litprotocol.com/naga-network-sunset ，https://spark.litprotocol.com/lit-v3-is-live-confidential-compute-and-key-management-in-one-api/
- **Threshold TACo**：由质押节点组成的 cohort 检查条件后放出解密材料。主网定价和 cohort 规模未核实（文档页 404）。来源：https://docs.threshold.network/applications/threshold-access-control
- **Shutter API**：2025-03 上线，可按时间或事件触发解密，运行在 Gnosis Chain。来源：https://www.globenewswire.com/news-release/2025/03/12/3041241/0/en/Shutter-Network-Launches-Shutter-API-Decentralized-Threshold-Encryption-That-Paves-the-Way-for-Trustless-Web3.html
- **Fairblock**：未找到资料。
- **drand 时间锁**：生产可用，但只能按时间解密，不能按投票结果，不适合本场景。来源：https://docs.drand.love/docs/timelock-encryption/
- **Safe 多签 + 线下 Shamir/FROST 分片**：本次未检索。我的判断：不依赖任何第三方网络存活，风险只剩分片丢失，靠 3-of-5 的冗余覆盖。

## 3. 先例

**Concordium 最贴近**（官方流程文档）：
1. 身份提供方（IDP，第三方机构）做 KYC 并保存身份记录，但不知道用户的链上账户。
2. 链上只存加密的「持有人标识」，不存任何个人信息。
3. 隐私守护人（通常是律所）各持一份密钥，门限 2-of-3。
4. 执法方先拿到守护人所在法域的法院令，守护人各解一份，执法方合成标识。
5. 执法方再拿标识去找 IDP 调出身份；IDP 在别的法域时要第二张法院令。
6. 各方只和执法方通信，彼此不直连。

耗时和是否实际用过，官方没有公布（未核实）。
来源：https://docs.concordium.com/governance/identity/identity_processes.pdf ，https://docs.concordium.com/en/mainnet/docs/protocol/identity-disclosure-processes.html

其他先例：
- **Railgun PPOI、Privacy Pools**：只证明资金不来自黑名单，不提供去匿名。来源：https://docs.railgun.org/wiki/assurance/private-proofs-of-innocence
- **Hinkal**：用户自己持有查看密钥，自愿披露。来源：https://hinkal-team.gitbook.io/hinkal/introduction/compliance
- **Panther、Aztec 的委员会提案、Namada、Espresso CAPE**：未找到一手材料，未核实。

## 4. 去中心化裁决

- **Kleros**：第三方数据称平均 18–23 天；费用从官方说的几十美元到第三方说的 $50–500，无官方统计。来源：https://blog.kleros.io/kleros-enterprise
- **UMA oSnap**：挑战期各 DAO 设 1–3 天，有争议时再加 2–4 天。来源：https://blog.uma.xyz/articles/how-umas-optimistic-oracle-resolves-osnap-disputes
- **Snapshot 屏蔽投票**：只在投票期间隐藏票，不隐藏提案内容。来源：https://decrypt.co/105201/snapshot-adds-shielded-voting-daos-help-solve-voter-apathy
- **保密问题**（我的判断）：执法请求通常不能公开，投票人会看到案情，代币投票因此不现实。Concordium 也选了具名律所而非代币持有人。

## 5. 各架构对比（均为我的估计）

| 架构 | 非币圈用户注册 | 解密耗时 | 单次成本 | 2–3 人搭建 |
|---|---|---|---|---|
| A. 独立托管方持 KYC + 委员会门限加密「账户↔KYC 编号」 | 网页拍证件，2–3 分钟 | 数小时到数天，取决于凑齐委员 | 约 $1.5 | 3–6 周 |
| B. Human ID + 自定义放行合约 | 需装 App，可能涉及钱包 | 合约放行后分钟级 | 未知 | 4–8 周，依赖其网络存活 |
| C. zk 护照 + 自写可验证加密电路 | 装 App、需 NFC 护照 | 同 A | 低 | 3 个月以上，需审计 |
| D. 代币 DAO 投票放行 | 同上 | 数天到数周 | 高 | 不建议 |

## 建议

**第一期上 A**，分工如下：
- **托管方**：独立法律实体持有 KYC 厂商账户，公司没有读取权限。
- **委员会**：5 名具名委员（律师、隐私组织、主机代表等）各持一份密钥，门限 3-of-5，公司最多占 1 份。
- **公司**：只存用委员会公钥加密的「账户↔KYC 编号」。
- **链**：每次请求和表决只把哈希写上链（Safe 多签），再定期出透明度报告。

**第二期**：加 zk 护照作为可选通道，让有 NFC 护照的用户连托管方都不必信任。

**第三期**：Human Network 或 TACo 运行稳定、定价明确后，把放行逻辑迁到链上合约。

**仍不确定**：
- zk 方案各家的价格和国家清单。
- Human Network 自定义放行合约在主网是否可用。
- TACo 定价。
- 各国执法请求如何鉴真。
- 独立托管方的合同能否真正挡住公司读取。
- 委员失联或密钥丢失后的轮换流程。
