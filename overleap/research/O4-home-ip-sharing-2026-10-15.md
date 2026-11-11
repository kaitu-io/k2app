# O4 家宽 IP 共享出口网络：先例、法律、经济与可行性（2026-10-15/16）

> 只读调研：没有注册、联系、下单。访问日期为 2026-10-15 或 2026-10-16，另有日期的单独注明。
> 图例：✅ 打开过原文或一手页面 ｜ ⚠️ 只见到搜索摘要、二手转述或旧版本 ｜ ❓ 无法核实或没找到。标“计算”的是本文自己的算术，标“推论”的是判断，不是事实。
> 方案（创始人提出）：Wordgate LLC 给有家宽的人寄一台预装 Kaitu 出口软件的路由器（先美国，后欧洲大陆；不做英国和中国大陆），只供自家付费订户出口，不当代理卖，主机拿分成。
> **补充方案**：①小圈分配，每个用户软分配约 10 个家宽节点，每个节点最多约 20 个用户；②面向全球，让中国、伊朗、俄罗斯、缅甸等受审查网络里的用户经开放国家的家宽出去；③分配表和“不留日志”之间存在矛盾；④测算这个比例下的经济账。
> 相关前文：`O3-isp-ip-suppliers-2026-10-15.md`（SolaDrive 每个 IP $19.5–25/月，以及“ISP 段做 VPN 出口迟早会被重新分类”）。

---

## 0. 一页结论

1. **主机一侧的合同违约在美国几乎是确定的。** Xfinity 的 AUP 点名禁止 “proxy services and servers” 和 “make available to anyone outside the Premises the ability to use the Service”。Verizon 禁止 “any type of server”，也禁止 “allow third parties to use the Service”。AT&T 把 “selling of use of or access to AT&T Services” 列为可以终止服务的行为。后果是限速、停机、销户（§2.1）。德国 Telekom 只禁止交给第三方“zum alleinigen Gebrauch”（独占使用），相对宽松。
2. **最大的现实风险是刑事侦查落到主机家里，不是民事版权。** 美国有 2011 年 Buffalo 破门搜查（蹭网邻居下载儿童色情）、2016 年西雅图 Tor 出口家庭被搜；德国有 2018、2024 年 Tor 组织被搜。民事方面在变好：Cobbler v. Gonzales（2018），Cox v. Sony（美国最高法院 2026-03），德国 DDG §7(3)。但**法律上的免责挡不住搜查令**，而搜查令找的是 IP 的持有人，也就是主机（§1.10、§2.3、§2.4）。
3. **所有先例都没有给主机提供过法律援助或保险。** 真正起作用的风控都在“谁能用”这一侧：Bright Data 强制 KYC、Mysterium 的 B2B 白名单。Psiphon Conduit、Tor Snowflake、Lantern Unbounded 这三个反审查先例**都刻意让家宽只做入口（桥），出口留在自己的机房**。我们的方案恰好承担了它们回避掉的那一半风险（§1、§7b）。
4. **IP 情报厂商不靠“人数”打标，靠枚举和行为。** Spur 已经有 `MYSTERIUM_VPN` 标签⚠️；只有 4 个客户端的 IP 也能被打上代理标签✅。20 人的小圈能降低统计异常，但挡不住“注册账号，看自己被分到哪些出口”这种枚举方式。长期固定的出口会被**持续观测到**，标签会一直挂着（§6、§7a）。
5. **在 10:20 的分配比例下经济账算不过来。** 节点数 = 用户数 ÷ 2，每个节点平均只对应 **2 个付费用户的收入，约 $13.2/月**（计算）。扣掉路由器摊销（约 $4/月）后，能分给主机的只有几美元。按每个用户算，出口成本大约是 SolaDrive 的 2–4 倍（§5、§7d）。
6. **结论：按原方案（寄硬件、付现金、家宽当出口、面向审查国用户）不建议推进（No-go）。** 有条件推进的版本是：**会员互助制**（主机本身是会员，收益是免会员费；不寄硬件，用户自己的设备或已有的路由器装软件）+ **只给开放国家的已核验付费用户用作“住宅出口”** + **审查国用户只走“家宽入口桥 + 机房出口”** + 分配比例从 10:20 改成约 3:20。先在德国或美国招 10–20 个知情的内部主机做 90 天试点（§9–§11）。

---

## 1. 先例和它们的结局

### 1.1 Mysterium Network（家用 dVPN 出口，付 MYST 代币）
- **模式**：任何人都可以跑节点（软件、Docker、预装好的树莓派套件），按流量拿 MYST。流量分 B2B 数据采集、B2B VPN、数据传输、Public 四类。官方推荐 B2B 为 “the most "clean and safe" type of traffic”。这是 Kryptex 转述的，https://www.kryptex.com/es/articles/category/mysterium ⚠️。
- **规模**：“over 22,000 active nodes across 135+ countries”，“1 petabyte of monthly traffic”。来源是 QuickNode 名录，页面没有日期，https://www.quicknode.com/builders-guide/tools/mysterium-network-by-mysterium-foundation ⚠️。
- **主机收入**：**没有找到可靠的实际收入数字**。有一个第三方 ROI 模型算出净收益为负（-$0.0068/天），页面自己标注数据已过时，https://gamefi-roi-web.onrender.com/opportunities/mysterium-network-node ⚠️；常见的“$10–200/月”说法没有出处 ❓。
- **官方的风险提示**：“your IP address becomes theirs for the session”。见 https://help.mystnodes.com/en/articles/8004257-how-to-stay-safe-operating-a-node ✅（访问日 2026-10-15）。dVPN Alliance 指南建议优先用机房、向 ISP 申请单独 IP、把 IP 登记到 RIR，并附了一封写给 ISP 的模板信。见 Storj 论坛转引，https://forum.storj.io/t/mysterium-and-storj-collaborate-to-free-the-cloud-through-decentralized-protocols/12937 ⚠️。
- **主机出事**：只有论坛二手传闻。2021-04-23 有用户写道 “node operators have received letters from lawyers regarding illegal activities”，Discord 上有人说 “had to pay a fine of around € 936”（德国）。来源同上 ❓。**没有找到警察搜查 Mysterium 节点的报道。**
- **被标记**：proxycheck.io 给 Mysterium 地址的风险分从 73% 起，理由是 “most of the addresses being hosted from residential address ranges it has become a magnet for criminals”，https://proxycheck.io/blog/post/improving-support-for-decentralized-vpn-networks ⚠️。Spur 有专门的 `MYSTERIUM_VPN` 标签，“activity… in 91 distinct countries and 1219 distinct ISPs”，https://spur.us/tags/MYSTERIUM_VPN ⚠️（原页返回 429，信息来自摘要）。
- **路由器版本**：没有找到 GL.iNet 或华硕的官方 Mysterium 固件 ❓，只有树莓派套件 https://raspberrypi.dk/en/?p=85311 ⚠️。

