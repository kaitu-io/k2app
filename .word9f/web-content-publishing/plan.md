# Plan: Web Content Publishing

## Meta

| Field | Value |
|-------|-------|
| Feature | web-content-publishing |
| Spec | docs/features/web-content-publishing.md |
| Date | 2026-02-20 |
| Complexity | moderate |

## AC Mapping

| AC | Test | Task |
|----|------|------|
| AC1: md 文件 → URL 可访问 | `test_content_page_renders` (E2E) | F1 + T2 |
| AC2: frontmatter 缺字段构建报错 | `test_invalid_frontmatter_build_fails` (build) | F1 |
| AC3: draft:true 不生成页面 | `test_draft_not_in_list`, `test_draft_page_404` (E2E) | T2 |
| AC4: 多语言回退 zh-CN | `test_locale_fallback_to_zhcn` (E2E) | T2 |
| AC5: 目录列表页按日期倒序 | `test_directory_listing_sorted` (E2E) | T2 |
| AC6: SEO metadata 自动生成 | `test_page_has_meta_tags` (E2E) | T2 |
| AC7: sitemap 包含内容页 | `test_sitemap_includes_content` (vitest) | T2 |
| AC8: 新目录无需改代码 | `test_nested_directory_page` (E2E) | T2 |
| AC9: 静态路由不受影响 | `test_static_routes_still_work` (E2E) | T2 |
| AC10: TypeScript 类型可用 | `tsc --noEmit` (build) | F1 |
| AC11: publish-content skill | 文件存在 + 格式验证 | T3 |

## Foundation Tasks

### F1: Velite 基础设施

**Scope**: 安装 Velite，配置 schema，集成到 Next.js 构建管线，创建种子内容，验证构建通过。

**Files**:
- `web/package.json` — 添加 velite, @tailwindcss/typography
- `web/velite.config.ts` — 新建：defineConfig + Zod schema（posts collection）
- `web/next.config.ts` — 修改：添加 Velite 启动代码（process.argv 模式）
- `web/tsconfig.json` — 修改：添加 `"#velite": ["./.velite"]` 路径
- `web/.gitignore` — 修改：添加 `.velite/`
- `web/content/zh-CN/blog/hello-world.md` — 新建：种子博客文章
- `web/content/zh-CN/guides/getting-started.md` — 新建：种子教程（验证多目录）
- `web/content/en-US/blog/hello-world.md` — 新建：英文版种子（验证多语言）

**Depends on**: none

**TDD**:
- RED:
  - `test_velite_build_succeeds`: 运行 velite build，验证 `.velite/` 目录生成且包含 index.ts
  - `test_velite_types_importable`: TypeScript 可以 `import { posts } from '#velite'`，`tsc --noEmit` 通过
  - `test_invalid_frontmatter_build_fails`: 创建一个缺少 `title` 的 md 文件，velite build 应报错
- GREEN:
  - 安装 velite: `cd web && yarn add velite @tailwindcss/typography`
  - 创建 `velite.config.ts`:
    - `defineCollection` 名为 `Post`，pattern `'**/*.md'`，root `'content'`
    - Zod schema: `title` (required string), `date` (required isodate), `summary` (optional string), `tags` (optional string[]), `coverImage` (optional string), `draft` (optional boolean, default false)
    - Transform: 从文件路径提取 `locale` 和 `slug`（剥离 locale 前缀 + `.md` 后缀）
  - 修改 `next.config.ts`: 添加 Velite process.argv 启动逻辑（Turbopack 兼容）
  - 修改 `tsconfig.json`: paths 添加 `"#velite": ["./.velite"]`
  - 修改 `.gitignore`: 添加 `.velite/`
  - 创建种子内容文件（3 个 markdown 文件）
  - 验证 `yarn build` 通过
- REFACTOR:
  - [SHOULD] 优化 velite.config.ts 中的 slug 提取逻辑可读性

**Acceptance**:
- `yarn build` 在包含种子内容的情况下成功
- `.velite/` 目录生成，包含类型安全的 posts 数据
- 故意创建格式错误的 md 文件时构建失败

**Knowledge**: docs/knowledge/task-splitting.md — "Simple Features: Sequential Tasks"

---

## Feature Tasks

### T2: 内容页面渲染 + SEO + Sitemap

**Scope**: 创建 catch-all `[...slug]` 页面，实现文章详情渲染、目录列表页、多语言回退、SEO metadata 生成、sitemap 扩展。

**Files**:
- `web/src/app/[locale]/[...slug]/page.tsx` — 新建：catch-all 内容页
- `web/src/app/sitemap.ts` — 修改：扩展加入内容页面

**Depends on**: [F1]

