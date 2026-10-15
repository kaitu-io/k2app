---
title: "国际学校和留学家长常用的境外系统：国内访问情况汇总（2026 年 10 月）"
date: 2026-10-15
summary: "ManageBac、Toddle、College Board、Flywire 在国内能不能用？本文按系统汇总：有没有中国专用域名、登录页依赖哪些外部服务、官方怎么建议，每一行都附来源和核对日期，并链到单独的详细说明。适合国际学校和留学家庭的家长收藏转发。"
tags: ["国际学校", "留学", "ManageBac", "Toddle", "College Board", "Flywire"]
brand: kaitu
draft: true
---

<strong>国际学校和留学家庭常用的境外系统，大多数在国内能打开，只是不稳定。真正卡住的通常是其中某个外部服务，例如“用 Google 账号登录”、境外视频，或者第三方登录服务。</strong>有中国专用域名的系统（ManageBac 的 managebac.cn、Toddle 的 toddleapp.cn），先问学校能不能用中国版。没有中国版的，先按单页里不需要任何工具的办法试一遍。

<em>依据：各系统官方帮助中心、2026 年 10 月 15 日对公开登录页做的页面依赖分析（在大陆以外的网络中进行，只打开公开页面，不登录、不提交表单）、GreatFire 的公开监测记录，以及公开的用户反馈。我们没有在大陆网络里测试过，国内表现以表中注明的来源为准。</em>

## 汇总表