### 1.2 Sentinel dVPN
- 官网称 “1500+ P2P Bandwidth Providers”，页面没有日期，https://sentinel.co/ ⚠️。社区遥测显示 24 小时内活跃节点 1,574 个，分布在 88 国，没有日期，https://suchnode.net/ ⚠️。2024-05 的数据是注册节点 4,005 个，https://www.coindesk.com/es/tech/2024/05/23/protocol-village ⚠️。
- 主机收入、滥用事件、法律行动都**没找到** ❓。

### 1.3 Orchid
- 独立提供者质押 OXT 出售带宽，“Orchid Global doesn't run servers…”，https://orchid.com/vpn ✅。Dealroom 2025-11 称累计融资超过 4,800 万美元 ⚠️。
- 节点数、主机收入、事故都**没找到** ❓。

### 1.4 Honeygain（Oxylabs 的主要货源）
- 2021-03 与 Oxylabs 签了独家批发协议，Oxylabs 称 Honeygain 是它的 “main source”，https://oxylabs.io/blog/oxylabs-signs-exclusive-contract-with-honeygain ⚠️。
- **主机收入**：没有官方固定费率。有个人 30 天实测得到 “Effective Rate ~$0.11 per GB”，https://www.blog.brightcoding.dev/2026/07/10/i-let-my-internet-idle-for-30-days-heres-how-much-honeygain-actually-paid-me （2026-07-10）⚠️。
- **滥用**：Trend Micro（2023-02-07）在 Honeygain 和 IPRoyal Pawns 的出口上观察到点击欺诈、SQL 注入探测、读取 /etc/passwd、批量注册账号等行为，并写道 “users who share their connections could potentially be involved in legal investigations.”，https://www.trendmicro.com/en_us/research/23/b/hijacking-your-bandwidth-how-proxyware-apps-open-you-up-to-risk.html ✅。Digital Citizens Alliance 的测试发现有流量来自受美国制裁的 T-Bank，并警告 “if the FBI investigates, that could lead agents to your front door.”，见 Techlicious 2026-07-06，https://www.techlicious.com/blog/honeygain-passive-income-apps-criminal-traffic/ ⚠️。
- **主机受影响**：有用户自述因此被 ISP 封号，https://www.publish0x.com/pennypay/the-dangers-of-bandwidth-sharing-my-experience-with-honeygai-xywywpg ❓（孤例）。也有人说家宽 IP 被标成 VPN 后，问卷站把他封了 ❓。Cisco Talos 2021-09 发现恶意软件静默安装 Honeygain，建议把它当作 PUA 处理，https://thehackernews.com/2021/09/cybercriminals-abusing-internet-sharing.html ⚠️。

### 1.5 Pawns.app / IPRoyal
- 第三方普遍写 “$0.20 per GB”，https://hardbacon.ca/en/financial-independence/pawns-app-review ⚠️。官网现在主推问卷、游戏、任务，自称 “$4+ Million Paid out to users in 2026”，最低提现 $5，https://pawns.app/ ⚠️。
- 主机被罚或被警察找上门：**没找到** ❓。

### 1.6 PacketStream
- 买方价格是 “$1 per GB”（最少买 50GB），卖方收入 “starting at $0.10 per GB”，https://www.techradar.com/reviews/packetstream ⚠️。Proxyway 评它 “on life support”，https://proxyway.com/reviews/packetstream-proxies ⚠️。
- 2019 年有人在 NANOG 上问：“how does this not violate just about every provider's ToS?”，https://mailman.nanog.org/pipermail/nanog/2019-April/100626.html ⚠️。

### 1.7 Grass（Wynd Labs）
- 用主机闲置带宽抓公开网页，把数据卖给 AI 公司；主机拿积分，再换代币空投。Nansen 称 “over 3m active nodes across 190 countries”，2025-03 每天抓取约 110 万 GB，https://research.nansen.ai/articles/1871 ⚠️。2023-12 获得种子轮 $350 万，https://www.coingecko.com/learn/what-is-grass-io-potential-airdrop ⚠️。
- 按美元算的主机收入、诉讼：**没找到** ❓。

### 1.8 Bright Data / Luminati / Hola（2015）及 EarnApp / Bright SDK
- **2015 年 8chan 事件**：Hola 把免费用户的带宽经 Luminati 出售。8chan 的 Brennan 称 “An attacker used the Luminati network to send thousands of legitimate-looking requests to 8chan in 30 seconds, representing a 100x spike over peak traffic”。来源 https://en.wikipedia.org/wiki/Hola_(VPN) ⚠️、PCWorld 2015-05-29 https://www.pcworld.com/article/427726/ultra-popular-hola-vpn-extension-sold-your-bandwidth-for-use-in-a-botnet-attack.html ⚠️。创始人承认有这回事，说公司会筛查买家，但攻击者还是混了进来。
- **EarnApp**：2025-08 从按 GB 计费改成按在线时长计费，“up to $10/month per device”，https://raspberrytips.com/raspberry-pi-earnapp-guide/ ⚠️。
- **Bright SDK**：Spur 查了 LG 和 Samsung 电视上的 6,038 个应用，其中 2,058 个内嵌代理 SDK、367 个与 Bright Data 有关，https://www.helpnetsecurity.com/2026/06/23/tv-residential-proxy-sdk/ （2026-06-23）⚠️。
- **现行风控**：住宅代理只开放给 “verified companies only”，适用于 2026-07-07 以后新建的 zone；“government websites are blocked across all networks”。见 https://docs.brightdata.com/proxy-networks/residential/network-access ✅。

### 1.9 Helium（硬件换收益）
- 投资人 Liron Shapira 说热点售价 $400–800，用户本来预期每月约 $100，实际只有约 $20，https://cryptoslate.com/angel-investor-says-helium-has-no-demand-returns-are-poor/ ⚠️（个人说法）。Fortune（2022-09-27）报道 HNT 跌了 90% 以上，当时 IoT 客户每月只花约 $6,500 数据费，https://www.fortune.com/2022/09/27/embattled-helium-attempting-crypto-powered-mobile-network ⚠️。
- **更正 brief 里的说法**：SEC 起诉 Nova Labs 是在 **2025-01**，不是 2023；2025-04 以 “with prejudice” 撤诉，罚款 $20 万，指控内容包括 “sale of hotspot devices”，https://decrypt.co/314437/sec-dismisses-helium-case-ending-gary-genslers-final-act ⚠️。
- **教训（推论）**：硬件补贴模式下，收益预期一旦落空，信誉风险和监管风险会一起出现。

