import { getPayload } from 'payload'
import config from '@payload-config'
import { Link } from '@/i18n/routing'
import { setRequestLocale } from 'next-intl/server'
import { routing } from '@/i18n/routing'
import { getBrand } from '@/lib/brand-server'
import Header from '@/components/Header'
import Footer from '@/components/Footer'

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

  const brand = await getBrand()

  const visibilityField = brand.id === 'kaitu' ? 'showOnKaitu' : 'showOnOverleap'
  const payload = await getPayload({ config })
  const { docs } = await payload.find({
    collection: 'posts',
    locale: locale as (typeof routing.locales)[number],
    where: {
      and: [
        { status: { equals: 'published' } },
        { [visibilityField]: { equals: true } },
      ],
    },
    sort: '-publishedAt',
    limit: 50,
    depth: 1,
    overrideAccess: true,
  })
  const posts = docs as unknown as BlogListItem[]

  return (
    <>
      <Header />
      <div className="mx-auto max-w-3xl px-4 py-12">
        <h1 className="mb-8 text-3xl font-bold">{'Blog'}</h1>
        {posts.length === 0 ? (
          <p className="text-muted-foreground">
            {locale.startsWith('zh') ? '即将上线，敬请期待。' : 'Coming soon.'}
          </p>
        ) : (
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
        )}
      </div>
      <Footer />
    </>
  )
}

export async function generateMetadata({ params }: Props) {
  const { locale } = await params
  const brand = await getBrand()
  return {
    title: `Blog | ${brand.displayName}`,
    description: locale.startsWith('zh')
      ? `${brand.displayName} 博客 — 技术文章与产品动态`
      : `${brand.displayName} Blog — technical articles and product updates`,
    alternates: {
      canonical: `${brand.baseUrl}/${locale}/blog`,
    },
  }
}
