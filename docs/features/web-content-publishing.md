# Feature: Web Content Publishing（AI 内容发布系统）

## Meta

| Field | Value |
|-------|-------|
| Feature | web-content-publishing |
| Version | v1 |
| Status | implemented |
| Created | 2026-02-20 |
| Updated | 2026-02-20 |

## Version History

| Version | Date | Summary |
|---------|------|---------|
| v1 | 2026-02-20 | 初始：Payload CMS 删除后的 AI 友好内容发布系统设计 |

## 概述

Payload CMS 删除后，web/ 项目需要一个轻量级内容发布方案。核心设计理念：**AI（Claude Code）就是编辑器**。内容以 Markdown 文件存储在 Git 仓库中，通过 Velite 构建层提供类型安全的数据访问，Next.js SSG 生成静态页面。

**路由能力**：内容目录结构 = URL 结构，支持任意深度路径嵌套，控制力仅次于 Next.js 原生 router。现有静态路由（install、purchase 等）优先级高于内容 catch-all 路由。

工作流：AI 写 Markdown → Git commit/push → Amplify 自动构建部署

## Product Requirements

### P0 — 核心内容系统

1. **Markdown 内容源**: 在 `web/content/{locale}/` 目录下存储 Markdown 文件，YAML frontmatter 定义元数据 (v1)
2. **Velite 构建层**: Zod schema 定义内容结构，构建时验证所有内容文件 (v1)
3. **任意路径路由**: 内容目录结构直接映射 URL，支持任意深度 (v1)
   - `content/zh-CN/blog/hello.md` → `/zh-CN/blog/hello`
   - `content/zh-CN/guides/setup.md` → `/zh-CN/guides/setup`
   - `content/zh-CN/news/2026/big.md` → `/zh-CN/news/2026/big`
   - `content/zh-CN/about.md` → `/zh-CN/about`
4. **多语言内容**: 按语言目录组织，同文件路径 = 同一篇文章的不同语言版本。某语言无对应文件时回退到 zh-CN (v1)
5. **SSG 静态生成**: `generateStaticParams` 在构建时生成所有内容页面 (v1)
6. **目录列表页**: 任何包含内容文件的目录自动获得列表页，按日期倒序 (v1)
   - `/zh-CN/blog` — 列出 `content/zh-CN/blog/` 下所有文章
   - `/zh-CN/guides` — 列出 `content/zh-CN/guides/` 下所有文章
   - `/zh-CN/news/2026` — 列出该子目录下所有文章

### P1 — SEO 与元数据

7. **自动 SEO**: Frontmatter → Next.js Metadata (title, description, Open Graph) (v1)
8. **Sitemap 集成**: 内容页面自动加入 `sitemap.ts` (v1)

### P2 — 未来扩展（不在本次范围）

- ~~Changelog 迁移~~ — 保持现有 `releases/` 不变
- ~~Discovery 外部化~~ — 保持硬编码不变
- ~~RSS Feed~~ — 后续按需

## Technical Decisions

### TD-1: 内容数据层 — Velite

