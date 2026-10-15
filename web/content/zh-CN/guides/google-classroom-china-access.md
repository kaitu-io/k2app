---
title: "Google Classroom 在国内怎么登录：网页版和 App 打不开怎么办"
date: 2026-10-15
summary: "Google Classroom 在国内能下载 App，但网页版和 App 背后的服务都依赖 Google 账号、云端硬盘和 YouTube，这些在大陆受干扰或被封。本文按 GreatFire 记录和页面依赖分析说明卡在哪里，哪些事不用工具就能先做，以及什么情况下只能用加速工具。"
tags: ["Google Classroom", "Google 课堂", "Google Classroom 登录", "国际学校", "网课"]
brand: kaitu
draft: false
---

<strong>在中国大陆，Google Classroom（Google 课堂）基本无法直接使用。App 在中国区 App Store 能下载，但登录要用 Google 账号，作业和资料放在 Google 云端硬盘和文档里，视频多半来自 YouTube，这几项在大陆都受干扰或被封。</strong>孩子的学校如果用 Google Classroom，先和老师确认有没有其他交作业、拿资料的方式；确实要每天用的，通常只能借助加速工具。

<em>依据：GreatFire 的公开监测记录、Google 官方帮助页、2026 年 10 月 15 日对公开登录页的页面依赖分析（在大陆以外的网络中进行，没有登录）、Apple 中国区 App Store 的公开数据和评论，以及境外学校公开的 IT 说明。我们没有在大陆网络里测试过。资料核对日期：2026-10-15。</em>

这篇是[国际学校和留学家长常用的境外系统：国内访问情况汇总](/guides/intl-school-systems-china-access)中的一篇。

## 为什么在国内会卡

打开 classroom.google.com，网页会先跳转到 Google 账号登录页（accounts.google.com），登录成功才能进入课堂。进了课堂以后，作业附件、讲义和视频又分别来自 Google 的其他服务：

