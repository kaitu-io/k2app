---
title: "ManageBac 在国内打不开、很慢怎么办：登录入口和中国版说明"
date: 2026-10-15
summary: "ManageBac 在国内打不开或很慢？官方为中国学校提供 managebac.cn 中国版，数据放在国内。本文按官方帮助中心和登录页依赖分析，说明国际版和中国版的区别、家长端怎么登录、为什么“用 Google 登录”容易卡住，以及不用任何工具可以先试的办法。"
tags: ["ManageBac", "ManageBac 登录", "managebac.cn", "国际学校", "IB", "家长端"]
brand: kaitu
draft: false
section: study
---

<strong>ManageBac 有中国版。学校用的如果是以 managebac.cn 结尾的网址，数据和网页都在国内，一般不需要任何工具。</strong>在国内卡住的情况多数有三种：学校用的是国际版（managebac.com），用了“Sign in with Google”登录，或者上传作业时要用 Google Drive。先看清楚学校给的登录网址，再按下面的步骤试。

<em>依据：ManageBac 官方帮助中心和公告、2026 年 10 月 15 日对公开登录页做的页面依赖分析（在大陆以外的网络中进行，只打开登录页，没有登录）、GreatFire 的公开监测记录、App Store 公开评论。我们没有在大陆网络里测试过。资料核对日期：2026-10-15。</em>

这篇是[国际学校和留学家长常用的境外系统：国内访问情况汇总](/guides/intl-school-systems-china-access)中的一篇。

## ManageBac 中国版和国际版有什么区别