### 1.10 Tor 出口运营者（警察上门的主要证据都来自这里）
- EFF 的 Tor 法律 FAQ 写道：“we are not aware of an individual being sued, prosecuted, or convicted for running a Tor relay”。对“能不能在家跑出口”的回答是 “No, this is risky and not recommended… it's best not to run your exit relay in your home”，https://community.torproject.org/relay/community-resources/eff-tor-legal-faq/ ✅。
- Tor 博客说 “there have been no equipment seizures due to Tor exits, but there have been phone calls and visits”（美国），https://blog.torproject.org/tips-running-exit-node/ ✅。
- **西雅图 2016-03-30**：起因是 NCMEC 线报，6 名警察清晨持搜查令进门，结果 “no child porn was found, no assets were seized, and no arrests were made”，https://www.thestranger.com/tech/2016/03/30/23885710/police-go-on-fishing-expedition-search-the-home-of-seattle-privacy-activists-who-maintain-tor-network ⚠️。
- **奥地利 Weber**：2012-11 被搜查，2014-06 被判缓刑。定罪依据是他在聊天中教唆，不是跑出口本身，https://www.pcworld.com/article/440223/tor-exit-node-operator-convicted-of-abetting-spread-of-child-porn.html ⚠️。
- **俄罗斯 Bogatov 2017-04**：因为有人经他家的出口 IP 发帖，他被捕，https://www.eff.org/deeplinks/2017/04/access-now-and-eff-condemn-arrest-tor-node-operator-dmitry-bogatov-russia ⚠️。
- **德国 Zwiebelfreunde 2018-06-20**：董事住宅被搜查，法院后来判定搜查违法，https://en.wikipedia.org/wiki/Torservers.net ⚠️。**Artikel 5 e.V. 2024-08-16** 在埃森被武装警察搜查，理事随后表示：“I am personally no longer willing to provide my personal address&office-space as registered address… as long as we risk more raids by running exit nodes”，https://lists.torproject.org/mailman3/hyperkitty/list/tor-relays@lists.torproject.org/thread/RW64BFD3ENP4NXA4RYZKARIRR5HRAXME/ ✅。
- 2013 年有 ISP 以 “disproportional amount of abuse and legal complaints” 为由要求撤掉 Tor 节点，https://archive.torproject.org/websites/lists.torproject.org/pipermail/tor-relays/2013-July/002224.html ⚠️。

### 1.11 Anyone Protocol（原 ATOR，硬件中继）
- 2024-06 改名，当时有 1,800 多个中继，首批 1,000 台硬件要求持有 NFT，https://theblock.co/post/302077/ator-rebrands-to-anyone-hits-1800-relays-to-bring-privacy-mainstream ⚠️。
- FAQ 没有说明硬件路由器是否充当出口，https://docs.anyone.io/welcome/faq ❓。

### 1.12 其他：Peer2Profit、Proxyrack、Repocket 和盒子类硬件
- **Peer2Profit**：主机拿住宅流量 $0.28/GB；关联的 Astroproxy 卖 $7.60/GB，“up to 27 times what the user was paid”。它的 SDK 曾被按安装付费的僵尸网络大量装机，Silent Push 2026-08-20，https://silentpush.com/blog/peer2profit-astroproxy ⚠️。
- Proxyrack 付 $0.50/GB，Repocket 付 $0.20/GB，来自博客 https://techien.substack.com/p/apps-that-earn-passive-income-in ⚠️。
- **“插上就能赚钱”的合规路由器或盒子：没有找到成规模的先例** ❓。

### 1.13 2026 年的执法和平台背景
- **911 S5**（2024-05）：DOJ 捣毁，涉及 1,900 万个 IP，靠 MaskVPN、DewVPN 等免费 VPN 传播，https://www.jurist.org/news/2024/05/us-department-of-justice-dismantles-massive-botnet-in-major-cybercrime-crackdown/ ⚠️。
- **IPIDEA**（2026-01-28）：Google 联合 Spur、Cloudflare、Lumen 打击，设备池减少 “by millions”，https://www.theregister.com/2026/01/29/google_ipidea_crime_network/ ⚠️。
- **NetNut**（2026-07-02）：FBI 查封了数百个域名，https://krebsonsecurity.com/2026/07/fbi-seizes-netnut-proxy-platform-popa-botnet/ ⚠️。
- **FBI PSA I-031226-PSA（2026-03-12）**：“People often do not realize that criminals use their internet connection to commit cyber attacks”，并建议企业 “Block IP addresses that are known to be associated with residential proxy networks”，https://www.ic3.gov/PSA/2026/PSA260312 ⚠️。
- **含义（推论）**：“付费分享家宽”这一整类已经被执法和平台当作风险对象。我们做的是知情同意、只给自家订户用，性质不同，但会被 IP 情报库和平台归进同一类。

---

## 2. 主机一侧的法律和合同风险

### 2.1 美国住宅宽带条款
| ISP | 原文 | 状态 |
|---|---|---|
| **Comcast Xfinity**（AUP，Last updated 2021-02-01） | “use or run dedicated, stand-alone equipment or servers from the Premises that provide network content or any other services to anyone outside of your Premises local area network… Examples… **proxy services and servers**”；“**resell the Service or otherwise make available to anyone outside the Premises the ability to use the Service**”；“personal and **non-commercial** residential use”；违约可以 “immediately to suspend” 或 “terminate” | ✅ https://www.xfinity.com/corporate/customers/policies/highspeedinternetaup |
| **Verizon Fios**（ToS v20-1，2020-01-30） | “not designed for or intended to be used for any commercial purpose… you may not resell, re-provision or rent the Service, (either for a fee or without charge) or allow third parties to use the Service… use the Service to host **any type of server**” | ✅ https://www.verizon.com/about/sites/default/files/Verizon-Online-Terms-of-Service-v20-1-013020.pdf （2026 年现行版本的页面 404 ⚠️） |
| **AT&T** | 可以终止服务的情形包括 “any reselling of AT&T Services (including **selling of use of or access to** AT&T Services)”；设备 “only for its intended residential use” | ✅ att.com/legal/terms.internetAttTermsOfService.html（摘取部分） |
| **Spectrum** | 摘要显示禁止 “Reselling or otherwise redistributing the Service”，并写有 “residential use only” | ⚠️ https://www.spectrum.com/policies/internet-use-policy （原页 Access Denied） |
| T-Mobile Home Internet | 没找到原文 | ❓ |

**判断（推论）**：在美国，主机拿钱让户外的人用自己的宽带，四大 ISP 的条款都违反，Xfinity 的条款还直接点名代理。后果是合同层面的（限速、停机、销户），不是刑事问题。**没有找到 ISP 实际销户的案例** ❓，执行力度不明。

### 2.2 欧盟（简要）
- **Deutsche Telekom**（AGB，Stand 11.08.2026）第 5.2 条：“nicht gestattet, a) die Leistungen ohne unsere Zustimmung Dritten **zum alleinigen Gebrauch** zu überlassen, b) selbst als **Anbieter von Telekommunikationsdiensten**… aufzutreten”，https://www.telekom.de/agb/downloads/56442.pdf ✅。共享使用没有被明文禁止；b) 项能不能套用到主机身上有争议 ⚠️（推论）。
- **Orange France**：“pour ses besoins propres et pour un usage strictement personnel et privé”，https://www.resilier.fr/images/cg-livebox-livebox-up.pdf ⚠️。
- **KPN**：原文没有取到 ❓。

