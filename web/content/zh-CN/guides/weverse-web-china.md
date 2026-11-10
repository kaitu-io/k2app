---
title: "Weverse 网页版入口与官网：国内打不开怎么办（2026 实测）"
date: 2026-11-10
summary: "Weverse 官网和网页版是 weverse.io。在中国大陆直接打不开：我们 2026 年 11 月在广西电信网络实测，域名解析被污染，换成真实地址后连接也被中断。附官方入口、App 下载情况和几种办法对比。"
tags: ["weverse", "weverse网页版", "weverse官网", "weverse网页版入口", "weverse下载", "weverse电脑版"]
brand: kaitu
draft: false
section: overseas-sites
---

<strong>Weverse 的官方网址是 weverse.io，网页版也是这个地址。</strong>但在中国大陆的网络下，这个地址直接打不开：我们在 2026 年 11 月 10 日用广西电信的宽带实测，Weverse 的域名解析被污染；绕开 DNS、直接用它的真实服务器地址，连接在握手时就被中断了。所以打不开不是你的浏览器、电脑或账号出了问题，改 DNS、改 hosts 也解决不了，需要换一个能稳定访问境外网站的网络。

<em>本文实测时间：2026 年 11 月 10 日，网络为中国大陆广西南宁电信。App Store 情况同日查询。网络状况会变化，以你自己的网络为准。</em>

## Weverse 官网和网页版入口是哪个

| 用途 | 官方地址 | 说明 |
|---|---|---|
| 官网、网页版 | `https://weverse.io` | 电脑和手机浏览器都用这个地址 |
| 账号登录 | `account.weverse.io`、`accountapi.weverse.io` | 登录时会跳转到这里，不是单独的入口 |
| Weverse Shop（周边商店） | `shop.weverse.io` | 官方商店域名 |
| iPhone / iPad App | App Store 搜索 “Weverse”，开发者 WEVERSE COMPANY Inc. | 中国区 App Store 能搜到，见下文 |
| 安卓 App | Google Play | 国内打不开 Google Play |

<strong>搜索“Weverse 网页版入口”“Weverse 官网”时，留意别进错网站。</strong>搜索结果里有不少导航站、非官方镜像和“下载站”，它们不是 Weverse 官方。登录或输入密码之前，确认地址栏的域名是 `weverse.io` 结尾。

## 国内为什么打不开：实测结果

我们在大陆网络下测试了 Weverse 用到的域名。每个域名测两次：一次用国内 DNS 返回的地址，一次绕开 DNS、直接用我们在境外查到的真实地址。<strong>两种方式都没能连上</strong>，国内网站作为对照则正常：

| 测试的地址 | 国内 DNS 返回的地址 | 连接结果 | 直接用真实地址（跳过 DNS） |
|---|---|---|---|
| weverse.io | 108.160.161.20（不属于 Weverse） | 超时 | 18.172.202.122，连接被中断 |
| www.weverse.io | 108.160.167.167（不属于 Weverse） | 超时 | 18.172.202.44，连接被中断 |
| account.weverse.io（登录） | 108.160.165.62（不属于 Weverse） | 超时 | 54.230.41.112，连接被中断 |
| accountapi.weverse.io（登录接口） | 31.13.69.245（不属于 Weverse） | 无法连接 | 3.166.34.34，连接被中断 |
| shop.weverse.io（周边商店） | 199.59.148.206（不属于 Weverse） | 超时 | 18.239.134.81，连接被中断 |
| 对照：www.baidu.com | 正常地址 | 正常打开 | — |

从这张表能看出两层限制：

1. <strong>域名解析被污染</strong>：国内 DNS 给 Weverse 的域名返回的是和 Weverse 无关的地址，浏览器连向了错误的服务器。
2. <strong>真实地址也连不上</strong>：绕开 DNS，直接连真实服务器，TCP 能建立，但在加密握手阶段连接就被中断（约 1 到 2 秒，提示连接被对方提前关闭），页面根本加载不出来。