- <strong>中国版</strong>：官方写明，中国的学校使用 .cn 域名，数据存放在中国境内，并持有 ICP 备案（[官方 Security & Compliance](https://help.managebac.com/hc/en-us/articles/360044964551-Security-Compliance)）。我们打开一所学校 .cn 版的登录页，页面资源来自 assets.managebac.cn，学校图片来自 AWS 北京区域（cn-north-1）。
- <strong>国际版</strong>：网址以 managebac.com 结尾，按学校所在地托管在加拿大或美国的 AWS 数据中心（[官方 FAQ](https://help.managebac.com/hc/en-us/articles/360031510571-ManageBac-FAQs)）。
- <strong>账号不互通</strong>：Faria 统一账号也分两套，中国服务器是 accounts.faria.cn，全球服务器是 accounts.faria.org，<strong>中国服务器上的账号只能和中国服务器上的账号关联</strong>（[官方](https://help.managebac.com/hc/en-us/articles/360018226991-Using-the-Faria-Accounts-Portal-for-Single-Sign-on-SSO)）。

学校用哪个版本由学校决定，家长没法自己切换。看学校发来的欢迎邮件里的链接就能知道。

## 为什么在国内会卡

我们打开了 ManageBac 的学校查找页，以及国际版、中国版各一所学校的登录页，记录到的外部请求如下：

| 依赖 | 用来做什么 | 国内表现（来源） |
|---|---|---|
| 学校子域名 .managebac.com、assets.managebac.com | 国际版登录页和页面资源 | managebac.com 最近一次有效测试可访问（[GreatFire](https://en.greatfire.org/https/managebac.com)） |
| 学校子域名 .managebac.cn、assets.managebac.cn | 中国版登录页和页面资源 | managebac.cn 的 HTTPS 访问最近一次有效测试可访问（[GreatFire](https://en.greatfire.org/https/managebac.cn)） |
| /auth/google（转到 accounts.google.com） | 部分学校开了“Sign in with Google” | <strong>不稳定，最近 6 次有效测试都受到干扰</strong>（[GreatFire](https://en.greatfire.org/https/accounts.google.com)） |
| /auth/azureactivedirectory | 部分学校开了“Sign in with Microsoft” | 没有找到 GreatFire 对这一登录流程的记录 |
| fonts.googleapis.com | 学校查找页（signin.managebac.com）的网页字体 | 可访问（[GreatFire](https://en.greatfire.org/https/fonts.googleapis.com)） |
| js-agent.newrelic.com、bam.nr-data.net | 性能监控 | 不影响登录 |

我们看了几所学校的国际版登录页：有的只有邮箱和密码登录，有的同时提供 Google、Microsoft 或 Clever 登录。我们看的那所中国版学校只提供邮箱密码和 Microsoft 登录。所以<strong>同样叫 ManageBac，不同学校的卡点不一样</strong>：

- <strong>国际版</strong>：所有请求都要跨境，高峰时段慢是常见情况。ManageBac 在 2020 年给中国大陆学校的公告里也提到，国际带宽受限，已经在中国的数据中心加了代理（[官方公告](https://managebac.com/blog/message-schools-mainland-china)）。
- <strong>用 Google 登录</strong>：页面本身能打开，但点了“Sign in with Google”之后，要连到 accounts.google.com，这一步在国内很容易卡住。
- <strong>用 Google Drive 上传作业</strong>：官方的上传排查文章要求先在同一个浏览器登录 Google 账号，再用 “Add From GDrive”（[官方](https://help.managebac.com/hc/en-us/articles/62739177664153-What-to-do-if-an-upload-or-submission-fails)）。在国内，这一步同样依赖 Google。

## 先试这些（不需要任何工具）

1. <strong>用学校给的完整网址登录。</strong>找不到学校的网址时，官方给的入口是：全球学校用 www.managebac.com/login，<strong>中国学校用 www.managebac.cn/login</strong>（[官方](https://help.managebac.com/hc/en-us/articles/360017949832-Accessing-your-account-What-to-do-if-you-Forgot-your-Password)）。
2. <strong>家长第一次登录，要先用欢迎邮件里的链接设置密码。</strong>这个链接 <strong>7 天内有效</strong>，过期了请学校重新发（[官方家长入门](https://help.managebac.com/hc/en-us/articles/360045820491-Getting-Started-with-ManageBac-as-a-Parent)）。设好密码后用邮箱登录，不必用 Google 登录。
3. <strong>App 要先在浏览器里登录一次。</strong>官方说明，首次设置密码这一步 App 做不了（[官方 App 指南](https://help.managebac.com/hc/en-us/articles/360017949892-ManageBac-iOS-Android-app-mobile-QuickStart-Guide)）。iOS 的 ManageBac App 在中国区 App Store 可以下载（[App Store](https://apps.apple.com/cn/app/managebac/id1437382327)）。有几个孩子在同一所学校的，点右上角的头像切换孩子。
4. <strong>App 上传作业失败，就换电脑上的浏览器提交</strong>，单个文件不要超过 500 MB（官方上传排查文章）。
5. <strong>大文件放在晚上以外的时段上传。</strong>这是 ManageBac 2020 年公告里的建议：网络高峰时段，跨境上传更容易失败。
6. <strong>Microsoft 登录有问题，可以请学校 IT 核对设置。</strong>官方文档写明，使用 managebac.cn 的学校如果 O365 登录出问题，需要把回调地址从 .com 改成 .cn（[官方](https://help.managebac.com/hc/en-us/articles/360017949052-Enabling-Login-with-Microsoft-Microsoft-Entra-ID-Single-Sign-on)）。这一步只有学校管理员能改。

## 还是不行时

如果学校用的是国际版，或者学校要求用 Google 账号登录、用 Google Drive 交作业，在家里的宽带上确实经常卡住，可以考虑用加速工具。ManageBac 在 2020 年给中国大陆学校的公告里也提到过这种做法，并说明效果“无法保证”。

开途是其中一个选择：一个账号可以 5 台设备同时在线，孩子的电脑、平板和家长的手机可以一起用。首次购买 7 天内、使用流量不超过 1GB 可以申请退款，详见[定价页](/pricing?utm_source=guide&utm_medium=content&utm_campaign=managebac-china-access)。安装方法见[下载页](/install?utm_source=guide&utm_medium=content&utm_campaign=managebac-china-access)和[快速入门指南](/guides/getting-started)。

学校用的如果是 managebac.cn 中国版，通常不需要加速工具。卡住时，先按上面的步骤排查，再联系学校 IT。

## 常见问题

<strong>ManageBac 在中国能用吗？</strong>

能用。ManageBac 为中国的学校提供 .cn 中国版，数据存放在中国境内，并持有 ICP 备案。学校用的如果是国际版 managebac.com，也能打开，但所有请求都要跨境，高峰时段可能比较慢。

<strong>managebac.cn 和 managebac.com 有什么区别？</strong>

managebac.cn 是中国版，服务器和数据在国内；managebac.com 是国际版，托管在加拿大或美国。两边的账号不互通，学校用哪个版本由学校决定。看欢迎邮件里的登录链接，就知道你家孩子的学校用的是哪个。

<strong>ManageBac 家长端在哪里登录？</strong>

用学校欢迎邮件里的链接设置密码（7 天内有效），之后用学校的专属网址登录，也可以从官方入口 www.managebac.cn/login（中国学校）或 www.managebac.com/login（其他学校）找到自己的学校。iOS 版 App 在中国区 App Store 可以下载。

<strong>为什么 ManageBac 点“用 Google 登录”后一直转圈？</strong>

点了这个按钮后，浏览器要连到 Google 的登录服务器 accounts.google.com。GreatFire 的记录显示，这个地址在大陆最近 6 次有效测试都受到干扰。如果学校允许，改用邮箱和密码登录。

<strong>ManageBac 安卓版在哪里下载？</strong>

官方在 2024 年 9 月的更新日志里说，ManageBac+ 安卓版已经在腾讯应用宝上架。但我们在 2026 年 10 月从境外查询时没有找到这个页面，目前是否还在上架无法确认。建议先问学校 IT 推荐的下载方式。

---

延伸阅读：[Toddle 在国内怎么登录](/guides/toddle-china-access) · [国际学校和留学家长常用的境外系统：国内访问情况汇总](/guides/intl-school-systems-china-access)