### 2.3 美国：主机对第三方流量的责任
- **17 USC 512(a) mere conduit**：512(k)(1)(A) 的定义是 “an entity offering the transmission, routing, or providing of connections…”；512(i) 要求 “adopted and reasonably implemented… a policy that provides for the termination… of… repeat infringers”，https://www.law.cornell.edu/uscode/text/17/512 ✅。EFF 认为开放 Wi-Fi 提供者可以主张这一条，https://www.eff.org/deeplinks/2011/08/open-wifi-and-copyright-liability-setting-record ⚠️（属倡导观点）。**没有找到个人主机成功援引 512(a) 的判例** ❓。主机本身没有 “subscribers”，真正能执行重复侵权者政策的是我们，所以 512(i) 的合规应由 **Wordgate** 承担（推论）。
- **Cobbler Nevada v. Gonzales**，901 F.3d 1142（9th Cir. 2018-08-27）：单凭 IP 订户身份 “standing alone” 不足以认定侵权，“failure to take affirmative steps to police his internet connection is insufficient”，https://blog.ericgoldman.org/archives/2018/09/ip-address-subscriber-isnt-liable-for-copyright-infringement-by-users-sharing-that-ip-address-cobbler-v-gonzales.htm ⚠️。只在第九巡回区有约束力，版权方的和解信和传票照样会寄到主机手上。
- **Cox v. Sony**（美国最高法院，2026-03-25，No. 24-171）：“mere knowledge… is insufficient”，ISP 只有在诱导侵权或提供专为侵权定制的服务时才承担帮助侵权责任，https://www.faegredrinker.com/en/insights/publications/2026/3/supreme-court-decides-cox-communications-v-sony-music-entertainment ⚠️（律所解读，判决原文没读）。
- **刑事侦查**：2011 年 Buffalo，ICE 循着 P2P 下载儿童色情的记录破门进屋，扣走了电脑、iPad 和 iPhone，3 天后排除户主嫌疑，真正下载的是蹭网的邻居，https://www.nbcnews.com/id/wbna42740201 ⚠️。

### 2.4 德国、荷兰、欧盟
- **DSA 第 4 条（mere conduit）**：只要不发起传输、不选择接收方、不修改内容，就对所传信息免责，但 “does not affect the possibility… of requiring the service provider to terminate or prevent an infringement”，https://www.springlex.eu/en/packages/dsa/dsa-regulation/article-4/ ⚠️。
- **德国 DDG §7(3)**（2024-05 取代 TMG）：免责的提供者 “auch nicht… auf Schadensersatz oder Beseitigung oder **Unterlassung**… in Anspruch genommen werden; dasselbe gilt hinsichtlich aller Kosten”，https://www.gesetze-im-internet.de/ddg/__7.html ✅。2017 年废除的妨害人责任（Störerhaftung），在新法里延续下来。
- **DDG §8 封锁请求权（Sperranspruch）**：权利人在 “zumutbar und verhältnismäßig” 的前提下可以要求封锁，费用不由提供者承担，https://www.gesetze-im-internet.de/ddg/__8.html ✅。BGH I ZR 64/17（Dead Island，2018-07-26）认为封锁措施可以包括设置密码，极端情况下可以关闭接入 ⚠️。
- **荷兰**：DSA 第 4 条直接适用；Telecommunicatiewet 是否另有义务，没有查到 ❓。
- **结论**：德国民事责任对主机最友好，但德国也是 Tor 出口被搜查次数最多的国家（§1.10）。

### 2.5 税务（给主机的分成）
- **1099-NEC / 1099-MISC**：根据 OBBBA，2026-01-01 起支付的款项，申报门槛从 $600 提到 **$2,000**；备用预扣门槛也一样，https://cpapracticeadvisor.com/2025/07/30/one-big-beautiful-bill-act-changes-1099-thresholds/165863/ ⚠️（没读 IRS 原文）。主机每年拿几十到几百美元，**基本不需要开 1099**，但仍然要收 W-9；拿不到 TIN 时要做备用预扣。
- **1099-K**：恢复为超过 $20,000 **且** 超过 200 笔，https://www.avalara.com/us/en/blog/one-big-beautiful-bill-act-1099-reporting-threshold.html ⚠️。
- **欧盟主机**：收 W-8BEN。境外提供的劳务一般不算美国来源所得 ⚠️。“出租带宽”算劳务、租金还是特许权使用费，**没有定论** ❓，需要税务顾问确认。DAC7 只覆盖四类活动，被动出租带宽看起来不在范围内，https://belastingdienst.nl/wps/wcm/connect/en/business/content/information-for-platform-operators-dac7 ❓。
- **会员互助制**（主机拿的是“免会员费”）：这算实物报酬，按理也是收入；但金额远低于申报门槛（推论，需要 CPA 确认）。

---

## 3. 我们（Wordgate LLC）的责任

- **ECPA/SCA**：18 USC 2510(15) 把 ECS 定义为 “any service which provides to users thereof the ability to send or receive wire or electronic communications”，https://www.law.cornell.edu/uscode/text/18/2510 ✅。面向公众的 VPN **很可能**属于 ECS，但没有直接判例 ❓。按 2703(c)(2)，凭行政传票就必须披露姓名、地址、“records of session times and durations”、“any temporarily assigned network address”，https://www.law.cornell.edu/uscode/text/18/2703 ✅。**执法部门先找到主机，下一步就会来找我们。** 所以必须事先定好要保留哪些记录（见 §7c），并建立法律请求处理流程。18 USC 2258A 规定了发现 CSAM 后向 NCMEC 报告的义务，这里只做提示 ❓。
- **CALEA**：47 USC 1002(b)(2) 规定拦截能力要求 “do not apply to— (A) information services”，https://www.law.cornell.edu/uscode/text/47/1002 ✅。VPN 通常被视为 information service，但 FCC 没有专门认定过 ❓。
- **运营方因主机出事而担责的先例：没找到** ❓。911 S5、IPIDEA、NetNut 被打击，原因都是**未经同意装机**以及**卖给犯罪分子**。Hola 之后没有找到用户集体诉讼，只有专利和反垄断纠纷，https://oxylabs.io/blog/oxylabs-files-antitrust-claims-against-luminati-emk-capital-and-hola ⚠️。
- **Windscribe 的教训**：希腊检方顺着 IP 直接起诉了公司 CEO 本人，2025-04-11 撤诉 ⚠️（依据主要是公司自述）。在我们的模式里，**IP 的持有人是主机本人**。
- **出口管制和设备合规（只做提示）**：
  - EAR 740.17(b)(1)：大众市场加密物品自行分类后归入 5A992/5D992，年度自分类报告只适用于部件、可执行软件，以及仍归在 5A002 的物品，https://www.ecfr.gov/current/title-15/subtitle-B/chapter-VII/subchapter-C/part-740/section-740.17 ⚠️。整台消费级路由器大概率不需要报告（推论）。如果被认定为 (b)(2) 意义上的网络基础设施，则需要 30 天分类 ❓。
  - **FCC**：5GHz 设备要求厂商防止用户改射频参数（KDB 594280），在原厂固件上加装应用风险较低 ⚠️。
  - **欧盟 RED 网络安全授权法规**（2025-08-01 起适用 EN 18031）：我们以自己的名义向欧盟主机投放改装过的路由器，**可能被视为投放者**，要承担 CE 合规，https://www.bsigroup.com/siteassets/pdf/en/products-and-services/bsi_radio_equipment_directive-cybersecurity_faq_flyer_a4_241014.pdf ⚠️。这是“不寄硬件”的又一个理由。
  - 德国 TKG §5 规定了商业电信服务的报备义务，VPN 是否需要报备，没有找到 BNetzA 的定性 ❓。

