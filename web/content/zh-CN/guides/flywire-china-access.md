---
title: "Flywire（飞汇）中国官网怎么进、在国内交学费付款失败怎么办"
date: 2026-10-15
summary: "在国内用 Flywire（飞汇）交境外学费，多数问题出在付款方式选错、在微信里打开链接、学费账单审核不通过，或者网络超时。本文按飞汇官方帮助中心的资料，整理人民币转账、支付宝、微信、银联各自的限制和常见报错，并说明官方为什么建议付款时关闭 VPN。"
tags: ["Flywire", "飞汇", "交学费", "留学", "国际学校", "Flywire 国内"]
brand: kaitu
draft: false
---

<strong>Flywire（飞汇）在国内可以正常使用，付款时一般也不需要任何加速工具。官方帮助中心甚至明确建议：卡号页加载不出来时，先关闭 VPN。</strong>飞汇的中文官网是 flywire.com/zh，中文付款入口是 pay.flywire.com/zh。在国内交学费失败，常见原因有四个：选的付款方式不适合（例如付款人不是直系亲属）、在微信里直接打开支付宝链接、学费账单审核没通过，以及网络超时导致付款单作废。下面逐一说明。

<em>依据：飞汇官方帮助中心（help.flywire.com，下文引用的文章在 2026 年 9–10 月更新过）、国家外汇管理局分局的公开问答、2026 年 10 月 15 日对公开付款页的页面依赖分析（在大陆以外的网络中进行），以及公开的用户评价。我们没有在大陆网络里测试过。资料核对日期：2026-10-15。</em>

这篇是[国际学校和留学家长常用的境外系统：国内访问情况汇总](/guides/intl-school-systems-china-access)中的一篇。

## 为什么在国内会卡

