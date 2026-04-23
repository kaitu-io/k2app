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
import { brandById, type BrandId } from '@/lib/brands'
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

function resolveCanonicalBrand(
  locale: string,
  showOnKaitu: boolean,
  showOnOverleap: boolean,
): BrandId {
  if (showOnKaitu && !showOnOverleap) return 'kaitu'
  if (showOnOverleap && !showOnKaitu) return 'overleap'
  return locale.startsWith('en-') ? 'overleap' : 'kaitu'
}

export default async function CatchAll({ params }: Props) {
  const { locale, slug } = await params
  setRequestLocale(locale as Locale)

  const brand = await getBrand()
  const visibilityField = brand.id === 'kaitu' ? 'showOnKaitu' : 'showOnOverleap'
  const payload = await getPayload({ config })

  if (slug.length === 1) {
    const category = await findCategoryBySlug(payload, locale as Locale, slug[0])
    if (!category) notFound()
    const posts = await listPostsInCategory(payload, locale as Locale, category.id, visibilityField)
    return <CategoryListPage category={category} posts={posts} />
  }

  if (slug.length === 2) {
    const [catSlug, postSlug] = slug
    const category = await findCategoryBySlug(payload, locale as Locale, catSlug)
    if (!category) notFound()
    const post = await findPostInCategory(payload, locale as Locale, category.id, postSlug)
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
}: {
  category: CategoryDoc
  posts: PostListItem[]
}) {
  return (
    <>
      <Header />
      <div className="mx-auto max-w-3xl px-4 py-12">
        <h1 className="mb-8 text-3xl font-bold">{category.name}</h1>
        {posts.length === 0 ? (
          <p className="text-muted-foreground">{'Coming soon.'}</p>
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
      </div>
      <Footer />
    </>
  )
}

function PostDetailPage({ post, locale }: { post: PostDoc; locale: string }) {
  return (
    <>
      <Header />
      <article className="prose dark:prose-invert mx-auto max-w-3xl px-4 py-12">
        <h1>{post.title}</h1>
        {post.publishedAt && (
          <time dateTime={post.publishedAt}>
            {new Date(post.publishedAt).toLocaleDateString(locale)}
          </time>
        )}
        <RichText data={post.content as SerializedEditorState} />
      </article>
      <Footer />
    </>
  )
}

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { locale, slug } = await params
  const brand = await getBrand()
  const payload = await getPayload({ config })

  if (slug.length === 1) {
    const category = await findCategoryBySlug(payload, locale as Locale, slug[0])
    if (!category) return {}
    return {
      title: `${category.name} | ${brand.displayName}`,
      description: category.description ?? undefined,
    }
  }

  if (slug.length === 2) {
    const [catSlug, postSlug] = slug
    const category = await findCategoryBySlug(payload, locale as Locale, catSlug)
    if (!category) return {}
    const post = await findPostInCategory(payload, locale as Locale, category.id, postSlug)
    if (!post) return {}

    const canonicalBrandId = resolveCanonicalBrand(
      locale,
      post.showOnKaitu,
      post.showOnOverleap,
    )
    const canonicalUrl = `${brandById(canonicalBrandId).baseUrl}/${locale}/${category.slug}/${post.slug}`

    return {
      title: `${post.title} | ${brand.displayName}`,
      description: post.excerpt,
      alternates: {
        canonical: canonicalUrl,
      },
    }
  }

  return {}
}