---

## 4. 先例中有效的滥用控制

| 控制手段 | 先例与效果 | 来源 |
|---|---|---|
| 封 25 端口 / 精简出口策略 | Tor 默认就封 25 端口，但只封端口不够：网页邮箱的垃圾邮件、SSH 爆破照样能出去。Comcast 本来就不允许住宅用户走 25 端口，FTC 建议 ISP “Block port 25 except for the outbound SMTP requirements of authenticated users” | https://lists.torproject.org/pipermail/tor-relays/2022-May/020540.html ⚠️；https://www.xfinity.com/support/internet/email-port-25-no-longer-supported/ ✅ |
| 白名单流量 | Mysterium 的 B2B 白名单被称为 “clean and safe”，代价是流量和收入都少 | 见 §1.1 ⚠️ |
| 对买家做 KYC + 拦截域名 | Bright Data 只开放给已验证的企业，政府网站全部拦截；Hola 当年的“筛查”还是被攻击者绕过 | §1.8 ✅ |
| 法律援助 / 保险 | EFF 只承诺 “assist… and will try to locate qualified legal counsel”。**没有找到任何先例为住宅出口主机提供法律辩护基金或保险** ❓ | https://www.eff.org/pages/legal-faq-tor-relay-operators ✅ |
| 反向 DNS / 出口说明页 / WHOIS 登记 | Tor 建议这样做，以 “prevent knee-jerk reactions”。**家宽主机做不到**，因为反向 DNS 和 SWIP 都由 ISP 控制（推论） | https://blog.torproject.org/tips-running-exit-node/ ✅ |
| 主机一键关停、按主机设流量上限、滥用处理台 | 各产品都能暂停，但成文的上限和滥用处理台**没有找到公开做法** ❓ | — |

**对我们的含义（推论）**：“只给自家付费订户用”本质上是白名单加 KYC，控制的是**谁能用**，但管不了用户在出口上做什么。因此必须再加上：①付款实名（只保留支付方提供的信息）；②新用户观察期，先走机房出口，30 天无投诉后才开放家宽出口；③出口侧封 25/465/587 端口、拦截 BitTorrent 和扫描；④按用户、按节点限速；⑤公司设滥用处理台，主机收到的投诉一律转给公司处理。

---

## 5. 经济账

### 5.1 价格基准
- **住宅代理批发价（买方，每 GB）**：Proxyway 2025 年中位价是 5GB 档 $4、50GB 档 $3.47、500GB 档 $2.58，1TB 档平均 $2.04，https://proxyway.com/research/proxy-market-research-2025 ⚠️。Bright Data $3–4，https://brightdata.com/pricing/proxy-network/residential-proxies ⚠️；Decodo $2.75–4 ⚠️；Oxylabs $2.5–6 ⚠️；IPRoyal $1.75–7.35，https://iproyal.com/pricing/residential-proxies/ ⚠️。Spur 称过去 5 年价格 “dropping 60-75%” ⚠️。
- **付给主机的价格**：$0.10–0.50/GB（PacketStream $0.10、Honeygain 实测 $0.11–0.15、Pawns $0.20、Peer2Profit $0.28、Proxyrack $0.50，见 §1），或按设备每月 $2.5–10（EarnApp “up to $10/month”）。主机拿到的约占零售价的 5–10%（计算）。

### 5.2 家宽上行和能带多少人
- AT&T Fiber 上行中位数 2025 年下半年为 309 Mbps（Ookla），https://www.rcrwireless.com/?p=428028 ⚠️。Opensignal 2024 上行：AT&T 81.4、Verizon 68.1、**Xfinity 23.3 Mbps**，https://rcrwireless.com/20240523/network-infrastructure/in-broadband-analysis-cable-generally-outperforms-telcos-opensignal/amp ⚠️。FCC MBA 的数字没拿到 ❓。
- **计算**：用户下载靠主机的**上行**。有线宽带上行 20–45 Mbps，留一半给主人，能同时看 HD 视频（Netflix HD “3 Mbps or higher”，https://help.netflix.com/en/node/306 ✅）的大约 **2–6 人**。光纤可以到几十人，这时瓶颈变成 IP 信誉，不是带宽。
- **流量上限按 2 倍计算**（推论）：用户 1GB 流量在主机线路上会同时产生约 1GB 下行和 1GB 上行。Cox 上限 1.25TB，超出部分每 50GB 收 $10、封顶 $100，https://www.cox.com/residential/internet/learn/extras.html ✅。按这个算，Cox 主机大约只能承载 600GB 的用户流量。Xfinity 新套餐不限量、旧套餐 1.2TB，https://www.cabletv.com/xfinity/internet/data-cap ⚠️（文章自相矛盾）。

### 5.3 硬件
- GL.iNet 官网价：Flint 2 $169.99（https://www.gl-inet.com/products/gl-mt6000/ ✅），Beryl AX $98.99（https://www.gl-inet.com/products/gl-mt3000/ ✅），Brume 2 $77.99（https://www.gl-inet.com/products/gl-mt2500/ ✅）。欧盟店 Opal €41 ⚠️。官方在美国和欧盟都有本地仓，标准运费免费，但 “Free shipping does not apply to… bulk orders”，https://www.gl-inet.com/policies/shipping-policy ✅。我们自己转寄给主机的单件运费**没查到** ❓。
- 摊销（计算）：Beryl AX 按 24 个月摊约 $4.1/月；Brume 2 约 $3.2/月。还没算丢失、回收、客服的费用。

