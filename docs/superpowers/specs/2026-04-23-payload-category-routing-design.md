# Payload 内容类别路由设计（Phase 1：技术设计）

**Date**: 2026-04-23
**Author**: David (与 Claude 协作 brainstorm)
**Scope**: Phase 1 — 代码/schema/路由/测试。Phase 2（Velite → Payload 数据迁移）另立 spec。
**Status**: 设计完成待实现

---

## 背景

当前 web 站点有两套内容系统并存：

- **Payload CMS** — 服务 `/blog/[slug]`，走 Payload Posts collection；`category` 字段存在但未参与 URL
- **Velite** — 服务 `/guides/*`、`/k2/*`，从 `content/{locale}/{category}/{slug}.md` 构建

两套系统造成：
- 内容编辑者要学两套工作流（.md 文件 vs admin UI）
- `/guides` 不能用 Payload 的 i18n + autoTranslate + brand 可见性
- 营销同事无法自助创建新 category（如 /changelog、/security）

目标：**统一走 Payload，URL 结构改为 `/{category}/{slug}`，列表页 `/{category}/`**。

---

## 架构决策

### 1. Payload 单源（Velite 彻底移除）

选项 A（Payload 单源） vs B（Payload + Velite 双源并存） vs C（迁移脚本一次性）。

**选 A**。理由：
- 双源查找就是典型的 defensive migration bridge，违反项目已有反馈规范
- 营销团队不可能同时维护 .md 文件和 CMS 两套编辑流程

### 2. 扁平 category（不嵌套）

URL 形式：`/{category.slug}/{post.slug}`，**不**做 `/{parent}/{child}/{slug}`。

理由：
- 嵌套会让 post 改 category 时 URL 大面积变动，引发 301 地狱
- 现有 Velite slug 都是两段 `{category}/{slug}`，扁平正好 1:1 对齐
- `Categories.parent` 字段**移除**（不进 URL 则保留只会误导后续开发者）

### 3. Posts 仅承载 Article；Pages（/install、/terms 等）延后

本期只做 Posts。Pages 作为独立 collection 后续单独设计，不在本 spec 范围内。

### 4. 列表页轻量

`/{category}/` 只渲染：
- H1 = `category.name`
- Post 列表（按 `publishedAt desc`，limit 50 无分页）
- SEO meta 自动生成：`title = "{category.name} | {brand.displayName}"`，`description = category.description || brand 默认描述`

**不**引入 `intro` richText / `coverImage` / Category 级别的 `showOnKaitu/showOnOverleap` 字段。

### 5. 不加 reserved slug 守卫

Next.js 静态路由优先级天然胜过 `[...slug]` catch-all。若 admin 不慎创建与硬编码路由撞名的 category（如 slug=`account`），该 category 的 list 页不可达（静态 `/account` 赢），admin 会很快发现并改名。无需额外验证代码。

---

## 数据模型变更

### `Categories` 集合

```ts
// web/src/payload/collections/Categories.ts
export const Categories: CollectionConfig = {
  slug: 'categories',
  admin: { useAsTitle: 'name', defaultColumns: ['name', 'slug', 'updatedAt'] },
  access: { read: isAdmin, create: isAdmin, update: isAdmin, delete: isAdmin },
  fields: [
    slugField(),
    { name: 'name', type: 'text', required: true, localized: true },
    { name: 'description', type: 'textarea', localized: true },
    // REMOVED: parent (was relationship to categories)
  ],
}
```

**DB migration**：`web/src/migrations/{timestamp}-remove-categories-parent.ts` drop `parent_id` 列。

### `Posts` 集合

新增一条 validate：`status=published` 时 `category` 必填。

```ts
{
  name: 'category',
  type: 'relationship',
  relationTo: 'categories',
  custom: { translatorSkip: true },
  validate: (value, { siblingData }) => {
    if (siblingData?.status === 'published' && !value) {
      return 'Category is required when publishing'
    }
    return true
  },
},
```

其它字段零改动。

---

