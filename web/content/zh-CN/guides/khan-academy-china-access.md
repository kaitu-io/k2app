---
title: "Khan Academy（可汗学院）在国内怎么登录、视频放不了怎么办"
date: 2026-10-15
summary: "Khan Academy 在国内能用吗？官方说明，在中国会改用自有播放器，不依赖 YouTube；中文版在 zh.khanacademy.org，App 和 Khan Academy Kids 也都在中国区 App Store 上架。本文按官方帮助中心和页面依赖分析，说明视频放不了、登录卡住的原因和办法。"
tags: ["Khan Academy", "可汗学院", "Khan Academy Kids", "网课", "可汗学院中文版"]
brand: kaitu
draft: false
---

<strong>Khan Academy（可汗学院）在国内大体可以用，而且官方专门为中国做了处理：YouTube 在中国被封时，网站会改用自己的视频播放器。</strong>常见的卡点有两个：一是用“Continue with Google”登录，二是视频没有切换到自有播放器，一直在等 YouTube。在国内请用邮箱或用户名登录；中文内容可以直接上 zh.khanacademy.org。

<em>依据：Khan Academy 官方帮助中心、2026 年 10 月 15 日对公开登录页和视频页做的页面依赖分析和前端代码阅读（在大陆以外的网络中进行，没有登录）、GreatFire 的公开监测记录、Apple 中国区 App Store 的公开数据。我们没有在大陆网络里测试过。资料核对日期：2026-10-15。</em>

这篇是[国际学校和留学家长常用的境外系统：国内访问情况汇总](/guides/intl-school-systems-china-access)中的一篇。

## 为什么在国内会卡