### 5.4 原方案（不分圈）下的分成上限（计算）
- 每个订户每月 $6.6，按 30–50GB 算，**每 GB 收入只有 $0.13–0.22**，和市场付给主机的 $0.10–0.20/GB 持平。**所以按 GB 付费不可行，只能付固定月费。**
- 对照：SolaDrive SD-2 每 IP $25/月（含 2TB），10 IP 套餐每 IP $19.5（见 O3）。按每个 IP 承载 10–20 人算，每个用户的出口成本是 **$1–2.5/月**。
- 要让家宽节点不比 SolaDrive 贵：主机月费 + 硬件摊销 ≤ $20–25/节点，而这个节点必须真的承载 10–20 个用户。

---

## 6. 会不会被识别，多快

- **Spur**：住宅 IP 会因为参与 “proxy-sharing networks” 被归为代理，分类反映的是 “what was observed happening on an IP during a given window”，https://docs.spur.us/knowledgebase/faqs/why-might-a-residential-ip-be-classified-as-a-proxy ✅。方法是 “Multiple signals are evaluated together”，https://docs.spur.us/knowledgebase/faqs/how-does-spur-detect-vpns-residential-proxies-and-isp-proxies ✅。新的 VPN 或代理 “typically identified within 24-48 hours” 这句来自搜索摘要，原页 404 ⚠️。
- **ipinfo**：2025-07-08 推出住宅代理检测，“107 million observed residential proxy IPs”，https://secure.businesswire.com/news/home/20250708302825/en/ ⚠️。流转分析显示，住宅代理 IP 平均只在 4.56 天内被看到，“78% of IPs do not persist beyond 30 days”，https://ipinfo.io/blog/residential-proxy-shared-infrastructure-churn ✅。
- **MaxMind**：住宅代理库提供 1–99 置信分、最后观测日期和供应商名，https://dev.maxmind.com/geoip/docs/databases/residential-proxy/ ✅。
- **IPQS**：“reported by 5 clients in a 24 hour period will be blacklisted across our entire network”，https://ipqs.com/templates/infographic/Intelligent-IP-Reputation-and-Proxy-Detection.pdf ⚠️（宣传材料）。
- **Google**：“Some VPNs and tunnel services cause all traffic to be blocked”，https://support.google.com/websearch/answer/86640 ✅。没有公开人数阈值。
- **判断（推论）**：代理池里的 IP 是靠快速轮换躲过标签的。**我们的家宽出口是长期固定的，会被持续观测到，标签也就会一直挂着。** 被标记以后，受影响的不只是我们的用户，**主机全家自己上网**也会遇到验证码、问卷站封禁、银行风控（Honeygain 用户就有这类自述，§1.4）。这是对主机的直接伤害，必须写进招募协议。具体多快被标，只能在试点里实测。

---

## 7. 补充方案的评估

### 7a. 小圈分配（每个用户约 10 个节点，每个节点 ≤20 人）能不能躲过检测
- **没有任何厂商公开“多少人共用一个 IP 就打标”的阈值** ❓。Spur 的 Context API 有 `client.count`、`countries`、`concentration` 等字段；官方样例里一个只有 `count: 4`、`countries: 2` 的 IP 也被打上了 `NETNUT_PROXY` 标签，https://docs.spur.us/context-api ✅。说明打标不靠人数门槛。
- IPQS 看的是滥用行为：`abuse_velocity`、`recent_abuse`，后者指 “within the past few days”，https://www.ipqualityscore.com/documentation/proxy-detection-api/response-parameters ✅。**小圈里只要有一个人滥用，整个 IP 都会被连累。**
- ipinfo 的数据来源是“观察到这个 IP 在替某个服务转发流量”（`last_seen`、`percent_days_seen`、`service`），https://ipinfo.io/products/residential-proxy ✅，同样是**枚举**。
- **Cloudflare（2025-10-29）**：CGNAT 一个 IP 可能对应 “hundreds or even thousands of users”；判断依据是 “CGNAT IPs are typically allocated large blocks of IPs, whereas VPNs IPs are more scattered”，https://blog.cloudflare.com/detecting-cgn-to-reduce-collateral-damage/ ✅。我们的节点零散分布在各家 ISP，**这个特征更像 VPN**（推论）。
- 学术研究：151 份黑名单里有 53–60% 包含共享或动态地址，一个被列入的地址 “can potentially affect as many as 78 legitimate users for as many as 44 days”（IMC 2020），https://steel.isi.edu/members/sivaram/blocklisting_impact/ ⚠️。
- **判断（推论）**：20 人接近一个大家庭或小办公室的规模，只看人数的话，确实比开放池更难被发现。但有两个更强的信号：
  - **①枚举**：任何人买 N 个账号，就能看到大约 10N 个节点。分配比例 10:20 恰恰让枚举更便宜。
  - **②客户端来自多个国家**：一个美国家宽 IP 后面的客户端分布在中国、伊朗等地，是很强的 VPN 信号。
  - 应对：按来源地区分圈、新用户设观察期、每个账号能看到的节点数从 10 个降到 2–3 个、节点慢速轮换。

### 7b. 面向审查国家（全球化）
- **Psiphon Conduit**：“A Conduit Station is not an exit node. It only forwards encrypted traffic”；“A Conduit Station can see the IP addresses of clients that connect through it”；“some governments restrict or prohibit proxies”，https://s3.amazonaws.com/psiphon-conduit-website/en/faq.html ✅。伊朗的规模：“more than 40,000 Iranian users were connected simultaneously”，“Each external user can enable access for roughly 25 people”，Iran International 2026-01-27，https://www.iranintl.com/en/202601240957 ⚠️。**每个志愿者约 25 人，和我们设想的 20 人同一量级，但它只做入口，不做出口。**
- **Tor Snowflake**：志愿者只做桥，“Your IP appears in the central broker logs”，https://orbot.app/fr/kindness ⚠️。社区认为风险低于非出口中继 ⚠️。
- **Lantern Unbounded**：浏览器志愿者通过 WebRTC 中继，出口是 Lantern 自己的服务器，“only reaches a whitelist”，https://unbounded.lantern.io/ ⚠️。
- **Mysterium 在审查国的使用量**：没找到 ❓。
- **审查方怎么封**：
  - GFW 从 2021-11 起被动识别“全加密流量”，再配合主动探测，见 USENIX Security 2023，https://www.usenix.org/conference/usenixsecurity23/presentation/wu-mingshi ✅。
  - 俄罗斯 TSPU 从 2023 年起封 OpenVPN 和 WireGuard，Mediazona 2023-08-08，https://en.zona.media/article/2023/08/08/vpn ⚠️。
  - 伊朗：没有找到针对住宅出口的研究 ❓。
  - 推论：中国用户直连的海外家宽 IP 一旦被识别，**只是被 GFW 封掉**。主机在美国不会受影响，但节点池会被逐个消耗。
