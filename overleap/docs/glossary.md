# Glossary

The single source of names. Code identifiers, API fields, database columns, UI copy and documents use these terms exactly as written here. If a concept has no entry, add one here first, then use it. Do not introduce synonyms.

The Chinese column is only for mapping to conversations and to `research/`; it never appears in code or docs.

## People and roles

| Term | Definition | 中文 | Do not say |
|---|---|---|---|
| **Member** | Any account on Overleap. A Member can be a Host, a Guest, or both. | 会员 / 账号 | user, customer |
| **Host** | A Member who owns at least one Box and lists it on the Marketplace. | 盒子主人 / 主机 | provider, node operator, owner |
| **Guest** | A Member who holds at least one Seat on someone else's Box. Accepts the Usage Undertaking when buying a Seat. Has a Sealed Identity only after an Identity Submission. | 使用方 | client, consumer, tenant |
| **Referrer** | The Member who brought a Guest in. May be the Host of the Box the Guest buys a Seat on. | 推荐人 | affiliate |
| **Platform** | Overleap as operator: the Marketplace, billing, payouts, the Relay, Center, support. Legally a Wyoming, US company. The Platform is the seller of every Seat: the Guest pays the Platform, and the Platform pays Hosts and Referrers as its suppliers. | 平台 | we, the company (in specs) |
| **Keyholder** | One of the three named people who each hold one key share of the Committee key. | 委员 | trustee, guardian |
| **Committee** | The three Keyholders acting together. Any two can perform a Disclosure. | 委员会 | council, board, DAO |

## The device

| Term | Definition | 中文 | Do not say |
|---|---|---|---|
| **Box** | The Overleap appliance: a Dell Wyse 3040 running the Overleap Image, wired to the Host's home router. | 盒子 | router, node, device, miner |
| **Seed Box** | A Box operated by the Platform itself during cold start. | 种子盒子 | |
| **Image** | The complete, signed operating-system image a Box runs. | 镜像 | firmware, ROM |
| **Slot** | One of the two system partitions (A and B) on a Box. One is active; an Update writes the other. | 系统分区 | |
| **Update** | Replacing the inactive Slot with a new Image and switching to it, with automatic Rollback. | 升级 / OTA | upgrade, flash |
| **Rollback** | Automatic return to the previous Slot when a new Image fails its boot or health check. | 回滚 | |
| **Rollout** | Staged release of an Image: Seed Boxes, then 10% of Boxes, then all. | 分批放量 | |
| **Box Agent** | The single Go daemon on a Box that does everything Overleap-specific. | 守护进程 | client, node software |
| **Claim** | The one-time act of a Host binding a Box to their account by scanning the QR code on the Box. | 认领 / 绑定 | activate, register, pair |
| **Gateway Mode** | A Box acting as the default gateway for those LAN devices the Host points at it. The home router is not replaced. | 旁路网关 | bypass router |
| **Center** | The control plane: accounts, Marketplace, Seats, ledgers, Claim, Telemetry intake, and telling a Guest which Node its Box is linked to. | 控制面 | backend, server |
| **Telemetry** | Operational data a Box reports to Center. Never contains a Destination. | 运行数据 | logs (for this meaning) |
| **Diagnostic Bundle** | Detailed logs a Host explicitly chooses to send. | 诊断包 | |
| **Support Session** | Time-limited remote access to a Box that the Host has switched on. | 远程登录 | backdoor, remote shell |

## The marketplace

| Term | Definition | 中文 | Do not say |
|---|---|---|---|
| **Marketplace** | Where Hosts list Boxes and Guests choose one. | 市场 | store, pool |
| **Listing** | A Box's public page: country, measured Uplink, Direct Rate, price, uptime, free Load. | 盒子页面 / 挂出来 | offer, ad |
| **Seat** | A Guest's monthly right to send traffic through one specific Box, with a Data Allowance. | 席位 | subscription, plan, slot, license |
| **Seat Type** | `lite`, `standard` or `dedicated`. | 轻量 / 标准 / 独享 | tier, plan |
| **Data Allowance** | Traffic included in a Seat per month: 5 / 50 / 200 GB. | 流量额度 | quota, cap |
| **Throttle** | Limiting a Seat to 256 kbps once its Data Allowance is used up. The Seat keeps working. | 降速 | cut off, suspend |
| **Top-up** | Extra Data Allowance bought for the current month. | 流量包 / 加购 | |
| **Load Unit (LU)** | The unit of Box capacity and of pricing. `lite` = 0.33, `standard` = 1, `dedicated` = 1.5. | 负载单位 | |
| **Load** | Sum of the Load Units of all active Seats on a Box. | 负载 | |
| **Target Load** | Per-country parameter: the Load at which a Host's Host Share covers their broadband bill. | 目标负载 | |
| **Sellable Load** | The most Load a Box may sell: `min(5, uplink_mbps × 0.7 ÷ 5)`. The cap of 5 is reached at about 36 Mbps. | 可卖负载上限 | capacity |
| **Uplink** | A Box's upload speed in Mbps as measured by the Platform. Never self-reported. | 上行带宽 | upload, bandwidth |
| **Reference Price** | The Platform's suggested monthly price for a Seat in a country. | 建议价 | list price, MSRP |
| **Host Price** | The price a Host actually sets for their Listing, within 0.5×–2× the Reference Price. | 主人定价 | |

## Money

