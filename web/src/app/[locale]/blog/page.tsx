import { getPayload } from 'payload'
import config from '@payload-config'
import { Link } from '@/i18n/routing'
import { setRequestLocale } from 'next-intl/server'
import { routing } from '@/i18n/routing'

export const dynamic = 'force-dynamic'

type BlogListItem = {
  id: string | number
  slug: string
  title: string
  excerpt?: string
}

type Props = {
  params: Promise<{ locale: string }>
}

export default async function BlogIndexPage({ params }: Props) {
  const { locale } = await params
  setRequestLocale(locale as (typeof routing.locales)[number])

  const payload = await getPayload({ config })
  const { docs } = await payload.find({
    collection: 'posts',
    locale: locale as (typeof routing.locales)[number],
    where: { status: { equals: 'published' } },
    sort: '-publishedAt',
    limit: 50,
    depth: 1,
    overrideAccess: true,
  })

  const posts = docs as unknown as BlogListItem[]

  return (
    <div className="mx-auto max-w-3xl px-4 py-12">
      <h1 className="mb-8 text-3xl font-bold">{'Blog'}</h1>
      <ul className="space-y-6">
        {posts.map((post) => (
          <li key={post.id}>
            <Link href={`/blog/${post.slug}`} className="block hover:underline">
              <h2 className="text-xl font-semibold">{post.title}</h2>
              {post.excerpt && <p className="mt-2 text-muted-foreground">{post.excerpt}</p>}
            </Link>
          </li>
        ))}
      </ul>
    </div>
  )
}

export async function generateMetadata({ params }: Props) {
  const { locale } = await params
  return {
    title: 'Blog | Kaitu',
    description: locale.startsWith('zh') ? 'Kaitu 博客 — 技术文章与产品动态' : 'Kaitu Blog — technical articles and product updates',
  }
}