这就是为什么网上流传的<strong>“改 DNS”“改 hosts 文件”对 Weverse 无效</strong>：它们最多解决第一层，解决不了第二层。第三方监测网站 GreatFire 目前也把 weverse.io 记为被屏蔽（[记录](https://zh.greatfire.org/weverse.io)）。这不是临时故障，等一等也不会恢复。

## Weverse App 怎么下载

- <strong>iPhone / iPad</strong>：2026 年 11 月 10 日查询 App Store 中国区，<strong>能搜到 Weverse 官方应用</strong>（名称“Weverse-和艺人更靠近”，开发者 WEVERSE COMPANY Inc.），用中国区 Apple ID 就能下载，不需要换区。但下载后在国内网络下登录和加载内容会失败，原因和网页版一样：它要连的服务器连不上。
- <strong>安卓</strong>：官方渠道是 Google Play，而 Google Play 在国内同样打不开。网上流传的安装包来源无法核实，可能被改过，不建议安装。

也就是说，不管用网页版还是 App，卡住的都是同一件事：网络连不上 Weverse 的服务器。

## 几种常见办法对比

| 办法 | 能不能用 | 优点 | 缺点 |
|---|---|---|---|
| 改 DNS / 改 hosts | 不能 | 免费 | 实测真实地址也连不上，解决不了问题 |
| 第三方导航站、搬运站 | 只能看到零散内容 | 不用装软件 | 不是官方，登录要输入账号密码有盗号风险；内容不完整、经常失效 |
| 免费加速器 / 免费 VPN | 时好时坏 | 免费 | 速度和稳定性难保证，开发者和隐私政策往往查不到 |
| 机场（自行配置代理客户端） | 能 | 选择多 | 要自己配置客户端和节点；服务商质量参差不齐 |
| 开途 | 能 | 装好一键连接；智能分流，国内 App 照常直连；k2cc 拥塞控制针对跨境线路的高丢包设计 | 要付费，App 版 <strong>1 年 $49 起</strong>，没有免费试用；首次购买 7 天内、流量不超过 1GB 可申请退款 |

Weverse 上有大量图片、视频和直播内容，晚上高峰时段跨境线路丢包多，最容易出现“图片加载一半”“视频转圈”“直播卡顿”。开途底层的 [k2cc 拥塞控制](/k2/k2cc)是为这种高丢包线路设计的：它会区分线路干扰造成的丢包和真正的拥堵，不会一丢包就大幅降速。

开途电脑版、手机版都可以在[下载页](/install?utm_source=guide&utm_medium=content&utm_campaign=weverse-web-china)安装，第一次使用可以照着[快速入门指南](/guides/getting-started)操作。需要说明的是，任何工具都不能承诺永远不受影响；建议在自己家里和手机流量下各试几天，再决定买多长的套餐。

## 连上以后

开途默认“智能分流”：只有访问 Weverse 这类境外网站时才走境外线路，微信、淘宝、小红书等国内 App 照常直连，不会因为开着开途而变慢。一个账号支持 5 台设备同时在线，电脑和手机可以一起连。如果某个国内 App 还是受影响，电脑和安卓可以把它单独设为直连，方法见[分应用分流说明](/guides/app-bypass)。

## 常见问题

<strong>Weverse 官网入口是哪个？</strong>

官网和网页版都是 weverse.io，周边商店是 shop.weverse.io。其他标着“官网入口”“网页版入口”的导航站不一定是官方，不要在那里输入密码。

<strong>Weverse 在国内能直接打开吗？</strong>

不能。2026 年 11 月 10 日在中国大陆电信网络实测，Weverse 的网页、登录和商店域名都连不上：域名解析被污染，用真实地址直接连接也在握手时被中断。

<strong>改 DNS 或者改 hosts 能打开 Weverse 吗？</strong>

不能。改 DNS、改 hosts 只能让你拿到正确的服务器地址，但实测用正确地址连接同样被中断，问题不只在 DNS。

<strong>苹果手机能下载 Weverse 吗？</strong>

能下载。2026 年 11 月 10 日查询，App Store 中国区能搜到官方应用（开发者 WEVERSE COMPANY Inc.）。但下载后在国内网络下登录和加载内容会失败，需要能访问境外网站的网络。

<strong>Weverse 安卓版怎么下载？</strong>

官方渠道是 Google Play，但 Google Play 在国内也打不开。网上流传的安装包来源无法核实，不建议安装。

<strong>用开途上 Weverse，会影响微信、淘宝吗？</strong>

默认不会。开途的智能分流让国内 App 直连，只有 Weverse 这类境外网站走境外线路。

延伸阅读：[开途快速入门](/guides/getting-started) · [k2cc 拥塞控制原理](/k2/k2cc) · [分应用分流说明](/guides/app-bypass) · [定价与退款规则](/pricing)
