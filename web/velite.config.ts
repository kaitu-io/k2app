import { defineCollection, defineConfig, s } from 'velite'

/**
 * Post collection — content files live at:
 *   content/{locale}/{category}/{slug}.md
 *
 * The `locale` and `slug` fields are extracted from the file's relative path.
 * `s.path()` provides the path relative to the collection root (without extension).
 *
 * A file at `content/zh-CN/blog/hello-world.md` produces:
 *   filePath: "zh-CN/blog/hello-world"
 *   locale:   "zh-CN"
 *   slug:     "blog/hello-world"
 */
const posts = defineCollection({
  name: 'Post',
  pattern: '**/*.md',
  schema: s
    .object({
      title: s.string(),
      date: s.isodate(),
      summary: s.string().optional(),
      tags: s.array(s.string()).optional(),
      coverImage: s.string().optional(),
      draft: s.boolean().default(false),
      order: s.number().optional(),
      section: s.string().optional(),
      content: s.markdown(),
      metadata: s.metadata(),
      // filePath is injected by velite: the path relative to the collection root,
      // without extension (e.g. "zh-CN/blog/hello-world").
      filePath: s.path(),
    })
    .transform((data) => {
      // Split filePath into [locale, ...rest]
      // e.g. "zh-CN/blog/hello-world" → locale="zh-CN", slug="blog/hello-world"
      const segments = data.filePath.split('/')
      const locale = segments[0] ?? 'zh-CN'
      const slug = segments.slice(1).join('/')

      return {
        ...data,
        locale,
        slug,
      }
    }),
})

export default defineConfig({
  root: 'content',
  output: {
    data: '.velite',
    assets: 'public/static',
    base: '/static/',
    name: '[name]-[hash:6].[ext]',
    clean: true,
  },
  collections: { posts },
})
