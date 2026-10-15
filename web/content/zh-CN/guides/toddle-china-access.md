---
title: "Toddle 在国内怎么登录：中国版入口、家长 App 下载和常见卡点"
date: 2026-10-15
summary: "Toddle 在国内登录不上或很慢？Toddle 为中国大陆的学校单独提供 web.toddleapp.cn 和 Toddle Family China App，和国际版完全分开。本文根据登录页依赖分析和公开资料，说明两个版本的区别、家长该用哪个入口，以及国际版为什么容易卡在 Google 登录和人机验证。"
tags: ["Toddle", "Toddle 登录", "Toddle Family China", "toddleapp.cn", "国际学校", "家长端"]
brand: kaitu
draft: false
---

<strong>Toddle 为中国大陆的学校单独提供了中国版：网页是 web.toddleapp.cn，iPhone 上用中国区 App Store 里的 Toddle Family China。</strong>中国版和国际版（web.toddleapp.com、Toddle Family）的服务器完全分开。学校如果已经迁到中国服务器，家长就要用中国版登录，用国际版是登不进去的。学校如果还在用国际版，登录页依赖 Google 登录和 Cloudflare 的人机验证，在国内更容易卡住。

<em>依据：Toddle 官方网站和帮助中心（中国迁移指南需要登录才能看，我们只看到摘要）、一所国内国际学校公开的家长指南、2026 年 10 月 15 日对国际版和中国版家长登录页做的页面依赖分析（在大陆以外的网络中进行，只打开登录页，没有登录）、Apple 中国区 App Store 的公开数据和评论，以及 GreatFire 的公开监测记录。我们没有在大陆网络里测试过。资料核对日期：2026-10-15。</em>

这篇是[国际学校和留学家长常用的境外系统：国内访问情况汇总](/guides/intl-school-systems-china-access)中的一篇。

## Toddle 中国版和国际版有什么区别