## 路由与渲染

### 统一入口 `[locale]/[...slug]/page.tsx`

```ts
// 伪代码
export default async function CatchAll({ params }) {
  const { locale, slug } = await params
  const brand = await getBrand()
  const payload = await getPayload({ config })
  const visibilityField = brand.id === 'kaitu' ? 'showOnKaitu' : 'showOnOverleap'

  if (slug.length === 1) {
    const category = await findCategory(payload, locale, slug[0])
    if (!category) return notFound()
    const posts = await findPublishedPosts(payload, locale, {
      category: category.id,
      [visibilityField]: true,
    })
    return <CategoryListPage category={category} posts={posts} />
  }

  if (slug.length === 2) {
    const [catSlug, postSlug] = slug
    const category = await findCategory(payload, locale, catSlug)
    if (!category) return notFound()
    const post = await findPost(payload, locale, { slug: postSlug, category: category.id })
    if (!post) return notFound()
    const visible = brand.id === 'kaitu' ? post.showOnKaitu : post.showOnOverleap
    if (!visible) return notFound()
    return <PostDetailPage post={post} />
  }

  return notFound()
}

export const dynamic = 'force-dynamic'
```

`PostDetailPage` / `CategoryListPage` 组件可复用现有 `/blog/[slug]/page.tsx` 的 RichText 渲染块和 metadata 逻辑。

### 404 语义

| 情形 | 行为 |
|------|------|
| Category slug 不存在 | `notFound()` |
| Category 存在，post slug 不存在 | `notFound()` |
| Post 存在，当前 brand 不可见 | `notFound()` |
| `slug.length >= 3` | `notFound()` |
| Post 在当前 locale 无翻译 | Payload `fallback: true` 自动回落 `zh-CN`（既有行为） |

### Next.js 静态路由共存

- `/k2/page.tsx`（硬编码产品落地页）仍赢 `/k2` —— 符合预期
- `/k2/{slug}` 落到 `[...slug]`，查 Payload `category=k2 + post.slug={slug}`
- 其它硬编码路由（`/install`、`/account`、`/login`、`/purchase`、`/g`、`/s`、`/survey`、`/changelog`、`/releases`、`/support`、`/retailer`、`/privacy`、`/terms`、`/opensource`、`/discovery`、`/403`）同理，`/{route}` 继续走硬编码，但 `/{route}/{slug}` 可能落到 Payload（若 admin 创建了同名 category）

### 删除的路由

- `web/src/app/[locale]/blog/page.tsx`
- `web/src/app/[locale]/blog/[slug]/page.tsx`

删除后 `/blog`、`/blog/{slug}` 统一由 `[...slug]` 处理。

---

## 迁移路径（阶段划分）

### Phase 1（本 spec）

- Schema 变更：`Categories` 去 `parent`，`Posts.category` 加 published required 校验
- 路由：重写 `[...slug]/page.tsx` 为 Payload-only
- 删除旧 `/blog/*` 两个 page.tsx
- 测试（下一节）
- **不动 Velite**：`velite.config.ts`、`content/`、所有 `#velite` imports 保留运行状态
- **不动 sitemap.ts**：Phase 2 移除 Velite 时一并改

### Phase 2（另立 spec）

- `scripts/migrate-velite-to-payload.ts` 迁移脚本：31 `.md` 文件按路径分组 → Payload Posts
- `autoTranslate` hook 在迁移期间通过 `context.skipAutoTranslate` 绕过
- 处理 `k2/index.md` 归属（该 URL 在新路由下无家）
- `coverImage` 图片需先走 Media S3 上传（迁移脚本暂不处理或数量少手动补）
- 删除 `velite.config.ts`、`content/`、`#velite` imports
- 更新 `sitemap.ts` 为 Payload 查询
- 生产部署前 smoke 清单

### 部署协调

**Phase 1 代码不单独合入 main**。流程：

1. Phase 1 PR → merge 到 feature 分支 `feat/payload-category-routing`
2. Phase 2 PR（迁移脚本 + Velite 清理）→ chain 到同一 feature 分支
3. Feature 分支整体 merge 到 `main`，一次性部署