| 系统 | 用途 | 有没有中国专用域名或镜像 | 常见卡点（依赖了哪些外部服务） | 官方建议 | 详情链接 | 资料核对日期 |
|---|---|---|---|---|---|---|
| ManageBac | IB 学校的课程、作业、成绩和家长端 | <strong>有</strong>：managebac.cn，数据存放在中国境内，有 ICP 备案（[官方](https://help.managebac.com/hc/en-us/articles/360044964551-Security-Compliance)）；iOS App 在中国区 App Store 上架 | 国际版所有请求都要跨境；部分学校开了“用 Google 登录”（accounts.google.com 不稳定）；用 Google Drive 上传作业 | 中国学校从 www.managebac.cn/login 进入；.cn 学校的 O365 登录有问题时，回调地址改为 .cn；2020 年公告建议大文件避开高峰时段上传 | [ManageBac 单页](/guides/managebac-china-access) | 2026-10-15 |
| Toddle | IB / 国际学校的教学平台和家长端 | <strong>有</strong>：web.toddleapp.cn，以及中国区 App Store 的 Toddle Family China，和国际版完全分开 | 国际版家长登录页加载 Google 登录组件（accounts.google.com）和 Cloudflare 人机验证，接口在欧洲；中国版没有这两项，接口在国内 | 中国迁移指南需要登录才能看；按学校公开的家长指南，迁到中国服务器的学校要改用 .cn 和中国版 App | [Toddle 单页](/guides/toddle-china-access) | 2026-10-15 |
| College Board | AP、SAT 账号，查分，AP Classroom | <strong>没有</strong>；AP 本地报名信息在 Prometric 中国站（prometric.com.cn/apchina） | collegeboard.org 有部分网址受干扰（GreatFire 测了 15 个：3 个被封、6 个受干扰）；登录走 Okta（okta.com 测了 26 个，17 个受干扰）；登录页没有 Google 依赖 | 出分前先试登录、不要重复注册、用新版浏览器；Bluebook 考试流量要绕开 VPN 和代理 | [College Board 单页](/guides/college-board-china-access) | 2026-10-15 |
| Flywire（飞汇） | 付境外学费 | 没有中国专用域名，有中文站 flywire.com/zh 和中文付款页；选“付款来源国：中国”后可用人民币转账、支付宝、微信、银联 | 首页的 Vimeo 视频被封、“用 Google / Facebook 登录”在国内不可靠，这些都不影响付款；卡号页卡住多是网络超时 | <strong>付款时关闭 VPN</strong>，避开学校或公司 Wi-Fi；不要在微信里打开支付宝链接；信用卡页卡住就取消付款单重建 | [Flywire 单页](/guides/flywire-china-access) | 2026-10-15 |

后续会陆续补上 Schoology、PowerSchool、Google Classroom 等系统。

## 怎么看这张表

- <strong>有没有中国专用域名或镜像</strong>：有中国版的系统，数据和网页通常放在国内的服务器上，访问更快。但学校是用国际版还是中国版，是学校自己决定的，家长没法切换。可以在学校发的登录链接里看域名：以 .cn 结尾的，就是中国版。
- <strong>常见卡点</strong>：列的是我们在登录页上记录到的外部请求，以及 GreatFire 对这些域名的公开监测结果。“被封”只在 GreatFire 有明确记录时才写，其余写“受干扰”“不稳定”或“未测试”。
- <strong>官方建议</strong>：只写官方帮助中心或官方文档里的原话或要点，没有找到的写“未找到”。

## 不需要任何工具，先试这些

这几条对大多数系统都适用：

1. <strong>不要用“用 Google 登录”“用 Facebook 登录”。</strong>GreatFire 的记录显示，accounts.google.com 在大陆最近 6 次有效测试都受到干扰，connect.facebook.net 被封。如果学校允许，改用邮箱和密码登录，或者用 Microsoft 账号登录。
2. <strong>问学校有没有中国版。</strong>ManageBac 和 Toddle 都有中国版，网址分别以 managebac.cn 和 toddleapp.cn 结尾。
3. <strong>用电脑上的 Chrome、Edge 或 Safari 打开，不要在微信里直接点链接。</strong>
4. <strong>关掉浏览器的网页翻译插件。</strong>翻译插件会改写页面内容，可能导致登录后页面空白。
5. <strong>重要的日子提前试一次。</strong>出分日、缴费截止日、选课日之前，先登录一次确认能用。

## 还是不行时

如果按单页里的办法都试过了，家里的网络访问某个系统还是经常转圈，可以考虑用加速工具，让访问境外网站的流量走更稳定的线路。开途是其中一个选择：一个账号可以 5 台设备同时在线，首次购买 7 天内、使用流量不超过 1GB 可以申请退款，详见[定价页](/pricing?utm_source=guide&utm_medium=content&utm_campaign=intl-school-systems-china-access)，安装方法见[快速入门指南](/guides/getting-started)。

<strong>两种情况不要开加速工具</strong>：一是用 Flywire 等平台付学费（官方建议付款时关闭 VPN）；二是参加 Bluebook 等在线考试（官方要求考试流量绕开 VPN 和代理）。

## 常见问题

<strong>国际学校的 ManageBac、Toddle 在国内能用吗？</strong>

一般能用，而且这两个系统都有中国版：ManageBac 是 managebac.cn，Toddle 是 toddleapp.cn。学校用的是中国版的话，访问会更稳定。国际版的登录页如果提供“用 Google 登录”，这一步在国内容易卡住。

<strong>为什么学校系统在国内打得开，登录却一直转圈？</strong>

很多系统的登录环节依赖第三方服务，比如 Google 账号登录，或者 Okta 这样的身份服务。页面本身没有被封，但登录时要另外连接这些服务，其中某一个受干扰，就会一直转圈。

<strong>交学费或在线考试时可以开 VPN 吗？</strong>

不建议。飞汇官方建议付款时关闭 VPN。College Board 的 Bluebook 考试要求考试流量绕开 VPN 和代理。这两种场景都请按官方要求操作。

<strong>这张表多久更新一次？</strong>

每一行都写了资料核对日期。系统和网络情况都会变化，以核对日期最近的那一行为准。我们会定期重新核对，并把新的系统补进表里。

---

延伸阅读：[快速入门指南](/guides/getting-started) · [k2cc 拥塞控制：丢包时怎么保持速度](/k2/k2cc)