**TDD**:
- RED: (Playwright E2E + vitest)
  - `test_content_page_renders`: 访问 `/zh-CN/blog/hello-world`，页面包含文章标题和正文 HTML
  - `test_directory_listing_sorted`: 访问 `/zh-CN/blog`，显示所有已发布文章列表，按日期倒序
  - `test_nested_directory_page`: 访问 `/zh-CN/guides`，显示 guides 目录下的文章列表
  - `test_draft_page_404`: 创建 `draft: true` 的文章，访问其 URL 返回 404
  - `test_draft_not_in_list`: 列表页不显示 draft 文章
  - `test_locale_fallback_to_zhcn`: 访问 `/en-US/guides/getting-started`（无英文版），显示 zh-CN 版本内容
  - `test_page_has_meta_tags`: 文章页面 HTML 包含 `<title>`、`<meta name="description">`、`<meta property="og:title">`
  - `test_static_routes_still_work`: 访问 `/zh-CN/install`、`/zh-CN/purchase` 正常返回，不被 catch-all 拦截
  - `test_nonexistent_page_404`: 访问 `/zh-CN/nonexistent/path` 返回 404
  - `test_sitemap_includes_content`: sitemap 输出包含内容页面 URL
- GREEN:
  - 创建 `[...slug]/page.tsx`:
    - `generateStaticParams()`: 从 `#velite` 导入 posts，为每个 post 生成 `{ slug: [...] }` 参数（按 locale 分组）
    - `generateMetadata()`: 从 post frontmatter 生成 Next.js Metadata（title、description、openGraph）
    - 页面组件:
      1. 接收 `params.slug` 数组，拼接为路径
      2. 从 posts 中查找匹配 locale + slug 的文章
      3. 找到 → 渲染文章（Header + `prose` class markdown + Footer）
      4. 未找到 → 检查该路径是否是某些文章的目录前缀
         - 是 → 渲染列表页（标题、日期、摘要，按日期倒序）
         - 否 → 检查 zh-CN 回退
           - 有 → 渲染 zh-CN 版本
           - 无 → notFound()
  - 修改 `sitemap.ts`:
    - 导入 `posts from '#velite'`
    - 过滤 `draft !== true`
    - 为每篇文章每个语言版本生成 sitemap entry
- REFACTOR:
  - [MUST] 提取 `findPost(locale, slug)` 和 `findPostsInDirectory(locale, prefix)` 工具函数到页面文件顶部，T3 skill 文档需要引用这些约定
  - [SHOULD] 提取列表页和详情页为独立组件（ContentArticle / ContentListing）

**Acceptance**:
- 任意 `content/{locale}/` 下的 markdown 文件可通过对应 URL 访问
- 目录路径自动渲染列表页
- 多语言回退到 zh-CN
- SEO tags 自动生成
- sitemap 包含所有已发布内容
- 现有静态路由不受影响

---

### T3: Publish Content Skill

**Scope**: 创建 `/publish-content` Claude Code skill，标准化 AI 内容发布工作流。

**Files**:
- `.claude/skills/publish-content/SKILL.md` — 新建

**Depends on**: none（纯文档，不依赖代码实现。但逻辑上应在 F1+T2 完成后编写，以确保约定正确）

**TDD**:
- RED:
  - `test_skill_file_exists`: 验证 `.claude/skills/publish-content/SKILL.md` 文件存在
  - `test_skill_frontmatter_valid`: 文件包含有效的 YAML frontmatter（name、description 字段）
  - `test_skill_references_schema`: 文件内容包含完整的 frontmatter schema 说明
- GREEN:
  - 编写 SKILL.md，包含:
    - Frontmatter: `name: publish-content`, description 说明触发条件
    - 完整工作流（6 步）: 确认主题 → 确认语言 → 生成 markdown → 可选翻译 → 构建验证 → commit/push
    - 内置知识:
      - Frontmatter schema（完整字段说明，必填/可选标注）
      - 目录结构约定（`web/content/{locale}/...`）
      - 图片路径约定（`web/public/images/content/`）
      - 支持的 7 种语言
      - 保留路径列表（从现有静态路由提取：403, account, discovery, install, login, opensource, privacy, purchase, retailer, routers, s, terms, changelog, manager）
    - 示例：创建一篇博客文章的完整命令序列
    - 示例：创建多语言版本的工作流
- REFACTOR:
  - [SHOULD] 精简示例，确保 skill 文件不超过 150 行

**Acceptance**:
- Skill 文件格式正确，可被 Claude Code 识别
- 包含完整的 frontmatter schema 和路径约定
- 包含保留路径列表防止内容占用静态路由

---

## Execution Summary

```
F1 (Velite 基础设施) ──→ T2 (内容页面 + SEO + Sitemap)
T3 (Skill 文件)       ──→ (独立，建议 F1+T2 后编写)
```

**推荐执行顺序**: F1 → T2 → T3（线性，无并行收益——参考 task-splitting 知识"Simple Features"经验）

**单分支执行**: 依赖链为直线，建议在单分支上顺序执行，不使用 worktree。

**关键风险**:
- Velite 与 Next.js 15 + Turbopack 的兼容性（TD-7 中的 process.argv 启动模式已在官方示例验证）
- catch-all `[...slug]` 与现有静态路由的优先级（Next.js 保证静态路由优先，但需 E2E 验证）
- `@tailwindcss/typography` 与 Tailwind CSS 4 的兼容性（需确认 v4 版本的 prose class 可用性）
