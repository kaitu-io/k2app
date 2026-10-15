---
title: "College Board 官网登录不进去、在国内打不开怎么办（2026 年 10 月）"
date: 2026-10-15
summary: "College Board 在国内打不开、登录转圈？主站部分网址受干扰，登录走 Okta 身份服务，也有不少干扰记录。本文按官方资料和页面依赖分析，说明卡在哪一步、哪些办法不用任何工具就能先试，以及 Bluebook 考试时为什么不能开加速工具。"
tags: ["College Board", "AP", "SAT", "College Board 打不开", "国际学校", "留学"]
brand: kaitu
draft: false
---

<strong>College Board 在国内能用，但不稳定：打开慢、登录页一直转圈、登录后跳不回来，都是常见情况。</strong>原因主要有两点。第一，collegeboard.org 主站有一部分网址在大陆受到干扰。第二，账号登录走的是第三方身份服务 Okta，而 okta.com 在大陆的干扰记录也不少。遇到问题时，先换一个直接的入口：AP Classroom 和 AP 成绩都从 My AP（myap.collegeboard.org）登录，用的是同一个 College Board 账号。并且要提前试登录，不要等到出分当天才发现进不去。<strong>Bluebook 考试时不要开任何加速工具</strong>，官方要求考试流量绕开 VPN 和代理。

<em>依据：College Board 官方帮助页和网络要求文档、2026 年 10 月 15 日对公开登录页的页面依赖分析（在大陆以外的网络中进行）、GreatFire 的公开监测记录，以及公开的用户反馈。我们没有在大陆网络里测试过。资料核对日期：2026-10-15。</em>

这篇是[国际学校和留学家长常用的境外系统：国内访问情况汇总](/guides/intl-school-systems-china-access)中的一篇。

## 为什么在国内会卡

