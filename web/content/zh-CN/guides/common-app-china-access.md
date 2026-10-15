---
title: "Common App 官网登录在国内很慢、上传文件失败怎么办"
date: 2026-10-15
summary: "在国内申请美本，Common App 能打开吗？申请系统本身在大陆可以访问，登录只用邮箱和密码；最容易卡的是“从 Google Drive 选文件上传”。本文按官方系统要求和页面依赖分析，说明卡在哪里、上传失败先查什么，以及推荐信和学校材料怎么提交。"
tags: ["Common App", "Common App 登录", "美本申请", "留学", "国际学校"]
brand: kaitu
draft: true
---

<strong>Common App 的申请系统（apply.commonapp.org）在国内一般可以打开，登录只用邮箱和密码，没有 Google 登录或 reCAPTCHA 验证。</strong>最容易卡住的是上传文件时选“从 Google Drive 选取”，这一步要连接 Google 的账号、接口和云端硬盘服务，而它们在大陆受干扰或被封。把文件先存到电脑本地再上传，大多数问题就能绕开。

<em>依据：Common App 官方帮助中心（Applicant Solutions Center）、2026 年 10 月 15 日对公开登录页做的页面依赖分析和前端代码阅读（在大陆以外的网络中进行，没有登录）、GreatFire 的公开监测记录。我们没有在大陆网络里测试过，也没有找到中国申请者的公开反馈。资料核对日期：2026-10-15。</em>

这篇是[国际学校和留学家长常用的境外系统：国内访问情况汇总](/guides/intl-school-systems-china-access)中的一篇。

## 为什么在国内会卡

我们打开申请者登录页 apply.commonapp.org/login，记录到的主要外部请求如下（推荐人登录页 recommend.commonapp.org 基本相同）：

