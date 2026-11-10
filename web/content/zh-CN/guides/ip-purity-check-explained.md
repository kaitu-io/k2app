---
title: "IP 纯净度检测到底在测什么？各家分数为什么对不上（2026）"
date: 2026-11-10
summary: "IP 纯净度检测看的是 IP 归属类型、历史滥用记录、是否代理或 Tor、同 IP 用户数等几类数据，各站的数据源和算法不同，同一个 IP 结果可能相反。本文按 ping0、Scamalytics、IPQS、IPinfo 官方说明讲清每项指标，并附实测对照。"
tags: ["ip纯净度查询", "ip纯净度检测", "ip检测", "claude ip检测", "节点纯净度测试", "ip风控值"]
brand: kaitu
draft: false
section: kaitu
---

<strong>“IP 纯净度”不是一个统一的标准，而是各个检测网站根据自己的数据库给出的风险估计。</strong>它们看的主要是几类数据：这个 IP 属于家庭宽带还是机房、过去有没有扫描或发垃圾邮件之类的记录、是不是公开的代理或 Tor 节点、有多少人在共用。每家的数据源和算法都不一样，所以同一个 IP 在不同网站上的结果可能完全相反。另外，<strong>目前没有公开证据表明 Anthropic（Claude）或 OpenAI（ChatGPT）使用这些第三方网站的评分</strong>，分数只能作为参考，不能用来判断账号会不会出问题。

<em>本文引用的检测站说明和实测结果均为 2026 年 11 月 10 日查看，各站的算法和数据随时会更新。</em>

## IP 纯净度检测看的是哪几类数据

把 ping0、Scamalytics、IPQS（IPQualityScore）、IPinfo 这几家的官方说明放在一起，检测项可以归成六类：

| 数据类别 | 在问什么 | 各家怎么叫 |
|---|---|---|
| 归属类型 | 这个 IP 用在家庭宽带、手机网络、公司，还是机房服务器 | ping0「IP 类型」（家庭宽带 / IDC 机房）；IPQS `connection_type`（Residential、Corporate、Education、Mobile、Data Center）；IPinfo ASN `type`（isp、hosting、business、education、government）；Scamalytics「Datacenter」 |
| 历史风险 | 这个 IP 过去有没有扫描、暴力破解、爬虫、发垃圾邮件等行为 | ping0「风控值」；IPQS `fraud_score`、`recent_abuse`；Scamalytics「Fraud Score」 |
| 代理与匿名 | 是不是已知的代理、VPN 服务器或 Tor 出口 | IPQS `proxy` / `vpn` / `tor`；Scamalytics「Anonymizing VPN」「Tor Exit Node」「Public Proxy」 |
| 黑名单 | 是否出现在公开的滥用黑名单里 | Scamalytics 列出 Firehol、IPsum、Spamhaus 等名单的命中情况 |
| 共享程度 | 一段时间内有多少人用过这个 IP | ping0「共享人数」 |
| 地理与“原生” | IP 实际在哪里使用，地址段是不是本地运营商的 | ping0「原生 IP」与「广播 IP」 |

另有一类检测不看 IP 本身，而是看你的浏览器和网络连接是否前后一致，比如 DNS 请求从哪里发出、WebRTC 会不会暴露另一个地址、浏览器时区和语言。ippure 等网站会把这些和 IP 检测放在一起显示。

## 各家检测站官方怎么解释自己的分数

### ping0（风控值、原生 IP、共享人数）

ping0 在每个指标旁的“说明”里写得很具体：

- <strong>风控值</strong>：“值越大，风险越高，50 以内安全，70 以上可直接拉黑”。依据是“通过大数据监测 IP 是否有扫描、爆破、爬虫、对外攻击、发送垃圾邮件、作为木马病毒的 C&C 服务等行为”。分档为 0–15 极度纯净、40–50 轻微风险、50–70 稍高风险、70–100 极度风险。
- <strong>IP 类型</strong>：只分“IDC 机房 IP”和“家庭宽带 IP”。ping0 自己指出，只看 IP 的所有者是不是运营商并不准确，因为“国内的 IDC 服务器 IP，大部分都是归属于电信联通移动三大运营商”，所以它对每个 IP 段单独做了标注。
- <strong>原生 IP</strong>：大型云厂商（AWS、GCP、Azure 等）会把同一个大地址段广播到不同国家，这叫“广播 IP”；显示为原生 IP，“通常代表这是一个家庭宽带 IP 或者本地 IDC 提供商 IP”。
- <strong>共享人数</strong>：“该 IP 过去一年的使用人数，基于全网大数据监控的估算值”，分档 1–10 极好，到 10000 以上极度风险。
- <strong>适用场景</strong>：根据位置、类型、风控值给出的打分，ping0 自己注明“仅为经验总结，非 100% 准确，仅供参考”。