用一个 College Board 账号就能登录所有服务，包括 AP、SAT 和 BigFuture（[官方说明](https://support.collegeboard.org/help-center/how-do-i-sign-my-account)）。但从点击“登录”到进入账号，浏览器要连接好几套不同的服务。下表是我们打开 account.collegeboard.org 登录页和 myap.collegeboard.org 时，记录到的主要外部请求：

| 依赖 | 用来做什么 | 国内表现（来源） |
|---|---|---|
| www.collegeboard.org 及 atlas、kiwi、athena 等子域 | 主站页面、样式和脚本 | GreatFire 对 collegeboard.org 测了 <strong>15 个网址：3 个被封、6 个受干扰、6 个可访问</strong>。主站首页的记录前后不一致：汇总页标为“被封”，单页最近一次有效测试（2026-04-25）显示可访问（[GreatFire](https://en.greatfire.org/collegeboard.org)） |
| prod.idp.collegeboard.org、login.okta.com、ok12static.oktacdn.com | 账号登录（Okta 身份服务） | GreatFire 对 okta.com 测了 <strong>26 个网址：3 个被封、17 个受干扰</strong>（[GreatFire](https://en.greatfire.org/okta.com)）。College Board 自己的登录子域没有单独测过 |
| myap.collegeboard.org | AP 学生入口（My AP） | 2026-03-19 测试时可访问（[GreatFire](https://en.greatfire.org/https/myap.collegeboard.org/login)） |
| cdn.cookielaw.org、geolocation.onetrust.com | Cookie 同意弹窗 | 没有找到监测记录 |
| fast.appcues.com、sdk.amazonaws.com | My AP 页面的引导组件和 AWS 脚本 | 没有找到监测记录 |

从这张表可以看出两点：

- <strong>登录页里没有发现 Google 的依赖</strong>（没有 reCAPTCHA，也没有 Google 账号登录）。所以 College Board 卡住，和“Google 被封”不是一回事。
- 问题更可能出在<strong>主站部分网址受干扰</strong>，以及<strong>登录要经过 Okta</strong>。主页能打开、点登录后一直转圈，往往就是卡在 Okta 这一步。这是我们根据依赖结构做出的推断，没有在大陆网络里验证过。

另外，AP Classroom 的网址虽然是 apclassroom.collegeboard.org，但实际托管在第三方平台上。学校的网络如果只放行了 collegeboard.org，AP Classroom 也可能打不开。

## 先试这些（不需要任何工具）

1. <strong>直接打开具体服务的入口，不要从主站首页一层层点进去。</strong>查 AP 成绩、加入班级用 [My AP](https://myap.collegeboard.org/)；管理账号用 account.collegeboard.org。GreatFire 的记录里，myap 和查分页（apscore）都显示可访问，主站首页的记录则不一致。
2. <strong>出分日、报名截止日之前，先登录一次试试。</strong>College Board 官方公众号转载的查分指南也是这样建议的：提前测试能不能登录，不要重复注册账号（[网易转载，2026-07-06](https://c.m.163.com/news/a/L16JOIOG0556BFNP.html)）。
3. <strong>用新版的 Chrome、Edge、Safari 或 Firefox</strong>，不要用 IE 或某些浏览器的“兼容模式”（同上）。
4. <strong>登录后页面空白时，把浏览器语言改成英文，关掉网页翻译插件。</strong>这是知乎上一些用户分享的经验（[知乎](https://zhuanlan.zhihu.com/p/476836566)），官方没有这样说过。
5. <strong>报名问题先找学校的 AP 协调员。</strong>按 College Board 的说明，中国本地学校的学生和自学考生通过 Prometric 管理的考点报考，要按 Prometric 的中国时间表，在考试前一年秋季报名；国际学校的 AP 考点通常只接受本校学生（[Taking AP in China](https://international.collegeboard.org/students/ap/taking-ap-china)、[Prometric 中国 AP 站](https://www.prometric.com.cn/apchina)）。
6. <strong>Bluebook 机考请用考点或学校提供的网络。</strong>官方的网络配置文档要求，<strong>`*.collegeboard.org` 和 `*.sentry.io` 的流量要绕开防火墙、内容过滤、VPN、代理服务器和私人中继</strong>（[Bluebook 网络配置](https://bluebook.collegeboard.org/technology/networks/configure)）。考试网络是考点负责的，考生不用自己想办法。

## 还是不行时

如果是<strong>平时</strong>查分、看 AP Classroom 视频、管理账号，在家里的宽带上还是经常转圈，可以考虑用加速工具，让访问境外网站的流量走更稳定的线路。开途是其中一个选择：一个账号可以 5 台设备同时在线，首次购买 7 天内、使用流量不超过 1GB 可以申请退款，详见[定价页](/pricing?utm_source=guide&utm_medium=content&utm_campaign=college-board-china-access)。安装方法见[下载页](/install?utm_source=guide&utm_medium=content&utm_campaign=college-board-china-access)和[快速入门指南](/guides/getting-started)。

两点提醒：

- <strong>Bluebook 正式考试时，不要开任何加速工具。</strong>官方要求考试流量绕开 VPN 和代理，开着反而可能出问题。考试网络由考点负责。
- 加速工具解决的是“连不上、很慢”。如果是账号被锁、密码错误这类问题，请用登录页上的 “Unlock account?” 或 [Student Help](https://support.collegeboard.org/) 处理。

## 常见问题

<strong>College Board 在中国被封了吗？</strong>

没有被整站封掉，但有一部分网址受到干扰。GreatFire 对 collegeboard.org 的 15 个网址做过测试：3 个被封，6 个受干扰，6 个可以访问。所以会出现有的页面打得开、有的打不开。资料核对日期：2026-10-15。

<strong>为什么 College Board 主页能打开，点登录却一直转圈？</strong>

College Board 的账号登录用的是第三方身份服务 Okta，登录时浏览器要另外连接 Okta 的服务器。GreatFire 对 okta.com 的 26 个网址做过测试，其中 17 个受到干扰。可以先直接打开 myap.collegeboard.org 再登录，并且避开出分当天的高峰时段。

<strong>AP 查分一定要用 VPN 吗？</strong>

不一定。官方没有要求查分用 VPN。网上有“按 IP 分时区放分”的说法，但 College Board 官方转载的文章写的是全球同步发布。建议提前几天先试一次能不能登录，确实登不上，再考虑加速工具。

<strong>Bluebook 考 SAT 或 AP 时可以开加速工具吗？</strong>

不建议开。官方网络文档要求，College Board 和 Sentry 的考试流量要绕开 VPN 和代理。考试用考点提供的网络，网络问题由考点负责处理。

<strong>College Board 有中文官网或中国专用网址吗？</strong>

没有找到中文的官方帮助中心，也没有找到中国专用的网址。面向中国学生的 AP 报名信息在 Prometric 中国 AP 站（prometric.com.cn/apchina），具体流程以学校 AP 协调员的通知为准。

---

延伸阅读：[国际学校和留学家长常用的境外系统：国内访问情况汇总](/guides/intl-school-systems-china-access) · [k2cc 拥塞控制：丢包时怎么保持速度](/k2/k2cc)
