---
purpose: Spec for integrating Payload CMS v3 into web/ for articles/categories/tags with AI auto-translation
date: 2026-04-20
status: verified — ready for plan
verified_against:
  - Payload v3.83.0 source (latest 2026-04-15)
  - @payload-enchants/translator v1.3.0 source
  - Payload blank template `src/app/(payload)/` layout
---

# Payload CMS 集成到 web/

## 目标
在现有 `web/` (Next.js 15 + React 19) 里嵌入 Payload v3.83，用于新增博客/文章类内容管理。3 个 collections（分类/文章/标签），由 `/manager` 管理员身份直接登录（无需二次注册），所有翻译由 AI 自动完成。

## 非目标
- 不迁移 Velite 现有 `/k2/` 文档
- 不替换 `/manager` 现有 Center API 管理界面
- 不对外暴露 Payload REST 或 GraphQL API

---

## 已验证决策

### D1：数据库 = PostgreSQL
`@payloadcms/db-postgres`。本地 docker-compose；生产 URL 待用户提供。

### D2：路由挂载（关键修正）
**不在 `/manager/cms` 下**，避开现有 `(manager)` 路由组冲突风险。
- Admin UI：**`/cms`**（顶级路径）
- REST API：**`/payload/api`** — 避开现有 `next.config.ts` 的 `/api/:path*` rewrite
- GraphQL：**禁用**（删 `(payload)/payload/api/graphql` 和 `graphql-playground` 子目录）

配置：
```ts
buildConfig({
  routes: { admin: '/cms', api: '/payload/api' },
})
```

**Payload 规则验证**（`packages/payload/src/config/types.ts:1442-1468`）：`config.routes.admin` 与 `config.routes.api` 存在；folder 名字必须匹配 URL 路径（文件型路由本质）。

### D3：认证 = Custom Auth Strategy 桥接 Center API
读 `access_token` HttpOnly cookie → 调 Center `/api/auth/me` → 检查 `roles` 含 admin → 返回虚拟 Payload User。用户 collection `admins`，`auth.disableLocalStrategy: true`。

**验证签名**（`packages/payload/src/auth/types.ts`）：
```ts
type AuthStrategy = { name: string, authenticate: AuthStrategyFunction }
type AuthStrategyFunctionArgs = { canSetHeaders?, headers, isGraphQL?, payload, strategyName? }
type AuthStrategyResult = { responseHeaders?: Headers, user: ({ _strategy?, collection? } & TypedUser) | null }
```

### D4：公开 API 关闭
- **GraphQL 不创建对应目录** —— Payload v3 按 `(payload)/api/graphql` 和 `graphql-playground` 目录存在与否决定 GraphQL 是否挂载，无需配置 key
- 所有内容 collections `access: { read: () => false }`（admins 限制为 `req.user` 存在）
- 前端 Server Component 用 `getPayload().find()` Local API 消费

### D5：翻译 = 官方 `openAIResolver` + OpenRouter baseUrl（关键修正）

**重大简化**：读源码（`packages/translator/src/resolvers/openAI.ts`）发现内置 `openAIResolver` 已支持 `baseUrl` 参数（README 失时），可直接指向 OpenRouter。**无需写自定义 resolver**。

```ts
openAIResolver({
  apiKey: process.env.TRANSLATOR_API_KEY!,
  baseUrl: 'https://openrouter.ai/api',  // 源码 fetch(`${baseUrl}/v1/chat/completions`)
  model: process.env.TRANSLATOR_MODEL ?? 'google/gemini-2.5-flash',
  prompt: customPrompt,  // 覆写默认 prompt 提升质量 + 强制 JSON 输出
})
```

**自定义 prompt** 的必要性：默认 prompt 只要求返回 JSON 数组，Gemini 可能返回 markdown 代码块包裹的 JSON。源码 `JSON.parse(content)` 会炸。我们在 prompt 里强制：
- 不输出 markdown 代码块
- 只输出一个纯 JSON 数组
- locale 描述：告诉 LLM zh-TW / zh-HK 用哪种词汇
- 保留品牌词：Kaitu / 开途 / k2 / k2cc

**Lexical 处理验证**（`packages/translator/src/translate/traverseRichText.ts`）：插件遍历 AST，只替换 text node 的 `.text` 字段，**结构天然保留**。无需 Markdown 往返。

**默认模型选择**：`google/gemini-2.5-flash` via OpenRouter
- 价格：$0.30/M input + $2.50/M output（更新自 OpenRouter 2026 定价）
- 典型 2000 字文章翻译到 6 locale ≈ $0.045/次
- 2025 WMT25 机器翻译竞赛胜者
- 备选 `anthropic/claude-sonnet-4.6`（env 切换，无代码改动）

### D6：源语言 = `zh-CN` → 6 目标 locale
`en-US`、`en-GB`、`en-AU`、`zh-TW`、`zh-HK`、`ja`。对齐 next-intl defaultLocale。

### D7：Lexical 空字段 bug #14372 已修
**已 closed 2025-10-29**（GitHub API 验证）。Payload v3.83.0 含修复。无需 `isEmptyLexical` guard。

