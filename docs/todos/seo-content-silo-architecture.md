# SEO: 内容集群 (Content Silo) 架构扩展

## 优先级: P2

## 目标
在 web 项目中建立 `/blog` 和 `/docs` 内容路由系统，承载竞品对比、技术白皮书、基准测试等 SEO 内容。

## 当前基础
- 已有 Velite 集成（`web/content/` 目录）
- 已有 `web/content/en-US/k2/index.md` 等内容文件
- Next.js 15 + MDX 渲染能力

## 需要建设的内容集群
1. `/docs/protocol` — 协议技术文档（白皮书、k2cc 规范）
2. `/blog` — 技术博客（竞品对比、使用教程、网络诊断指南）
3. `/benchmarks` — 性能基准测试
4. `/docs/comparisons` — 竞品对比专区

## 技术任务
- 扩展 Velite schema 支持 blog/docs 内容类型
- 建立 MDX 组件库（代码块、图表、对比表格）
- 添加文章列表页和分页
- 各内容类型的 generateMetadata + JSON-LD
- 内部链接网络（文章间交叉引用）