- <strong>服务器是隔开的。</strong>Toddle 帮助中心有一篇《Migration guide for schools moving to China data center》（[官方，需要登录](https://help.toddleapp.com/en/articles/9531001-migration-guide-for-schools-moving-to-china-data-center)）。按搜索结果里的摘要：Toddle 在中国大陆有独立的服务器，和全球服务器完全隔开；学校迁移后，数据会转到 web.toddleapp.cn。一所国内国际学校的家长指南也是这么写的（[学校家长指南，2025-09](https://www.qiss.org.cn/blog/toddle-parent-tutorial-qiss-guide-for-parents/)）。
- <strong>App 也分两套。</strong>Apple 中国区 App Store 里有 Toddle Family China、Toddle Student China、Toddle Educator China 三个 App，开发者是一家北京公司（[Toddle Family China](https://apps.apple.com/cn/app/id6474382315)）。国际版的 Toddle Family 在中国区 App Store 里搜不到。
- <strong>国际版</strong>：网页是 web.toddleapp.com，App 是 Toddle Family。

学校用哪个版本由学校决定。学校已经迁到中国服务器的，家长按学校通知改用中国版即可。

## 为什么在国内会卡

我们分别打开国际版和中国版的“Family account”登录页，记录到的外部请求如下：

| 依赖 | 用来做什么 | 国际版 | 中国版 | 国内表现（来源） |
|---|---|---|---|---|
| accounts.google.com/gsi/client | “用 Google 登录”按钮 | 有 | <strong>没有</strong> | <strong>不稳定，最近 6 次有效测试都受到干扰</strong>（[GreatFire](https://en.greatfire.org/https/accounts.google.com)） |
| challenges.cloudflare.com（Turnstile） | 用邮箱或手机号登录时的人机验证 | 有 | <strong>没有</strong>（验证配置为关闭） | 判定为可访问，但这次判定修订过，之前曾标为“部分受干扰”（[GreatFire](https://en.greatfire.org/https/challenges.cloudflare.com)） |
| 接口服务器 | 登录和数据 | eu-west-1 区域（欧洲） | cn-north-1 区域（中国） | GreatFire 没有测试过 |
| widget.intercom.io、sentry.io | 客服窗口、错误上报 | 有 | 没有 | 不影响登录 |
| fonts.googleapis.com、fast.fonts.net | 网页字体 | 有 | 有 | fonts.googleapis.com 可访问（[GreatFire](https://en.greatfire.org/https/fonts.googleapis.com)）；fast.fonts.net 没有找到记录 |

国际版的家长登录页提供邮箱或手机号、Google、Microsoft、Lex 和“Your school”几种登录方式；中国版只提供邮箱、Microsoft 和“Your school”。中国版去掉了 Google 登录和人机验证，接口也放在国内，这正是国际版在国内容易卡住的几个环节。

## 先试这些（不需要任何工具）

1. <strong>先确认学校用的是哪个版本。</strong>看学校的通知或邀请邮件里的网址：以 toddleapp.cn 结尾的就是中国版。学校已经迁到中国服务器的，再打开国际版 web.toddleapp.com，会提示你改去 .cn（据上面那份学校家长指南）。
2. <strong>iPhone 用中国区 App Store 的 Toddle Family China。</strong>据学校家长指南，Apple ID 是其他国家或地区的，可以用国际版 Toddle Family，退出登录后在 App 设置里切换到中国服务器。安卓手机可以在腾讯应用宝里搜索 Toddle。
3. <strong>第一次登录，要用学校给的家庭邀请码。</strong>按 Toddle 官方说明，首次注册时选“Create new”，输入 family invite code，再填写邮件里收到的验证码（[官方](https://support.toddleapp.com/en/articles/8612346-how-do-i-sign-up-as-a-first-time-user-on-toddle-as-a-family-member)）。
4. <strong>验证码邮件来得慢，耐心等一下再输入。</strong>中国区 App Store 里有家长反映，验证码邮件要 10–20 秒才到，而有效时间比较短，试了好几次才登上（[App Store 评论](https://apps.apple.com/cn/app/id6474382315)）。收到后尽快输入；过期了就重新获取。
5. <strong>用国际版时，不要选“用 Google 登录”。</strong>如果学校允许，改用邮箱、手机号或 Microsoft 账号登录。

## 还是不行时

学校还在用国际版，家里的网络打开 web.toddleapp.com 经常转圈、验证码页面加载不出来，可以考虑用加速工具。开途是其中一个选择：一个账号可以 5 台设备同时在线，首次购买 7 天内、使用流量不超过 1GB 可以申请退款，详见[定价页](/pricing?utm_source=guide&utm_medium=content&utm_campaign=toddle-china-access)。安装方法见[下载页](/install?utm_source=guide&utm_medium=content&utm_campaign=toddle-china-access)和[快速入门指南](/guides/getting-started)。

学校已经迁到中国版的，一般不需要加速工具。中国版 App 里有学生反映网络卡、文件打不开（[App Store 评论](https://apps.apple.com/cn/app/id6474382166)），这类问题请先反馈给学校。

## 常见问题

<strong>Toddle 中国版的网址是什么？</strong>

网页版是 web.toddleapp.cn，国际版是 web.toddleapp.com。两边服务器是分开的。学校的数据迁到中国服务器后，要用 .cn 的网址登录。

<strong>Toddle Family China 和 Toddle Family 有什么区别？</strong>

Toddle Family China 是给使用中国服务器的学校用的家长 App，在中国区 App Store 下载；Toddle Family 是国际版，在中国区 App Store 里搜不到。用哪个，取决于孩子的学校用的是哪个服务器。

<strong>为什么 Toddle 登录页一直转圈？</strong>

如果用的是国际版，登录页要加载 Google 登录组件和 Cloudflare 的人机验证，接口服务器也在欧洲，这几处在国内都可能变慢或卡住。中国版去掉了 Google 登录和人机验证，接口在国内。可以先问学校能不能用中国版。

<strong>Toddle 和 ManageBac 是一家公司的吗？</strong>

不是。两者都是 IB 学校常用的教学平台，但属于不同的公司。它们都为中国学校提供了单独的中国版：ManageBac 的中国版域名是 managebac.cn，Toddle 的是 toddleapp.cn。

---

延伸阅读：[ManageBac 在国内打不开、很慢怎么办](/guides/managebac-china-access) · [国际学校和留学家长常用的境外系统：国内访问情况汇总](/guides/intl-school-systems-china-access)