- **OFAC 制裁**：
  - **伊朗 GL D-2**（2022-09-23，已逐字核对）：(a)(1) 允许 “**Fee-based or no-cost services** … incident to the exchange of communications over the Internet, such as … web browsing”。附件第 10 项列有 “Virtual Private Network (VPN) client software, proxy tools, and fee-based client personal communications tools”。排除项有：明知服务对象是伊朗政府；SDN 名单上的人；“commercial-grade Internet connectivity services… with quality of service guarantees”。原文 https://ofac.treasury.gov/media/928096/download ✅。**结论：向伊朗普通用户提供收费的反审查 VPN 在授权范围内**，但不要承诺 SLA。实际卡住的是收款：Stripe 禁止与 “Cuba, Iran, North Korea, Syria, Crimea…” 的交易，https://stripe.com/legal/restricted-businesses ⚠️。合法不等于收得到钱。客户端需要先完成 5D992.c 自行分类。
  - **古巴** 31 CFR 515.578（2024-05-29 修订）覆盖 “certain fee-based internet communications services”；FAQ 1177 对受限官员只允许免费服务，https://ofac.treasury.gov/faqs/1177 ⚠️。
  - **叙利亚**：EO 14312 自 2025-07-01 起终止全面制裁，https://www.hklaw.com/en/insights/publications/2025/07/executive-order-terminates-syria-sanctions ⚠️。
  - **俄罗斯**：GL 25G（2024-10-30）覆盖电信，https://ofac.treasury.gov/media/922006/download ⚠️。俄罗斯不是全面禁运，普通用户只需做 SDN 筛查。2025–2026 年是否有新版本没查到 ❓。
  - **朝鲜**：建议直接不服务 ❓。
  - **缅甸**：属于定向制裁（EO 14014），https://modeldiplomat.com/learn/glossary/ofac-burma-sanctions ⚠️。
  - **中国**：没有全面制裁，只需筛查实体清单和 SDN（推论）。
- **主机的风险（推论，无判例）**：GL D-2 授权的是 “by a U.S. person”，主机作为链条中的美国人也在授权范围内，不构成违法。真正的风险是：
  - 受制裁地区用户的流量从主机 IP 出去，会让主机 IP 进入银行、电商的风控信号（DCA 在 Honeygain 上就测到过 T-Bank 的流量，§1.4）。
  - 审查方可以通过枚举拿到主机 IP。如果主机是还有亲属在国内的海外移民，就存在人身风险 ❓。
- **结论（推论）**：审查国用户需要的是“连得上”，这件事交给**入口桥**解决：家宽或机房做入口，出口放在机房。家宽出口只给开放国家里需要“干净住宅 IP”的用户（YouTube、Reddit 验证码这类场景）。**两个池子必须分开。**

### 7c. 分配表和“不留日志”之间的矛盾
- **先例的做法**：
  - **Tor**：网络结构上就无法记录“谁用了哪个出口”。保护运营者靠公开声明自己是出口（反向 DNS、出口说明页）、DMCA §512 和 EFF。https://community.torproject.org/relay/community-resources/tor-exit-guidelines/ ✅。
  - **Psiphon**：“We never log your IP address”，但会记录会话元数据（国家、城市、ISP、时长、流量、连接的服务器），“deleted on an automated schedule”，https://psiphon.ca/en/privacy.html ✅。
  - **Bright Data**：先做 KYC，再 “monitors Residential network traffic 24/7”，和 VPN 的“不留日志”正好相反 ✅。
  - **Mysterium**：节点和用户对应关系的日志政策没找到 ❓。
- **“不留日志”在法庭上被检验过的例子**：
  - PIA：2016 年和 2018 年两次，https://torrentfreak.com/private-internet-access-no-logging-claims-proven-true-again-in-court-180606/ ⚠️。
  - ExpressVPN：2017 年土耳其，⚠️。
  - Windscribe：2025-04 在希腊撤诉，⚠️。
  - 这些案例保护的都是**公司**。在我们的模式里，执法部门首先找到的是**主机**。
- **欧盟的限制**：CJEU 在 C-511/18（2020）中只允许 “general and indiscriminate retention of IP addresses… for a period that is limited in time to what is strictly necessary”，用于国家安全或严重犯罪，https://fra.europa.eu/en/caselaw-reference/cjeu-joined-cases-c-51118-c-51218-and-c-52018-judgment ⚠️。GDPR 第 5(1)(c)/(e) 条要求写明保留目的和期限。
- **两个方案**（推论）：
  - **A 不留日志 + 法律辩护基金**：分配关系不落盘。主机遇到刑事调查时只能拿出“我是 Kaitu 出口”的证明信，处境和 Tor 运营者一样，照样会被搜查。没有先例，保险也很难买到。
  - **B 有时限地保留分配记录，并在隐私政策里写明**：只记“节点 X 在时间段 T 的候选用户集合（约 20 人）”，不记目标地址和内容，7–30 天后自动删除（期限是建议值，没有来源）。好处是能帮主机自证；代价是执法部门可以拿到这 20 人的名单。
- **建议（推论）**：**按池子分开处理。** 审查国用户只走“入口桥 + 机房出口”，不进入家宽出口圈，所以分配表里根本没有高敏感人群。家宽出口圈采用方案 B：只服务开放国家的、付款已实名的订户，在隐私政策里逐字写明保留什么、保留多久。这样“保护主机”和“保护审查国用户”这两件互相冲突的事，就分别落在两个池子里，不再互相牵制。

### 7d. 小圈分配下的经济账（全部为计算）
- **节点数** = 用户数 × 10 ÷ 20 = **用户数 ÷ 2**。1,000 个用户需要 500 个节点，1 万个用户需要 5,000 个节点。
- **每个节点对应的收入** = 20 个用户 × $6.6 ÷ 10 = **$13.2/月**，相当于每个节点只“养活” 2 个用户的收入。
- **每个节点的月流量**：用户总流量 × 每个节点的份额 = 2 × 30–50GB = **60–100GB/月**；按 2 倍计量，主机线路上是 120–200GB。带宽和流量上限都不是问题，问题在收入太薄。
- **成本对比（按每个用户每月算）**：
  - 寄硬件：Beryl AX 每月摊销约 $4.1 + 主机现金分成 $3–5，每个节点约 $7–9，**折合每个用户 $3.5–4.5/月**。
  - SolaDrive：每个用户 $1–2.5/月（§5.4）。**家宽方案贵 2–4 倍**，还没算客服、丢件、滥用处理。
  - 硬件一次性投入：每个用户约 $50（$99 ÷ 2），相当于 7.5 个月的订阅费。
- **对照市场的主机收入**：每个节点 60–100GB，按 Honeygain 实测 $0.11/GB，主机在市场上能拿 **$6.6–11/月**。我们每个节点的毛收入只有 $13.2，**付出同等水平的报酬后几乎没有利润**。
- **会员互助制（主机就是会员，回报是免会员费 + 少量分成）**：
  - 对主机来说，免会员费价值 $6.6/月，和市场价相当。对我们来说，现金成本是少收一份订阅费；在每个节点收入 $13.2 的结构下，净剩约 $6.6/节点。
  - 如果**不寄硬件**（主机在自己的电脑、NAS 或现有路由器上装软件），就没有硬件摊销，毛利为正。
  - 但这样就需要**一半的用户都当主机**（节点数 = 用户数 ÷ 2）。不现实（推论）。
