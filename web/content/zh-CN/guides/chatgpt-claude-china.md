---
title: "国内能用 ChatGPT 和 Claude 吗？注册、打不开和报错对照（2026）"
date: 2026-11-10
summary: "OpenAI 和 Anthropic 的支持地区都不含中国大陆、香港和澳门，台湾在列。本文按官方原文说明 ChatGPT 和 Claude 的注册要求与手机验证规则，列出常见报错的原文和含义，分清哪些是网络问题、哪些是地区政策，并给出国内可用的替代选择。"
tags: ["国内怎么用chatgpt", "国内怎么用claude", "chatgpt 打不开", "chatgpt 注册", "claude 注册"]
brand: kaitu
draft: false
section: kaitu
---

<strong>按官方政策，在中国大陆不能用。</strong>OpenAI（ChatGPT）和 Anthropic（Claude）公布的支持地区名单里，都没有中国大陆、香港和澳门，台湾在名单里。OpenAI 写明在名单以外访问可能导致账号被封禁或暂停；Anthropic 写明名单以外的地区一律不支持，人在不支持的地区时使用也不支持。很多人遇到的“打不开”，其实分成两种：一种是网络连不上，一种是平台按地区拒绝服务。前者是网络问题，后者是政策问题，处理方法完全不同，下面的报错对照表可以帮你分清。

<em>政策原文：OpenAI [ChatGPT supported countries](https://help.openai.com/en/articles/7947663) 与 [API supported countries](https://developers.openai.com/api/docs/supported-countries)、Anthropic [supported countries](https://www.anthropic.com/supported-countries)，2026 年 11 月 10 日查看，以官方最新页面为准。</em>

## ChatGPT 和 Claude 打不开：先看报错原文

| 你看到的 | 出现在哪 | 意思 | 换网络能不能解决 |
|---|---|---|---|
| 页面一直转圈、无法访问此网站 | 浏览器打开 chatgpt.com / claude.ai | 网络连不上 | 人在支持地区（出差、留学）时是网络问题；人在大陆时，网络通了也不在服务范围内 |
| 注册或登录时提示所在国家不受支持 | ChatGPT 网页、App | 平台按地区拒绝注册（OpenAI 帮助中心有[专门说明](https://help.openai.com/en/articles/8983035)） | 不应该靠换网络绕过 |
| `unsupported_country_region_territory`，“Country, region, or territory not supported” | 调用 OpenAI API 时返回 403 | 平台按请求来源判断你在不支持的地区 | 不应该靠换网络绕过，官方写明可能导致封号 |
| “Unfortunately, Claude is only available in certain regions right now.” | 打开 Claude 网页或 App | 同上，平台判断你所在地区不在支持名单 | 同上 |

判断方法：页面根本打不开，是网络层面的问题；页面打开了、明确告诉你地区不支持，是政策问题。

## ChatGPT 和 Claude 注册会卡在哪

- <strong>ChatGPT</strong>：OpenAI 帮助中心说明，注册和使用 ChatGPT 一般不需要手机验证，个别功能可能要求；验证不接受座机、VoIP、Google Voice 号码（[手机验证说明](https://help.openai.com/en/articles/8983040)）。在不支持的地区注册，会直接提示国家不受支持（[说明](https://help.openai.com/en/articles/8983035)）。
- <strong>Claude</strong>：Anthropic 帮助中心写明，所有新用户都必须用能收短信的手机号验证，不能跳过（[说明](https://support.claude.com/en/articles/8287232)，2026-11-10 查看）。

网上卖的“成品账号”“共享账号”不建议买。账号随时可能被平台收回，付过的订阅费和对话记录会一起没了。

## 哪些情况下可以正常用

- <strong>人在支持地区</strong>：比如在台湾、日本、美国出差或留学。两家都按你使用时所在的地区判断。
- <strong>公司用 API</strong>：Anthropic 额外规定，注册地或总部在不支持地区的公司，以及被这类主体控股或控制的公司，也不能使用。在海外注册的公司用之前，先确认股权结构是否符合。
- <strong>回国以后</strong>：人在大陆时，按政策就不在服务范围内。重要的对话和资料，出境前或在支持地区时导出备份，不要等回国再处理。

如果只是写作、翻译、总结、写代码这类常见需求，国内已经备案的大模型产品可以直接用，比如 DeepSeek、Kimi、通义千问、豆包，没有上面这些限制。

## 开途能解决什么，不能解决什么

开途是网络工具，解决的是第一种问题：境外网站连不上、连上了很卡。它在电脑上默认智能分流，国内网站直连，只有境外网站走开途的线路；拥塞控制 k2cc 针对跨境线路的高丢包设计（[原理](/k2/k2cc)），对 GitHub、Google 文档这类需要长时间稳定连接的网站更有用。

它解决不了第二种问题：开途不能改变 OpenAI、Anthropic 的地区政策，不提供账号，也不能保证你的账号不被限制。人在支持地区、但所在网络访问境外网站不稳定时，开途才派得上用场。电脑版在[下载页](/install)，价格和退款规则见[定价页](/pricing)。

## 常见问题

<strong>国内能用 ChatGPT 吗？</strong>

按 OpenAI 的政策不能。支持名单里没有中国大陆、香港和澳门，官方写明在名单以外访问可能导致账号被封禁或暂停。国内可以用 DeepSeek、Kimi、通义千问、豆包等已备案的产品。

<strong>ChatGPT 注册需要手机号吗？</strong>

一般不需要。OpenAI 说明注册和使用 ChatGPT 一般不需要手机验证，个别功能可能要求；验证不接受座机、VoIP、Google Voice 号码。

<strong>Claude 在国内能注册吗？</strong>

不能。Anthropic 的支持名单不含中国大陆、香港和澳门，所有新用户还必须通过手机短信验证。

<strong>ChatGPT 打不开，是网络问题还是被封了？</strong>

页面完全打不开或一直转圈，是网络问题；页面能打开，但提示地区不支持（API 返回 `unsupported_country_region_territory`），是地区政策问题，换网络不能解决。

<strong>台湾能用 ChatGPT 和 Claude 吗？</strong>

能。台湾在 OpenAI 和 Anthropic 的支持地区名单里。

延伸阅读：[k2cc 拥塞控制原理](/k2/k2cc) · [开途快速入门](/guides/getting-started)