| 依赖 | 用来做什么 | 国内表现（来源） |
|---|---|---|
| apply.commonapp.org、api26.commonapp.org、forge-assets.commonapp.org | 申请系统本身、接口、字体图标 | apply.commonapp.org 可访问，但最近一次测试是 2026-04-20（[GreatFire](https://en.greatfire.org/https/apply.commonapp.org)）；commonapp.org 测了 6 个网址，全部可访问（[GreatFire](https://en.greatfire.org/commonapp.org)） |
| apis.google.com | 加载 Google API 客户端，用于“从 Google Drive 选文件” | <strong>不稳定，最近 2 次有效测试都受到干扰</strong>（[GreatFire](https://en.greatfire.org/https/apis.google.com)） |
| accounts.google.com、www.googleapis.com | 选 Drive 文件时的 Google 授权和文件下载 | <strong>都不稳定</strong>，最近的有效测试全部受到干扰（[GreatFire](https://en.greatfire.org/https/accounts.google.com)、[GreatFire](https://en.greatfire.org/https/www.googleapis.com)） |
| drive.google.com、docs.google.com | 文件本身所在的服务 | <strong>被封</strong>（[GreatFire](https://en.greatfire.org/https/drive.google.com)） |
| googletagmanager、google-analytics、cookieyes、logr-ingest | 访问统计、Cookie 弹窗、日志 | 不影响登录和填表 |

我们读了登录页的前端代码：apis.google.com 的脚本是给 <strong>Google Picker（从 Google Drive 选文件）</strong>用的，不是登录用的。上传时如果选了从 Drive 选取，浏览器要先授权 Google 账号，再从 Google 下载文件，这几步在国内都容易失败。直接从电脑本地选文件，就不走这条路。

另外，这两个 Google 脚本是在页面加载时一起引入的。如果 apis.google.com 一直连不上，也没有返回失败，<strong>可能</strong>会让登录页打开得慢一些。这是我们读代码后的推断，没有在大陆网络里验证过。

## 先试这些（不需要任何工具）

1. <strong>文件先存到电脑本地，再上传。</strong>文书、成绩单扫描件这类文件，不要用“从 Google Drive 选取”，先下载到电脑，再从本地上传。官方支持 PDF、Word（doc、docx）、RTF、纯文本等格式。
2. <strong>用官方支持的浏览器，并关闭弹窗拦截。</strong>官方系统要求是：Windows 11 用最新版 Chrome、Firefox 或 Edge；Mac（macOS 14 及以上）用 Chrome、Firefox 或 Safari 17 及以上；要开启 JavaScript 和 Cookies，<strong>必须关闭弹窗拦截</strong>（[官方系统要求](https://appsupport.commonapp.org/applicantsupport/s/article/What-are-the-System-Requirements-to-use-The-Common-Application)）。
3. <strong>把 Common App 的邮箱加进白名单。</strong>官方建议把 appsupport@commonapp.net 加进邮箱白名单，以免收不到通知（同上）。用国内邮箱注册的，特别要注意垃圾邮件箱。
4. <strong>上传失败时，先检查文件本身。</strong>PDF 不要加密，也不要设置打开密码。如果还是失败，可以用“打印为 PDF”重新导出一份再试。
5. <strong>推荐信收不到邀请，先查这几项。</strong>按官方说明，常见原因有：学生还没把推荐人分配给至少一所学校、邀请邮箱写错了、邮件进了垃圾箱或广告箱（[官方推荐人帮助](https://recsupport.commonapp.org/recommendersupport/s/article/Why-haven-t-I-received-a-recommendation-invitation-message)）。
6. <strong>问清楚学校老师用的是哪个平台。</strong>Common App 和 BridgeU、Cialfo、Naviance 等升学平台有合作。学校用了这类平台的话，老师和升学顾问会在平台里提交推荐信和学校报告，不用登录 Common App 的推荐人系统（[官方合作方列表](https://recsupport.commonapp.org/recommendersupport/s/article/Who-are-the-Common-App-recommendation-system-partners)）。这些平台在国内的访问情况，我们这次没有查。

## 还是不行时

如果你的文书、材料都存在 Google Drive 或 Google 文档里，必须从那里取，或者家里的网络打开 Common App 确实一直很慢，可以考虑用加速工具。开途是其中一个选择：默认“智能分流”，国内网站照常直连，只有访问境外网站才走境外线路。首次购买 7 天内、使用流量不超过 1GB 可以申请退款，详见[定价页](/pricing?utm_source=guide&utm_medium=content&utm_campaign=common-app-china-access)。安装方法见[快速入门指南](/guides/getting-started)。

<strong>提交申请和付申请费前，建议先在本地网络下确认页面显示正常</strong>，不要在截止日前最后一刻才操作。

## 常见问题

<strong>Common App 在国内能打开吗？</strong>

一般能打开。GreatFire 对 commonapp.org 测试的 6 个网址都可以访问，申请系统 apply.commonapp.org 也显示可访问，不过最近一次测试是 2026 年 4 月。登录页只用邮箱和密码，不依赖 Google 登录。

<strong>Common App 上传文件一直失败怎么办？</strong>

先确认你没有用“从 Google Drive 选取”，这一步要连 Google 的服务，在国内容易失败。把文件下载到电脑本地再上传。再检查 PDF 有没有加密，必要时用“打印为 PDF”重新导出。

<strong>Common App 官网和申请系统是同一个网址吗？</strong>

不是。官网是 www.commonapp.org，用来查学校和申请信息；填写和提交申请在 apply.commonapp.org；老师和升学顾问用的推荐人系统在 recommend.commonapp.org。

<strong>国内学校的老师怎么提交 Common App 推荐信？</strong>

学校如果用 BridgeU、Cialfo、Naviance 等合作平台，老师在平台里提交即可；没有用合作平台的，老师通过邀请邮件激活 Common App 推荐人账号，再提交。收不到邀请时，先查垃圾邮件箱，再确认学生已经把推荐人分配给至少一所学校。

<strong>Common App 有中文界面吗？</strong>

我们没有找到中文界面，也没有找到面向中国申请者的官方说明。Common App 的统计显示，2025–2026 申请季美国以外的第一年申请者中，中国排第一（[官方讲座资料](https://www.commonapp.org/files/webinar_decks/Counselor_Convo_International.pdf)）。

---

延伸阅读：[College Board 官网登录不进去怎么办](/guides/college-board-china-access) · [国际学校和留学家长常用的境外系统：国内访问情况汇总](/guides/intl-school-systems-china-access)