官方帮助中心有一篇专门的文章《Can I access Khan Academy from China and Iran?》，原文说：<strong>“In cases when YouTube is blocked in China and Iran, we use alternative video players to show the instructional videos.”</strong>（[官方，2026-10-09 更新](https://support.khanacademy.org/hc/en-us/articles/202487590-Can-I-access-Khan-Academy-from-China-and-Iran)）也就是说，YouTube 被封时，网站会改用其他播放器播放教学视频。

我们打开了登录页和一个视频页，记录到的主要外部请求如下：

| 依赖 | 用来做什么 | 国内表现（来源） |
|---|---|---|
| www.khanacademy.org | 网站本身 | 最近一次有效测试可访问；khanacademy.org 测了 21 个网址，19 个可访问、2 个受干扰（[GreatFire](https://en.greatfire.org/khanacademy.org)） |
| cdn.kastatic.org | 页面资源，以及中国用的视频文件 | 可访问（[GreatFire](https://en.greatfire.org/cdn.kastatic.org)） |
| www.youtube-nocookie.com、www.youtube.com | 在中国以外的网络里播放视频 | <strong>被封</strong>（[GreatFire](https://en.greatfire.org/https/www.youtube-nocookie.com)） |
| accounts.google.com | 登录页的“Continue with Google” | <strong>不稳定，最近 6 次有效测试都受到干扰</strong>（[GreatFire](https://en.greatfire.org/https/accounts.google.com)） |
| fonts.googleapis.com、googletagmanager、trustarc | 字体、统计、Cookie 弹窗 | fonts.googleapis.com 可访问（[GreatFire](https://en.greatfire.org/https/fonts.googleapis.com)） |

我们从境外打开视频页，视频是用 YouTube 播放器嵌入的。读前端代码可以看到，网站判断访客的 IP 在中国时就不用 YouTube，改为播放 cdn.kastatic.org 上的视频文件；YouTube 播放器 5 秒内没准备好，也会切换到备用播放器。

由此推断，在国内“视频放不了”可能有两种情况：

- 网站没有把你识别为中国访客，比如开着公司专线或其他代理，结果还在等 YouTube。
- 视频文件从境外下载慢。官方建议每台设备播放标清视频需要约 <strong>1.5 Mbps</strong> 带宽（[官方](https://support.khanacademy.org/hc/en-us/articles/202487500-Why-are-Khan-Academy-videos-not-playing-for-me)）。

这两点都是我们根据代码和官方说明做的推断，没有在大陆网络里验证过。

## 先试这些（不需要任何工具）

1. <strong>用邮箱或用户名加密码登录，不要用 Google。</strong>Khan Academy 社区里有回复提到，在中国可能无法用 Google 账号登录，建议提前设置好用户名和密码（[社区帖](https://support.khanacademy.org/hc/en-us/community/posts/360047990012-Google-Khan-Academy-Account)）。这是社区回复，我们没能确认回复人是不是官方员工。
2. <strong>学中文内容直接上中文版。</strong>官方给出的中文入口是 zh-hans.khanacademy.org，实际会跳到 <strong>zh.khanacademy.org</strong>，界面是简体中文。GreatFire 的记录显示可以访问。
3. <strong>视频转圈时刷新一下，等几秒。</strong>网站在 YouTube 加载不出来时，会切换到备用播放器。
4. <strong>用 App。</strong>Khan Academy 和 Khan Academy Kids 现在都能在中国区 App Store 下载（[Khan Academy](https://apps.apple.com/cn/app/khan-academy/id469863705)、[Khan Academy Kids](https://apps.apple.com/cn/app/khan-academy-kids/id1378467217)）。网上有不少“Kids 在国区下载不了”的说法，那是 2018–2020 年的情况，我们 2026 年 10 月查询时两款 App 都在国区上架。
5. <strong>换浏览器，保证带宽。</strong>官方建议用受支持的浏览器并更新到最新版。同一个网络里同时有几台设备在看视频的话，每台大约需要 1.5 Mbps。
6. <strong>网络实在不好时，试试 Kolibri。</strong>官方帮助文章提到，连接有问题时可以试 Kolibri。这是一个可以离线使用的学习工具，能提前下载课程内容。

## 还是不行时

如果孩子用的是学校布置的 Khan Academy 作业，必须用 Google 账号登录；或者视频经常卡在加载，可以考虑用加速工具。开途是其中一个选择：默认“智能分流”，国内网站照常直连，只有访问境外网站才走境外线路；一个账号可以 5 台设备同时在线。首次购买 7 天内、使用流量不超过 1GB 可以申请退款，详见[定价页](/pricing?utm_source=guide&utm_medium=content&utm_campaign=khan-academy-china-access)。安装方法见[下载页](/install?utm_source=guide&utm_medium=content&utm_campaign=khan-academy-china-access)和[快速入门指南](/guides/getting-started)。

如果不开工具时视频本来就能放、也能正常登录，就没有必要开。

## 常见问题

<strong>Khan Academy 在中国能用吗？</strong>

大体可以用。官方说明，在中国 YouTube 被封时会改用其他播放器播放教学视频。GreatFire 的记录显示网站主体可以访问。在国内登录时，建议用邮箱或用户名，不要用 Google 账号。

<strong>可汗学院中文版网址是什么？</strong>

官方给出的入口是 zh-hans.khanacademy.org，打开后会跳到 zh.khanacademy.org，界面和课程都是简体中文。

<strong>Khan Academy 视频放不了怎么办？</strong>

先刷新页面，等几秒，网站会在 YouTube 加载不出来时切换到备用播放器。还不行的话，检查网速（官方建议每台设备约 1.5 Mbps），换用最新版浏览器，或者改用 App。

<strong>Khan Academy Kids 在中国能下载吗？</strong>

能。我们 2026 年 10 月查询中国区 App Store 时，Khan Academy Kids 已经上架。网上“国区下载不了”的说法来自 2018–2020 年，已经过时。

<strong>Khan Academy 登录不上怎么办？</strong>

如果你用的是“Continue with Google”，这一步在国内很容易卡住。可以在能访问 Google 的网络下给账号设置用户名和密码，之后在国内用用户名和密码登录。

---

延伸阅读：[国际学校和留学家长常用的境外系统：国内访问情况汇总](/guides/intl-school-systems-china-access) · [Google Classroom 在国内怎么登录](/guides/google-classroom-china-access)
