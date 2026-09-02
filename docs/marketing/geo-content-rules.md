# GEO Content Rules（Generative Engine Optimization — AI 搜索）

> 2026-09-02 自 `web/CLAUDE.md` "SEO & GEO Constitutional Rules" 迁出。技术 SEO 规则（`generateMetadata()`、JSON-LD 类型、sitemap/hreflang、`next/image`、标题层级）仍留在 `web/CLAUDE.md`，因为它们由 `web/tests/seo-completion.test.ts` 等测试锁住；本文是**内容写作**规则，由 `kaitu-content` skill 在写文章时执行。

面向 AI 搜索引擎（Google AI Overview、Perplexity、ChatGPT Search）优化：目标是让内容可被 LLM 直接抽取、引用。

## 规则

- **Citable facts over marketing**：技术内容用可直接引用的事实 + 数据。"k2cc maintains stable throughput under high packet loss" > "blazing fast speeds"。丢包率口径统一见 `README.md` 冲突 #3（"26% 丢包下 2-5× BBR"）。
- **FAQ with structured data**：功能页与 support 页必须有 FAQ 段落并打 `FAQPage` JSON-LD；问题用自然语言（"What is the difference between k2 and Clash?"）。
- **Comparison tables in semantic HTML**：协议对比表必须是 `<table>` + `<thead>`/`<tbody>`。AI 解析器抽取表格数据；对比数据**禁用** CSS grid。
- **E-E-A-T signals**：链接到 GitHub 仓库（`github.com/getoverleap` 是唯一允许的协议层 org）、changelog、技术文档；展示团队 / 组织信息。这些权威信号决定 AI 是否引用。
- **Long-tail query coverage**：为具体技术查询写独立内容页（"What is ECH encryption?"、"How does k2cc congestion control work?"）——这些是 AI 搜索会浮现的查询。
- **Schema.org SoftwareApplication**：install 页必须带 `SoftwareApplication` schema：`name`、`operatingSystem`、`downloadUrl`、`applicationCategory`。
- **Direct answers first**：内容页开头先给 1-2 句直接答案，再展开。AI 抽取的是第一句确定性陈述。

## 品牌口径

内容里的产品名取 `brand.displayName`：en/ja 面写 "Overleap"，zh 面写「开途」；协议层 "k2" / "k2cc" 全球共享。裸词 "Kaitu" 在 en/ja 消息文件里会被 `web/tests/messages-integrity.test.ts` 拒绝；隔离规则全文见 [`brand-naming-strategy.md`](./brand-naming-strategy.md)。