**Phase 1 合并前前置操作**：在 Payload admin 给现存 blog posts 补 `category=blog` 字段（手动操作，因为 CMS 刚上线不久，现有 post 数量极少）。PR 描述里明确写出此前置操作。

---

## 测试策略

### 单元测试（vitest）

- `[...slug]` handler 段数分支：1 段 / 2 段 / ≥3 段
- category 不存在 → 404
- post 不存在 → 404
- brand 不可见 → 404
- Posts 的 `category required when published` validate：draft 允许空，published 必填

### 集成测试

新建 `web/src/app/[locale]/[...slug]/__tests__/page.test.tsx`，用 Payload Local API 创建 fixture：

```
Fixture:
- Category: blog
- Category: guides
- Post A (category=blog, slug=hello, showOnKaitu=true, showOnOverleap=false, status=published)
- Post B (category=blog, slug=draft, status=draft)
- Post C (category=guides, slug=setup, both brands, status=published)

Assertions:
- /blog/         → renders Post A (on Kaitu host); renders empty (on Overleap host)
- /blog/hello    → renders Post A detail (on Kaitu); 404 (on Overleap)
- /blog/draft    → 404
- /guides/       → renders Post C on both brands
- /guides/setup  → renders Post C detail on both brands
- /nonexistent/  → 404
- /blog/nope     → 404
```

### 手工冒烟（dev env）

1. 后台创建 category=`testcat`，创建 post 挂 testcat，发布
2. `/testcat/` 见 post 列表
3. `/testcat/{slug}/` 见详情
4. 删除 category → `/testcat/` 404
5. 新建 post 未选 category、status=published → 保存失败报 validate 错误
6. `BRAND_ID=kaitu yarn dev` 和 `BRAND_ID=overleap yarn dev` 两环境各跑一遍

---

## 文件变更清单

**修改**：
- `web/src/payload/collections/Categories.ts` — 去 `parent`
- `web/src/payload/collections/Posts.ts` — 加 `category` published required 校验
- `web/src/app/[locale]/[...slug]/page.tsx` — 重写为 Payload-only handler

**新增**：
- `web/src/app/[locale]/[...slug]/__tests__/page.test.tsx`
- `web/src/migrations/{timestamp}-remove-categories-parent.ts`

**删除**：
- `web/src/app/[locale]/blog/page.tsx`
- `web/src/app/[locale]/blog/[slug]/page.tsx`

**不触碰（Phase 2 处理）**：
- `web/velite.config.ts`
- `web/content/` 整个目录
- 所有 `#velite` imports
- `web/src/app/sitemap.ts`

---

## 已明确的非目标

- Pages（/install、/terms 等硬编码 evergreen 页）不进 Payload — 另立 spec
- Category 级别的品牌可见性字段不加
- Category `intro` / `coverImage` / `seoTitle` / `seoDescription` 字段不加
- 嵌套 category（`/parent/child/slug`）不做
- Category list 页的分页 / 搜索 / filter 不做
- RSS / Atom feed 不做
- Reserved slug 守卫不做（Next.js 路由优先级天然保护）

---

## 风险与缓解

| 风险 | 缓解 |
|------|------|
| Phase 1 单独部署 → /blog 现存 post 无 category → 404 | Phase 1 不单独合 main，chain Phase 2 一起发；合并前手动补现存 blog post 的 category 字段 |
| Payload 查询性能（每次 `/{category}/` 请求走 DB） | `force-dynamic` 与现有 `/blog/page.tsx` 相同策略，先观察；若需要可后续加 revalidate 或 ISR |
| autoTranslate 在 Phase 2 迁移脚本时触发 7 locale × 31 post 批量翻译 | Phase 2 spec 明确 `context.skipAutoTranslate` 绕过机制 |
| admin 创建 slug 撞硬编码路由 | 静态路由赢，list 页不可达，admin 自发现 |