| 依赖 | 用来做什么 | 国内表现（来源） |
|---|---|---|
| classroom.google.com | 课堂本身 | <strong>不稳定，最近 2 次有效测试都受到干扰</strong>（[GreatFire](https://en.greatfire.org/https/classroom.google.com)） |
| accounts.google.com | 登录 Google 账号 | <strong>不稳定，最近 6 次有效测试都受到干扰</strong>（[GreatFire](https://en.greatfire.org/https/accounts.google.com)） |
| drive.google.com | 作业附件、学生上传的文件 | <strong>被封</strong>（[GreatFire](https://en.greatfire.org/https/drive.google.com)） |
| docs.google.com | 老师布置的 Google 文档、表格、幻灯片 | <strong>被封</strong>（[GreatFire](https://en.greatfire.org/https/docs.google.com)） |
| www.youtube.com | 课堂里嵌的教学视频 | <strong>被封</strong>（[GreatFire](https://en.greatfire.org/https/www.youtube.com)） |
| meet.google.com | 用 Google Meet 上直播课 | <strong>不稳定，最近 2 次有效测试都受到干扰</strong>（[GreatFire](https://en.greatfire.org/https/meet.google.com)） |

GreatFire 对 google.com 测过 <strong>2,923 个网址，结论是“大部分被封”</strong>（[GreatFire](https://en.greatfire.org/accounts.google.com)）。所以 Google Classroom 和 ManageBac 那类系统不一样：后者只是“用 Google 登录”这一个按钮会卡，而 Google Classroom 整个流程都离不开 Google 的服务。

另外：

- Google 官方的“Google Workspace 可用国家和地区”页面没有提到中国大陆（[Google 官方](https://knowledge.workspace.google.com/admin/support/troubleshooting/countries-or-regions-where-google-workspace-is-available?hl=en)）。Google Workspace 教育版注册页的国家列表里也没有中国大陆（香港、澳门、台湾都在）。我们没有找到 Google 官方明确写“中国大陆不能用”的说明，只能说明这两点。
- 中国区 App Store 里能下载“Google 课堂”App（[App Store](https://apps.apple.com/cn/app/google-%E8%AF%BE%E5%A0%82/id924620788)）。评论里有用户反映打开后“无法连接网络，请求超时”、上传作业总是失败、老师的文件看不了（这些评论多在 2020–2021 年）。<strong>能下载不等于能用</strong>，App 背后的服务和网页版是同一套。

## 先试这些（不需要任何工具）

这些办法没法让 Google Classroom 在国内变得能用，但可以减少因为网络问题耽误作业：

1. <strong>先和老师或学校确认替代方式。</strong>例如作业能不能通过邮件或学校的其他平台提交，资料能不能另外发一份。2020 年有在中国的国际学校老师公开分享过，因为 Google 在国内受限、多数家长没有加速工具，学校改用了 Seesaw 等平台（[International School Community，2020](https://internationalschoolcommunity.com/blog/shut-down-due-to-health-emergency-four-stories-from-international-schools-in-china-and-vietnam/)）。
2. <strong>把学校的 Google 邮件转发到国内能收的邮箱。</strong>课堂通知默认发到学校的 Google 邮箱。境外有的学校在给中国学生的 IT 说明里就是这样建议的（例如 [Whitman College](https://wcts-kb.atlassian.net/wiki/spaces/KB/pages/2462380)）。转发需要在能访问 Google 的网络下设置一次，具体步骤请问学校 IT。
3. <strong>回国前把这学期的资料下载到本地。</strong>孩子在境外读书、假期回国时，提前把要用的讲义和作业要求下载下来。
4. <strong>问学校有没有官方的远程访问方式。</strong>有的境外学校给在中国的学生提供过专门的访问服务或虚拟桌面。例如约克大学的 China Connect 服务，已在 2024 年 9 月停用（[University of York](https://www.york.ac.uk/students/news/2024/retiring-china-connect/)）。这类服务由学校提供，可以先问清楚孩子的学校有没有。

## 还是不行时

孩子每天都要在 Google Classroom 上看作业、交作业，学校又没有替代方式，在国内基本只能借助加速工具，让访问 Google 的流量走境外线路。

开途是其中一个选择：

- 开途默认是“智能分流”：国内网站和 App 照常直连，只有访问 Google 这类境外服务才走境外线路，不影响家里其他上网。
- 一个账号可以 5 台设备同时在线，孩子的电脑、平板和家长的手机可以一起用。
- 首次购买 7 天内、使用流量不超过 1GB 可以申请退款。详见[定价页](/pricing?utm_source=guide&utm_medium=content&utm_campaign=google-classroom-china-access)。

安装方法见[下载页](/install?utm_source=guide&utm_medium=content&utm_campaign=google-classroom-china-access)和[快速入门指南](/guides/getting-started)。

如果学校对网络访问方式有自己的规定，请先按学校的规定来。

## 常见问题

<strong>Google Classroom 在中国大陆能用吗？</strong>

基本不能直接用。Google Classroom 的登录、作业附件、文档和视频分别依赖 Google 账号、云端硬盘、Google 文档和 YouTube。GreatFire 的记录显示，这些服务在大陆要么被封，要么很不稳定。

<strong>中国区 App Store 能下载 Google 课堂，为什么还是打不开？</strong>

App 只是一个入口，背后连的还是 Google 的服务器。能在中国区 App Store 下载，不代表在国内能连上这些服务。不少评论反映打开后提示网络连接失败，或者作业上传不成功。

<strong>Google Classroom 网页版和电脑版有区别吗？</strong>

没有本质区别。电脑上一般直接用浏览器打开 classroom.google.com，手机上用 App。两者都要登录 Google 账号，在国内遇到的问题是一样的。

<strong>不用加速工具，有没有办法交 Google Classroom 的作业？</strong>

只能和老师商量替代方式，比如用邮件或学校的其他平台提交，或者请老师把资料另外发一份。Google Classroom 本身在国内没有能直接使用的入口。

<strong>Google 有没有官方说中国大陆不能用 Google Classroom？</strong>

我们没有找到 Google 官方的明确说明。能查到的是：Google Workspace 的“可用国家和地区”页面没有提到中国大陆，教育版注册页的国家列表里也没有中国大陆。资料核对日期：2026-10-15。

---

延伸阅读：[国际学校和留学家长常用的境外系统：国内访问情况汇总](/guides/intl-school-systems-china-access) · [k2cc 拥塞控制：丢包时怎么保持速度](/k2/k2cc)