| Term | Definition | 中文 | Do not say |
|---|---|---|---|
| **Seat Revenue** | The Host Price of a Seat for a month. All of it is the Platform's revenue. Discounts funded by the Platform (such as the 30%-off first month) do not reduce it; they come out of the Platform Share. | 席位实收 | |
| **Platform Share** | 40% of Seat Revenue, plus the Referral Share when there is no Referrer. | 平台分成 | commission, fee, cut |
| **Host Share** | 50% of Seat Revenue: the rate at which the Platform pays the Host for capacity. A cost of the Platform, not a share of profit. | 主人分成 | |
| **Referral Share** | 10% of Seat Revenue, paid to the Referrer for as long as the Guest keeps paying. | 推荐分成 | affiliate fee |
| **Relay Fee** | A deduction from Host Earnings for Relayed traffic. Set to 0 in version 1, because every Session is Relayed and the Host has no way to avoid it. | 中转费 | |
| **Host Earnings** | Host Share + own Referral Share − Relay Fee, per Box per month. | 主人收入 | income, reward |
| **Statement** | The monthly per-Box breakdown shown to the Host. | 明细 | invoice |
| **Balance** | A Member's unpaid Host Earnings and Referral Share. Can pay for the Member's own Seats. | 余额 | wallet, credit |
| **Payout** | A transfer of Balance to a Host's Payout Address, in USDC. Monthly, minimum $10. | 打款 | withdrawal, reward |
| **Payout Address** | The USDC address a Member provides, usually an exchange deposit address. | 收款地址 | wallet |
| **Contribution Ledger** | The append-only record of what each Box contributed: time, traffic, uptime, country, amount paid. A service record with no promised value. | 贡献流水 | points, credits, token |

## Connections

| Term | Definition | 中文 | Do not say |
|---|---|---|---|
| **Session** | One continuous connection of a Guest to a Box. | 连接 | |
| **Direct** | A Session whose traffic flows Guest ↔ Box without passing through a Node. Not in version 1. | 直连 / 打洞成功 | P2P |
| **Relayed** | A Session whose traffic passes through a Node. In version 1 this is every Session. | 中转 | proxied |
| **Node** | A datacenter server running the k2 server. Guests and Boxes both connect to Nodes. | 节点 | relay server, VPS |
| **Box Link** | The standing outbound k2 connection a Box keeps to a Node. All of the Box's Guest traffic arrives over it. | 盒子到节点的连接 | reverse tunnel |
| **Relay** | The k2 protocol mode in which a Node forwards a Guest's streams to a Box over its Box Link instead of dialling the destination itself. In version 1 every Session is Relayed. | 中转 | TURN, proxy, connect |
| **Direct Rate** | The share of a Box's Sessions that were Direct, shown on its Listing. | 直连率 | hole-punch rate |
| **Destination** | The domain and IP a Guest's traffic goes to. | 访问的域名和 IP | |
| **Egress Policy** | What a Box refuses to carry (P2P file sharing, high-risk ports) and how sites are grouped. | 出口策略 | filter, blocklist |
| **Site Group** | A site together with all its CDN and API domains. Traffic for one Site Group always leaves from one Box. | 网站组 | |
| **Host Priority** | The Host's own traffic always goes first; shared traffic never exceeds 70% of Uplink. | 主人优先 | QoS |

## Identity and disclosure

| Term | Definition | 中文 | Do not say |
|---|---|---|---|
| **Usage Undertaking** | What a Guest accepts when buying a Seat: to obey the law of the country the Box is in, and to bear the liability and compensate the Host if they do not. Made to the Platform, with the Host as beneficiary; Guest and Host never learn each other's identity. | 使用承诺 | P2P contract, terms (for this meaning) |
| **Identity Submission** | A Guest uploading an identity document and a photo taken live with their camera. Required to lift an Account Freeze, and before buying a Seat on a Box whose Host requires it. The Platform does not check it against any registry. | 提交资料 | Identity Verification, KYC, verified |
| **Source Address** | The IP address a Guest connects from. Part of the Connection Record. | 来源 IP | |
| **Sealed** | Encrypted to the Committee key at the moment of writing. The Platform alone cannot read it. | 加密封存 | encrypted (when this is meant) |
| **Sealed Identity** | A Guest's Identity Submission, Sealed. The Platform keeps no other copy. | 实名密文 | |
| **Connection Record** | A Sealed record of a Session: Member, Seat, Box, Source Address, time, Destinations. Written on the Node side and the Box side; hashes go to the Transparency Log. Kept 24 months. Never contains content. | 连接记录 | log, access log |
| **Charter** | The Committee's written rules. Core rule: respond only to Valid US Legal Process. | 章程 | policy |
| **Valid US Legal Process** | An order, subpoena or warrant issued by a US court, including one that results from a foreign request made through mutual legal assistance. | 对美国公司有效的文书 | |
| **Disclosure** | Two Keyholders unsealing specific Sealed data in response to Valid US Legal Process. | 解密 / 披露 | unmasking, de-anonymization |
| **Transparency Log** | The public on-chain record, as hashes, of every Disclosure request and vote and of batches of Connection Records. | 链上存证 | |
| **Cut-off** | A Host instantly ending a Session on their Box. Triggers an Account Freeze. | 一键熔断 | kill switch, ban |
| **Account Freeze** | The Platform suspending a Guest after a Cut-off. Lifted only after an Identity Submission and review. | 冻结 | |
| **Host Notice** | The plain-language statement a Host accepts before Claim: what is shared, what is recorded, that Guests are not identity-checked by default, ISP terms of service, and that the Host assesses their own legal risk. | 告知书 | disclosure notice, disclaimer |
| **Host Attestation** | A signed statement that a Box was carrying third-party traffic at a given time. Needs no Disclosure and names no one. | 证明信 | |

## Words we never use externally
"unblock", "bypass", "anonymous", "untraceable", "no logs", "even we can't know", "token", "airdrop", "mining", "passive income guaranteed", and the name of the sister brand.