飞汇有中文网站（[flywire.com/zh](https://www.flywire.com/zh)）和中文付款入口（pay.flywire.com/zh），帮助中心也有简体中文版。付款时把“付款来源国”选为中国，页面就会列出人民币付款方式。我们没有找到 flywire.cn 这类中国专用网址。

我们打开付款首页和付款人登录页，记录到的主要外部请求如下：

| 依赖 | 用来做什么 | 国内表现（来源） |
|---|---|---|
| pay.flywire.com、payment.flywire.com | 付款页本身（托管在 Cloudflare） | GreatFire 还没有测试过 |
| fonts.googleapis.com、fonts.gstatic.com | 网页字体 | 可访问（[GreatFire](https://en.greatfire.org/https/fonts.googleapis.com)） |
| player.vimeo.com | 付款首页里的介绍视频 | <strong>被封</strong>（[GreatFire](https://en.greatfire.org/https/player.vimeo.com)） |
| accounts.google.com | 登录页的“用 Google 登录” | <strong>不稳定，最近 6 次有效测试都受到干扰</strong>（[GreatFire](https://en.greatfire.org/https/accounts.google.com)） |
| connect.facebook.net | 登录页的“用 Facebook 登录”，以及广告统计 | <strong>被封</strong>（最近一次测试为 2026-05-27，[GreatFire](https://en.greatfire.org/https/connect.facebook.net)） |
| js.hcaptcha.com | 登录页的人机验证 | 可访问（[GreatFire](https://en.greatfire.org/https/js.hcaptcha.com)） |
| googletagmanager、heapanalytics、6sc.co、doubleclick 等 | 访问统计和广告追踪 | 不影响付款流程 |

结论：

- 首页的视频和统计脚本加载不出来，不影响付款，<strong>付款页本身没有被封锁的证据</strong>。
- <strong>不要用 Google 或 Facebook 账号登录飞汇</strong>，这两个登录方式在国内很可能卡住。改用邮箱和密码登录。
- 真正容易出问题的，是银联、信用卡等的卡号输入页（由收单方提供）。官方说，这一页卡住通常是网络超时或者浏览器问题。

## 先试这些（不需要任何工具）

### 先选对付款方式

下表整理自飞汇官方帮助中心：

| 方式 | 谁能付 | 单笔上限 | 要上传的文件 | 常见坑 |
|---|---|---|---|---|
| 人民币国内转账 | 持大陆身份证的本人 | <strong>100 万元</strong> | 学校正式出具的学费账单 | 必须从下单时填写的付款人本人账户转出，金额要精确；不接受对公账户（[官方](https://help.flywire.com/hc/en-us/articles/360012920574-Common-questions-RMB-bank-transfer)） |
| 支付宝 | 学生和付款人都要有大陆身份证；付款人只能是本人、父母、祖父母、配偶或兄弟姐妹 | <strong>30 万元</strong>（等值 350 美元以上才会显示这个选项） | 录取信、学费账单或缴费通知 | 在微信里打开会报 ILLEGAL_SIGN 错误（[官方](https://help.flywire.com/hc/en-us/articles/360019587898-Common-questions-Paying-with-Alipay-in-CNY)） |
| 微信支付 | 同支付宝 | <strong>30 万元</strong> | 学费账单和在读证明两份 | 转出的卡和小程序里选的卡不一致、金额不对，都会被自动退款（[官方](https://help.flywire.com/hc/en-us/articles/10387441618717-Common-Questions-WeChat-Pay)） |
| 银联卡 | 不限国籍和关系 | 受卡片限额约束 | 不需要 | 付款单 24 小时后失效，需要短信验证码（[官方](https://help.flywire.com/hc/en-us/articles/360021360334-How-to-make-a-UnionPay-payment-in-CNY-from-China)） |
| 外币电汇 | 在国内银行购汇后汇出 | 受外汇额度约束 | 银行可能要求出示授权函 | 费用承担方式选 OUR；银行不支持的话，多汇约 30 美元（[官方](https://help.flywire.com/hc/zh-cn/articles/360015673420)） |

### 网页打不开、卡号页一直转圈

1. <strong>不要在微信里直接打开付款链接。</strong>复制链接，用电脑上的 Chrome 等浏览器打开。这是飞汇官方对支付宝报错给出的办法。
2. <strong>关闭 VPN 和代理，换成家里的宽带或手机流量，不要用学校或公司的 Wi-Fi。</strong>这是飞汇在银联常见问题里给出的原话（"Disable VPN if enabled, and avoid public or restricted networks such as school or office Wi-Fi"，[官方](https://help.flywire.com/hc/en-us/articles/6167058549533-Common-questions-Paying-with-UnionPay-in-CNY)）。
3. <strong>换浏览器、用无痕窗口、清除缓存。</strong>付款过程中不要刷新，也不要后退。
4. <strong>信用卡页面卡住时，这张付款单就作废了</strong>，要取消后重新下单（[官方](https://help.flywire.com/hc/en-us/articles/360010750440-Common-questions-Paying-with-online-Credit-Card-in-CNY)）。

### 付了钱但状态一直不更新

- 人民币转账一般 1–2 个工作日确认，超过 72 个工作小时还没更新，带上银行回单联系客服。
- 外币电汇超过 3 个工作日没到账，向银行要 MT103 或 PACS008 交易凭证，交给飞汇客服查询（[官方](https://help.flywire.com/hc/en-us/articles/35044852848285-Common-Questions-International-Wire-Transfers-from-China)）。
- 邮件里的追踪链接 <strong>90 天有效</strong>。建议把付款关联到飞汇账户，以后在“我的付款”里查看。

### 购汇额度

国家外汇管理局规定，个人购汇有<strong>每人每年等值 5 万美元</strong>的便利化额度。凭境外学校录取通知书、学费证明等材料，可以办理超出额度的留学购汇（[外汇局天津分局，2025-12](https://www.safe.gov.cn/tianjin/2025/1223/2957.html)、[外汇局广东分局](https://www.safe.gov.cn/guangdong/2018/0913/1198.html)）。具体要哪些材料，以办理银行的要求为准。

## 还是不行时

<strong>交学费这一步，建议按飞汇官方的要求，先断开 VPN 和任何加速工具。</strong>开途默认的“智能分流”会让支付宝、国内银行这类国内服务直接连接，不经过境外线路；但飞汇自己的付款页是境外网站，开着开途时会走境外线路。我们没有验证过开着开途付款会不会触发飞汇或发卡行的风控。涉及学费，不值得冒这个险。按上面的步骤换浏览器、换网络，一般就能解决。

加速工具只在<strong>付款以外</strong>的场景有帮助：比如学校发来的缴费说明里嵌了境外视频，或者学校的缴费系统本身登录要用 Google 账号。遇到这类情况，可以考虑开途这类工具，<strong>但付款前请先把它关掉</strong>。开途首次购买 7 天内、使用流量不超过 1GB 可以申请退款，详见[定价页](/pricing?utm_source=guide&utm_medium=content&utm_campaign=flywire-china-access)；安装方法见[快速入门指南](/guides/getting-started)。

## 常见问题

<strong>Flywire 在中国大陆能用吗？</strong>

能用。飞汇有中文网站和中文付款入口，付款来源国选中国，就能用人民币转账、支付宝、微信支付或银联卡付款。我们没有找到它在大陆被封锁的权威记录。首页里的 Vimeo 视频加载不出来，不影响付款。

<strong>用 Flywire 付学费要开 VPN 吗？</strong>

不需要，也不建议开。飞汇官方帮助中心在银联卡付款失败的说明里，明确建议关闭 VPN，避开学校或公司的 Wi-Fi，改用家里的宽带或手机流量。

<strong>为什么用支付宝付 Flywire 会显示 ILLEGAL_SIGN？</strong>

按飞汇官方说法，这是在微信或手机 App 的内置浏览器里打开了支付宝链接导致的。复制链接，换到电脑上的 Chrome 等标准浏览器重新操作即可。

<strong>给孩子交学费，爷爷奶奶或舅舅能用支付宝付吗？</strong>

爷爷奶奶可以，舅舅不行。按飞汇官方规定，支付宝和微信支付的付款人只能是学生本人、父母、祖父母、配偶或兄弟姐妹。其他亲属付款，可以用银联卡，银联卡不限制付款人和学生的关系。

<strong>Flywire 有中国客服电话吗？</strong>

飞汇官网的中文联系页只列出美国的免费电话。一些学校转载的飞汇中文资料里写有一个 400 开头的中国客服电话，但我们在飞汇官方帮助中心里没有找到这个号码。建议以付款页和账户里显示的联系方式为准。

---

延伸阅读：[国际学校和留学家长常用的境外系统：国内访问情况汇总](/guides/intl-school-systems-china-access) · [开途定价与退款规则](/pricing?utm_source=guide&utm_medium=content&utm_campaign=flywire-china-access)