**选择**: Velite (https://velite.js.org/)

**理由**:
- Contentlayer 已废弃，Velite 是精神继承者
- Zod schema 构建时验证 frontmatter
- 生成类型安全的 TypeScript 数据（`import { posts } from '#velite'`）
- 零运行时依赖
- Turbopack 兼容（`process.argv` 启动模式，非 webpack 插件）

### TD-2: 路由架构 — Catch-all `[...slug]`

使用单个 catch-all 路由 `[locale]/[...slug]/page.tsx` 处理所有内容页面。

**路由优先级**（Next.js 天然保证）：
1. 现有静态路由（`install/page.tsx`、`purchase/page.tsx` 等）— 最高优先级
2. Catch-all `[...slug]/page.tsx` — 兜底，服务 Velite 内容

**slug 匹配逻辑**：
```
URL: /zh-CN/blog/hello
slug: ['blog', 'hello']
→ 查找 content/zh-CN/blog/hello.md
→ 找到 → 渲染文章
→ 未找到 → 检查是否是目录（content/zh-CN/blog/ 下有文件）
  → 是目录 → 渲染列表页
  → 否 → 404（或回退 zh-CN）
```

**不需要为每个内容分类创建单独的路由文件**。一个 `[...slug]` 统一处理。

### TD-3: 内容目录结构

```
web/content/
├── zh-CN/
│   ├── blog/
│   │   ├── getting-started.md
│   │   └── new-feature.md
│   ├── guides/
│   │   ├── setup.md
│   │   └── advanced.md
│   ├── news/
│   │   └── 2026/
│   │       └── announcement.md
│   └── about.md
├── en-US/                        # 按需
│   └── blog/
│       └── getting-started.md
└── ja/                           # 按需
```

**多语言规则**:
- 同路径文件 = 同一篇文章的不同语言版本
- 某语言无对应文件 → 回退显示 zh-CN 版本
- AI 操作者决定是否创建多语言版本

### TD-4: Frontmatter Schema

```yaml
---
title: "文章标题"                   # 必填
date: 2026-02-20                    # 必填
summary: "一句话摘要"               # 可选，列表页和 SEO description
tags: ["vpn", "update"]             # 可选
coverImage: "/images/content/x.jpg" # 可选，OG image
draft: false                        # 可选，true 则不生成页面
---
```

### TD-5: 图片处理

- 存放: `web/public/images/content/`
- 引用: `![描述](/images/content/photo.jpg)`
- Next.js 从 `public/` 直接 serve

### TD-6: 渲染策略 — SSG

- 构建时静态生成所有内容页面
- 发布 = git push → Amplify 自动重新构建
- 不使用 ISR

### TD-7: Velite 集成（基于官方 Next.js 示例）

**启动方式**（Turbopack 兼容）:
```typescript
// next.config.ts
const isDev = process.argv.indexOf('dev') !== -1
const isBuild = process.argv.indexOf('build') !== -1
if (!process.env.VELITE_STARTED && (isDev || isBuild)) {
  process.env.VELITE_STARTED = '1'
  import('velite').then(m => m.build({ watch: isDev, clean: !isDev }))
}
```

**导入方式**: `import { posts } from '#velite'`（tsconfig paths 别名 `#velite` → `.velite`）

**输出**: `.velite/` 目录（gitignore），包含类型安全的 index.ts

### TD-8: AI 发布工作流

```
AI 写 Markdown 文件（任意目录结构）+ 图片
    ↓
git commit → git push
    ↓
Amplify 构建 (Velite Zod 校验 → Next.js SSG)
    ↓
内容上线（新路径自动可用）
```

**AI 创建新内容分类只需创建新目录**：
```bash
# 创建一个全新的 "tutorials" 分类，无需改任何代码
mkdir -p web/content/zh-CN/tutorials
# 写文章
echo '---\ntitle: "入门教程"\ndate: 2026-02-20\n---\n\n内容...' > web/content/zh-CN/tutorials/intro.md
# 自动获得 /zh-CN/tutorials/intro 详情页 + /zh-CN/tutorials 列表页
```

### TD-9: 与现有系统的集成

- 复用 `[locale]` 路由组和 Header/Footer
- 使用 `@tailwindcss/typography` 的 `prose` class 渲染 Markdown
- 扩展 `sitemap.ts` 加入内容页面

### TD-10: `/publish-content` Skill

创建 Claude Code skill，标准化 AI 内容发布流程。

**位置**: `.claude/skills/publish-content/SKILL.md`

**触发**: 用户说"写一篇文章"、"发布内容"、"publish content"等。

**工作流**:
1. 确认内容主题和目标路径（如 `blog/new-feature`）
2. 确认目标语言（默认 zh-CN，可选多语言）
3. 生成 Markdown 文件（自动填充正确的 frontmatter schema）
4. 如需多语言 → 翻译并创建其他语言版本
5. 本地构建验证（`cd web && yarn build`）
6. 提示用户是否 commit + push 发布

**Skill 内置知识**:
- Frontmatter schema（必填字段、可选字段、格式）
- 目录结构约定（`web/content/{locale}/...`）
- 图片存放路径（`web/public/images/content/`）
- 支持的语言列表（zh-CN, en-US, ja, zh-TW, zh-HK, en-GB, en-AU）
- 现有静态路由保留路径（不允许内容占用的路径）

### TD-11: 不做的事

- 不做可视化编辑器
- 不做 AI 自动翻译（翻译由 skill 引导人工确认）
- 不做 MDX（纯 Markdown）
- 不做评论系统
- 不迁移 changelog 和 discovery

## Acceptance Criteria

- AC1: `web/content/zh-CN/` 下任意目录创建 `.md` 文件，`yarn build` 成功后可在对应 URL 访问 (v1)
- AC2: Frontmatter 缺少 `title` 或 `date` 时，`yarn build` 报错 (v1)
- AC3: `draft: true` 的文件不生成页面，也不出现在列表页 (v1)
- AC4: 多语言回退：`/en-US/blog/hello` 无英文版本时显示 zh-CN 版本 (v1)
- AC5: 目录列表页：`/zh-CN/blog` 按日期倒序显示该目录下所有已发布文章 (v1)
- AC6: 内容页面自动生成 `<title>`、`<meta description>`、Open Graph tags (v1)
- AC7: 内容页面出现在 `sitemap.xml` 中 (v1)
- AC8: 新建目录 + markdown 文件后，无需改代码即可获得详情页和列表页 (v1)
- AC9: 现有静态路由（/install、/purchase 等）不受影响 (v1)
- AC10: TypeScript 可以 import Velite 数据，有类型提示 (v1)
- AC11: `/publish-content` skill 可用，引导 AI 完成从创建到发布的完整流程 (v1)

## Testing Strategy

- **构建验证**: `yarn build` 成功即验证内容格式和渲染正确性 (v1)
- **E2E 测试**: Playwright 测试内容详情页、列表页、多语言回退 (v1)

## Deployment & CI/CD

- **本地开发**: `cd web && yarn dev`，Velite watch mode + Turbopack HMR (v1)
- **构建部署**: Velite → Next.js SSG → Amplify (v1)
- **CI**: 现有 `yarn build` 步骤自动覆盖内容验证 (v1)

## 文件变更清单

### 新增文件

| 文件 | 说明 |
|------|------|
| `web/velite.config.ts` | Velite 配置 + Zod schema |
| `web/content/zh-CN/blog/hello-world.md` | 种子博客文章 |
| `web/content/zh-CN/guides/getting-started.md` | 种子教程（验证多目录路由） |
| `web/src/app/[locale]/[...slug]/page.tsx` | Catch-all 内容页（详情 + 列表双模式） |
| `.claude/skills/publish-content/SKILL.md` | AI 内容发布 skill |

### 修改文件

| 文件 | 说明 |
|------|------|
| `web/package.json` | 添加 velite + @tailwindcss/typography |
| `web/next.config.ts` | Velite 启动集成 |
| `web/tsconfig.json` | `#velite` 路径映射 |
| `web/src/app/sitemap.ts` | 加入内容页面 |
| `web/.gitignore` | 忽略 `.velite/` |