### IPQS（Fraud Score）

IPQS 的接口文档给出了明确的阈值：<strong>“Fraud Scores >= 75 are suspicious, but not necessarily fraudulent”</strong>（75 分以上可疑，但不一定是欺诈），<strong>“Fraud Scores >= 90 are high-risk users likely to engage in malicious behavior”</strong>（90 分以上是高风险）。它还单独给出 `recent_abuse`（近期是否有经过核实的滥用）、`bot_status`（近期是否有机器人流量用这个 IP 做自动化欺诈）、`abuse_velocity`（滥用频率）等字段（[文档](https://www.ipqualityscore.com/documentation/proxy-detection-api/response-parameters)）。

### Scamalytics（Fraud Score）

Scamalytics 的分数是 0–100，同时列出机房、代理、Tor、外部黑名单等判断。它的页面写明：<strong>“The statements on this page represent our opinion based on the limited information we have available to us”</strong>（页面上的结论是根据有限信息作出的判断），而且只覆盖“web connections made by internet users to websites and applications”（[页面](https://scamalytics.com/ip)）。

### IPinfo（ASN 类型）

IPinfo 的 ASN 数据有一个 `type` 字段，说明拥有这个网络的组织是什么类型，取值为 <strong>isp、hosting、business、education、government</strong> 之一（[文档](https://ipinfo.io/developers/asn)）。它说的是“谁拥有这段地址”，不是“这个 IP 有没有干过坏事”。

## 实测：同一个 IP，各家结论对不上

为了不涉及任何个人，我们用三个公开、身份明确的 IP 做对照：谷歌的公共 DNS 服务器、一台亚马逊云服务器、一个 Tor 出口节点（来自 Tor Project 公布的出口列表）。2026 年 11 月 10 日查询结果：

| IP 和它实际是什么 | ping0 | Scamalytics |
|---|---|---|
| 8.8.8.8，谷歌公共 DNS（`dns.google`） | IDC 机房 IP；风控值 <strong>22%（纯净）</strong>；原生 IP；共享人数 1–10（极好）；AI 应用“适合” | Fraud Score <strong>0（Low）</strong>；Datacenter：Yes |
| 3.221.139.111，亚马逊 AWS 云服务器（`ec2-…amazonaws.com`） | IDC 机房 IP；风控值 <strong>21%（纯净）</strong>；原生 IP；共享人数 1–10（极好）；AI 应用“适合” | Fraud Score <strong>12（Low）</strong>；Datacenter：Yes |
| 109.70.100.4，Tor 出口节点 | IDC 机房 IP；风控值 <strong>91%（极度风险）</strong>；共享人数 <strong>1–10（极好）</strong>；AI 应用“不推荐” | Fraud Score <strong>0（Low）</strong>；Tor Exit Node：Yes；同时命中 Firehol、IPsum 两个黑名单 |

从这张表能看出三点：

1. <strong>同一个 Tor 出口，一家给 91% 极度风险，另一家给 0 分低风险。</strong>Scamalytics 明明识别出了它是 Tor 出口、也在黑名单里，总分却是 0。各家“总分”的算法差别很大，不能互相换算。
2. <strong>“纯净”不等于“像普通用户”。</strong>谷歌 DNS 服务器和亚马逊云服务器都是典型的机房 IP，正常情况下不会有人用它们刷网页，两家却都给了很低的风险分，ping0 还标注“AI 应用适合”。风险分低，往往只说明“数据库里没有它的不良记录”。
3. <strong>细项之间也会矛盾。</strong>Tor 出口是许多人共用的匿名网络，ping0 却显示共享人数 1–10“极好”。说明“共享人数”这种估算值，对某些类型的 IP 并不可靠。

## 常见误解

### “纯净度 100 分就不会被封号”

没有依据。Anthropic 官方列出的封号原因是反复违反使用政策、从不支持的地区创建账号、违反服务条款（[原文](https://support.claude.com/en/articles/8241253)）；OpenAI 写明，从不支持的国家访问可能导致账号被封禁或暂停，使用不在支持名单内的付款方式会被阻止使用服务（[原文](https://help.openai.com/en/articles/9131992)）。两家都没有公布风控系统用了哪些数据，也<strong>没有任何公开资料表明它们调用了 ping0、IPQS、Scamalytics 这类网站的评分</strong>。账号是否正常，是平台把地区、手机号、付款信息、设备、使用行为等放在一起判断的，IP 只是其中一项。详见[Claude 账号为什么会被封](/guides/claude-account-suspended-reasons)。

### “住宅 IP 一定比机房 IP 安全”

“住宅”只说明地址属于家庭宽带运营商，不说明有多少人在用、用来干什么。ping0 自己也提醒，按所有者判断类型“只能说明这个 IP 的拥有者是 ISP，并不代表此 IP 实际使用在家庭宽带里”。

### “换一个检测网站分数更高，就说明 IP 更好”

不说明。上面的实测里，同一个 IP 在两家的结论可以正好相反。只看对自己有利的那一家，等于没有检测。

### “检测网站是中立的第三方”

检测网站各有自己的商业模式。比如 ping0 的查询结果页上，就有多家代理 IP 服务商的广告，“适用场景”一栏按 TikTok、跨境电商、社媒运营、AI 应用打星。看分数时，记住它们是“参考意见”，不是平台的判决。

## 这些分数还有什么用

不是完全没用。它们适合用来<strong>发现明显的问题</strong>，而不是用来证明“没问题”：

- 某个 IP 在多家网站上同时被标为代理、Tor，或者命中公开黑名单，说明它确实被很多人用过、有过滥用记录，访问网站时更容易遇到验证码或被拒绝。
- 分数正常，只能说明“已知数据库里没有它的不良记录”，不代表任何平台会放行。

## 开途在其中的位置

开途是网络工具，负责的是“连得上、连得稳”这一项。电脑版默认智能分流，国内网站直连，境外网站走开途的线路；拥塞控制 [k2cc](/k2/k2cc) 针对跨境线路的高丢包设计。账号、手机号、付款和使用行为由各平台自己判断，不在网络工具的范围内。开途的下载入口在[下载页](/install?utm_source=guide&utm_medium=content&utm_campaign=ip-purity-check-explained)，价格和退款规则见[定价页](/pricing?utm_source=guide&utm_medium=content&utm_campaign=ip-purity-check-explained)。

## 常见问题

<strong>IP 纯净度是什么意思？</strong>

是中文网络上的说法，指一个 IP 在检测网站数据库里的风险情况：属于家庭宽带还是机房、有没有滥用记录、是不是代理或 Tor、有多少人共用。业内一般叫 IP 信誉或欺诈分（Fraud Score），没有统一标准，每家算法不同。

<strong>ping0 的风控值多少算正常？</strong>

按 ping0 自己的说明，风控值 50 以内算安全，70 以上属于极度风险；0–15 为极度纯净。这是 ping0 一家的标准，换到其他网站不通用。

<strong>IPQS 欺诈分多少算高？</strong>

IPQS 文档写明，75 分以上可疑但不一定是欺诈，90 分以上是高风险。

<strong>为什么同一个 IP 在不同网站上分数差很多？</strong>

各家的数据来源、更新频率和总分算法都不同。我们的实测里，同一个 Tor 出口节点在 ping0 上是 91%（极度风险），在 Scamalytics 上是 0 分（低风险）。

<strong>Claude 会用 IP 纯净度来封号吗？</strong>

Anthropic 没有公布风控系统使用哪些数据，也没有公开证据表明它使用 ping0、IPQS 等第三方网站的评分。官方列出的封号原因是违反使用政策、从不支持的地区创建账号和违反服务条款。

<strong>节点纯净度测试分数高，就可以放心用吗？</strong>

不能这样理解。分数高只说明已知数据库里没有不良记录。账号能否正常使用，是平台结合地区、手机号、付款信息、设备和使用行为等综合判断的。

延伸阅读：[Claude 账号为什么会被封](/guides/claude-account-suspended-reasons) · [国内能用 ChatGPT 和 Claude 吗？报错对照](/guides/chatgpt-claude-china) · [k2cc 拥塞控制原理](/k2/k2cc)
