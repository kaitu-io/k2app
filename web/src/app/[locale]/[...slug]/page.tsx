import { cache } from 'react'
import { notFound } from 'next/navigation'
import { setRequestLocale } from 'next-intl/server'
import type { Metadata } from 'next'
import { getPayload } from 'payload'
import config from '@payload-config'
import { RichText } from '@payloadcms/richtext-lexical/react'
import type { SerializedEditorState } from '@payloadcms/richtext-lexical/lexical'
import { Link } from '@/i18n/routing'
import { routing } from '@/i18n/routing'
import { getBrand } from '@/lib/brand-server'
import { generateMetadata as generatePageMetadata } from '../metadata'
import Header from '@/components/Header'
import Footer from '@/components/Footer'
import {
  findCategoryBySlug,
  findPostInCategory,
  listPostsInCategory,
  type CategoryDoc,
  type PostDoc,
  type PostListItem,
} from './queries'

export const dynamic = 'force-dynamic'

type Props = {
  params: Promise<{ locale: string; slug: string[] }>
}

type Locale = (typeof routing.locales)[number]

const getCategory = cache(
  async (locale: Locale, slug: string) => {
    const payload = await getPayload({ config })
    return findCategoryBySlug(payload, locale, slug)
  },
)

const getPost = cache(
  async (locale: Locale, categoryId: number | string, postSlug: string) => {
    const payload = await getPayload({ config })
    return findPostInCategory(payload, locale, categoryId, postSlug)
  },
)

export default async function CatchAll({ params }: Props) {
  const { locale, slug } = await params
  setRequestLocale(locale as Locale)

  const brand = await getBrand()
  const visibilityField = brand.id === 'kaitu' ? 'showOnKaitu' : 'showOnOverleap'
  const payload = await getPayload({ config })

  if (slug.length === 1) {
    const category = await getCategory(locale as Locale, slug[0])
    if (!category) notFound()
    const posts = await listPostsInCategory(payload, locale as Locale, category.id, visibilityField)
    return <CategoryListPage category={category} posts={posts} locale={locale} />
  }

  if (slug.length === 2) {
    const [catSlug, postSlug] = slug
    const category = await getCategory(locale as Locale, catSlug)
    if (!category) notFound()
    const post = await getPost(locale as Locale, category.id, postSlug)
    if (!post) notFound()
    const visible = brand.id === 'kaitu' ? post.showOnKaitu : post.showOnOverleap
    if (!visible) notFound()
    return <PostDetailPage post={post} locale={locale} />
  }

  notFound()
}

function CategoryListPage({
  category,
  posts,
  locale,
}: {
  category: CategoryDoc
  posts: PostListItem[]
  locale: string
}) {
  return (
    <>
      <Header />
      <main className="mx-auto max-w-3xl px-4 py-12">
        <h1 className="mb-8 text-3xl font-bold">{category.name}</h1>
        {posts.length === 0 ? (
          <p className="text-muted-foreground">
            {locale.startsWith('zh') ? '即将上线，敬请期待。' : 'Coming soon.'}
          </p>
        ) : (
          <ul className="space-y-6">
            {posts.map((post) => (
              <li key={post.id}>
                <Link
                  href={`/${category.slug}/${post.slug}`}
                  className="block hover:underline"
                >
                  <h2 className="text-xl font-semibold">{post.title}</h2>
                  {post.excerpt && (
                    <p className="mt-2 text-muted-foreground">{post.excerpt}</p>
                  )}
                </Link>
              </li>
            ))}
          </ul>
        )}
      </main>
      <Footer />
    </>
  )
}

function PostDetailPage({ post, locale }: { post: PostDoc; locale: string }) {
  return (
    <>
      <Header />
      <main>
        <article className="prose dark:prose-invert mx-auto max-w-3xl px-4 py-12">
          <h1>{post.title}</h1>
          {post.publishedAt && (
            <time dateTime={post.publishedAt}>
              {new Date(post.publishedAt).toLocaleDateString(locale)}
            </time>
          )}
          <RichText data={post.content as SerializedEditorState} />
        </article>
      </main>
      <Footer />
    </>
  )
}

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { locale, slug } = await params
  const brand = await getBrand()
  // Path without the locale prefix — the shared helper re-adds `/{locale}`.
  const pathname = `/${slug.join('/')}`

  if (slug.length === 1) {
    const category = await getCategory(locale as Locale, slug[0])
    if (!category) return {}
    // Route through the shared helper so category pages get their own
    // openGraph/twitter/hreflang instead of inheriting the homepage defaults
    // via Next.js shallow metadata merge.
    return generatePageMetadata(
      locale,
      pathname,
      {
        title: `${category.name} | ${brand.displayName}`,
        description: category.description ?? undefined,
      },
      brand,
    )
  }

  if (slug.length === 2) {
    const [catSlug, postSlug] = slug
    const category = await getCategory(locale as Locale, catSlug)
    if (!category) return {}
    const post = await getPost(locale as Locale, category.id, postSlug)
    if (!post) return {}

    // Phase 2: brands are fully isolated — a post visible on this deployment
    // canonicalizes to this deployment. (Cross-brand canonicals died with the
    // dual-host model; visibility gating already 404s off-brand posts.)
    const canonicalUrl = `${brand.baseUrl}/${locale}/${category.slug}/${post.slug}`

    // coverImage is populated to a media doc (absolute CDN url) at depth>=1.
    const cover =
      post.coverImage && typeof post.coverImage === 'object'
        ? post.coverImage.url ?? undefined
        : undefined

    const meta = generatePageMetadata(
      locale,
      pathname,
      {
        title: `${post.title} | ${brand.displayName}`,
        description: post.excerpt,
        ogType: 'article',
        ogImage: cover,
        article: {
          publishedTime: post.publishedAt,
          section: category.name,
        },
      },
      brand,
    )

    // Own-brand canonical while keeping the helper's language alternates.
    return {
      ...meta,
      alternates: {
        ...meta.alternates,
        canonical: canonicalUrl,
      },
    }
  }

  return {}
}