- **调整比例**：每个用户分 3 个节点（1 个主节点 + 2 个备份），每个节点 20 人，节点数 = 用户数 × 3 ÷ 20 = **用户数 × 0.15**，每个节点收入约 $44/月。这时：
  - 会员互助制（免会员费 + 每月 $5 现金）的成本约 $11.6/节点，毛利率高。
  - 只需要约 15% 的用户愿意当主机。
  - 硬件寄送也勉强能做：$99 的设备约 3 个月回本。
- **结论**：**10:20 的分配比例在经济上不成立。要做，就改成约 3:20、会员互助、不寄硬件（或只给高价值主机寄）。**

---

## 8. 风险排序（从高到低，推论）

1. **主机被警察搜查或卷入刑事调查**：概率低，但后果严重，而且没有任何先例给出过保护手段。一旦发生，口碑和招募都会受连带打击。
2. **主机违反 ISP 合同、被销户**：在美国几乎是确定违约。被执行的概率不明 ❓，但每一位主机都承担着这个风险。
3. **IP 被标记，连累主机全家日常上网**：概率高，而且是长期的（§6）。
4. **经济上不成立**：10:20 比例 + 寄硬件 + 付现金，每个用户的成本是 SolaDrive 的 2–4 倍（§7d）。
5. **审查国用户与分配记录之间的冲突**：分配表可能暴露高敏感用户；做方案 A 则保护不了主机（§7c）。
6. **被归入“住宅代理”一类**：FBI PSA、Google、平台方的打击对象是这一整类，我们会受连带影响（§1.13）。
7. **欧盟寄硬件的合规问题**：RED 网络安全要求、CE（§3）。
8. **收款和制裁**：伊朗用户合法但收不到钱；需要做 SDN 筛查（§7b）。
9. **税务**：金额小，门槛 $2,000，风险低（§2.5）。

## 9. 缓解措施
- **先不寄硬件**：软件跑在主机已有的设备上，规避 RED、FCC、运费和丢件问题。确实要寄的话，只寄原厂 GL.iNet 设备、只装应用，不改射频参数。
- **只招募知情的主机**：协议里逐条写明 ISP 条款风险、搜查风险、IP 被标记的风险，主机签字确认；首选德国 Telekom（只禁止“独占使用”），或使用商用宽带的主机。
- **出口侧硬性控制**：封 25/465/587 端口；拦截 BitTorrent、端口扫描和大量并发的新连接；按用户、按节点限速；主机可以一键暂停。
- **用户侧**：付款实名，新用户先观察 30 天（期间走机房出口）；每个账号最多看到 2–3 个家宽节点；家宽出口只开放给开放国家的用户。
- **审查国用户**：走“入口桥 + 机房出口”，不进入家宽出口圈。
- **公司侧**：设滥用处理台，主机收到的投诉一律转到公司；给主机出具“出口节点”证明信，并预留法律费用（没有现成保险可买 ❓，需要询价）；建立 SCA 法律请求处理流程；在隐私政策里写明有时限的分配记录（方案 B）。
- **退出机制**：节点 IP 被标记，或主机收到 ISP 警告时，立即下线该节点，换新节点顶上。

## 10. 可行性结论

**原方案（寄路由器 + 付现金分成 + 每个用户 10 个节点 + 面向审查国用户的家宽出口）：No-go。** 理由有三：经济上算不过来（§7d）；美国主机几乎必然违约（§2.1）；先例里没有任何保护主机免受搜查的手段（§1.10、§4）。

**可以试点的条件（全部满足才 Go）：**
1. 主机是**知情的会员或熟人**，签了风险告知书；首批不寄硬件，或寄的不超过 20 台。
2. 家宽出口**只服务开放国家、付款实名、已过观察期**的订户。审查国用户走入口桥。
3. 分配比例改为每个用户约 3 个节点、每个节点 ≤20 人。主机回报 = 免会员费 + 少量现金。
4. 律师书面确认三件事：主机协议、SCA 应对流程、隐私政策中的分配记录条款。
5. 试点数据达到第 11 节的成功标准。

**直接 No-go 的信号**：试点期间任何一位主机收到警方联系或 ISP 销户通知；节点 IP 在 7 天内就被 Spur、IPQS、ipinfo 标记，导致家宽出口相对 SolaDrive 失去优势。

## 11. 最小试点设计
- **规模**：10–20 个主机，30–100 个付费用户（按 3:20 的比例，100 个用户约需 15 个节点）。
- **地区**：德国 5–10 户（Telekom 或 1&1 的光纤、VDSL 用户）+ 美国 5–10 户（优先 AT&T Fiber，上行快、不限量）。只选使用商用宽带或已知情接受 ToS 风险的主机。不做英国、中国大陆。
- **时长**：90 天，第 30 天和第 60 天各做一次中期评估。
- **流量范围**：只对开放国家的已核验用户开放；端口和协议限制全开；不接入审查国用户（入口桥另行试点）。
- **成功标准**（数值都是建议，没有来源）：
  1. **被标记**：每周用 Spur、IPQS、ipinfo、MaxMind 查一遍节点 IP，记录首次被打上 proxy/VPN 标签的天数。90 天内被标记的节点比例低于 50%，才算优于机房 IP。
  2. **验证码**：YouTube、Reddit、Google 的验证码和 403 发生率，与 SolaDrive 和机房出口做 A/B 对比，家宽出口要明显更低。
  3. **滥用事件**：主机收到的投诉、ISP 警告、警方联系次数为 0；任何一次警方联系即终止试点。
  4. **主机体验**：主机家庭自己遇到的验证码或封禁情况（问卷记录），留存率 ≥80%。
  5. **经济**：每个节点的实际用户数、流量（GB）、晚高峰上行占用，算出每个用户的真实成本，与 SolaDrive 的 $1–2.5/用户/月比较。
  6. **服务质量**：晚高峰时每个用户的可用带宽 ≥5 Mbps 的时段占比。
- **试点前必须完成**：律师审阅主机协议和隐私政策；滥用处理台流程；出口侧过滤规则；SCA 法律请求处理流程。

---

### 未能核实或缺口清单（❓）
Mysterium 的实际主机收入和条款原文；Sentinel、Orchid、Grass 的主机收入；任何为住宅出口主机提供法律援助或保险的先例；ISP 因为带宽分享实际销户的案例；FCC MBA 上行数据；从我们这里寄到主机手上的单件运费；Spur “24–48 小时”识别的原文；KPN 条款；荷兰电信法义务；“出租带宽”的税务定性；俄罗斯 GL 25 的最新版本；Mysterium 在审查国的使用数据；伊朗对住宅出口的封锁研究。