### D8：Turbopack 兼容
Payload v3.83 + Next.js 15.4 Turbopack dev 模式 OK（Payload v3.73+ 起支持）。生产 build 继续用默认（未强制 turbopack）。

### D9：Middleware 放行清单
现有 `web/src/middleware.ts:35` 早返回 `/admin` / `/manager` 前缀。需加 **`/cms`** 和 **`/payload`**。
matcher 第 139 行 `['/', '/(zh-CN|...)/:path*', '/((?!api|app|_next|_vercel|.*\\..*).*)']` —— `api`/`app` 已排除，需加 `cms`/`payload`。

---

## Collections Schema

### `admins`（认证 collection，不存密码，strategy-only）
```ts
{
  slug: 'admins',
  auth: {
    disableLocalStrategy: true,
    strategies: [centerAuthStrategy],
  },
  access: { read: () => true, create: () => false, update: () => false, delete: () => false },
  fields: [
    { name: 'email', type: 'text' },
    { name: 'centerId', type: 'text', index: true },
  ],
}
```

### `categories`
```ts
{
  slug: 'categories',
  admin: { useAsTitle: 'name' },
  access: { read: () => false, create: isAdmin, update: isAdmin, delete: isAdmin },
  fields: [
    slugField(),  // helper: text, required, unique, index, translatorSkip
    { name: 'name', type: 'text', required: true, localized: true },
    { name: 'description', type: 'textarea', localized: true },
    { name: 'parent', type: 'relationship', relationTo: 'categories' },
  ],
}
```

### `tags`
```ts
{
  slug: 'tags',
  admin: { useAsTitle: 'name' },
  access: { read: () => false, create: isAdmin, update: isAdmin, delete: isAdmin },
  fields: [
    slugField(),
    { name: 'name', type: 'text', required: true, localized: true },
  ],
}
```

### `posts`
```ts
{
  slug: 'posts',
  admin: { useAsTitle: 'title', defaultColumns: ['title', 'status', 'publishedAt'] },
  access: { read: () => false, create: isAdmin, update: isAdmin, delete: isAdmin },
  versions: { drafts: true },
  fields: [
    slugField(),
    { name: 'title', type: 'text', required: true, localized: true },
    { name: 'excerpt', type: 'textarea', localized: true },
    { name: 'content', type: 'richText', editor: lexicalEditor(), required: true, localized: true },
    { name: 'coverImage', type: 'upload', relationTo: 'media',
      custom: { translatorSkip: true } },
    { name: 'category', type: 'relationship', relationTo: 'categories',
      custom: { translatorSkip: true } },
    { name: 'tags', type: 'relationship', relationTo: 'tags', hasMany: true,
      custom: { translatorSkip: true } },
    { name: 'status', type: 'select', options: ['draft', 'published'],
      defaultValue: 'draft', custom: { translatorSkip: true } },
    { name: 'publishedAt', type: 'date', custom: { translatorSkip: true } },
    { name: 'author', type: 'relationship', relationTo: 'admins',
      custom: { translatorSkip: true } },
  ],
  hooks: {
    beforeChange: [setAuthorFromRequest, setPublishedAt],
    afterChange: [autoTranslate],
  },
}
```

### `media`
```ts
{
  slug: 'media',
  upload: { staticDir: path.resolve(dirname, '../../public/cms-media') },
  access: { read: () => true, create: isAdmin, update: isAdmin, delete: isAdmin },
  fields: [
    { name: 'alt', type: 'text', localized: true },
  ],
}
```

> 本地 disk 存储；生产可切换 `@payloadcms/storage-s3` 但非本 spec 范围。

---

## 环境变量

```bash
# 数据库
DATABASE_URL=postgres://payload:payload@localhost:5432/kaitu_cms

# Payload
PAYLOAD_SECRET=<32 字节 hex>

# 翻译
TRANSLATOR_BASE_URL=https://openrouter.ai/api
TRANSLATOR_API_KEY=sk-or-v1-...
TRANSLATOR_MODEL=google/gemini-2.5-flash

# 认证桥接
CENTER_API_URL=https://k2.52j.me
```

---

## 风险与缓解

| 风险 | 影响 | 缓解 |
|---|---|---|
| Gemini 返回 markdown-包裹 JSON 导致 `JSON.parse` 失败 | 单次翻译整批失败（success=false） | 自定义 prompt 明确禁止代码块；`copyResolver` 作第二个 resolver 作降级入口 |
| 翻译 API 超时 | 翻译丢失 | `Promise.allSettled` 并发；单 locale 失败不影响其它 |
| OpenRouter 额度耗尽 | 全站翻译停摆 | 账户余额告警；`TRANSLATOR_MODEL` env 可切 Claude/GPT |
| Amplify build 连 DB 失败 | 构建失败 | Payload v3 不在 build 期连 DB；`payload migrate` 在 post-deploy 执行 |
| Center API `/auth/me` 改动 | SSO 破 | 写 e2e 测试覆盖该合约；`centerAuthStrategy` fallback 返回 `user: null` |
| 现有 `(manager)` layout root `<html>` vs 新 `(payload)` layout root `<html>` | 可能冲突 | 两者是不同 route groups，Next.js 允许；验证：步骤 1 `/cms` 页面加载后必测 |

---

## 待确认

无。所有决策已定，可进入 plan。
