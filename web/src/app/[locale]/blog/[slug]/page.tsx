import { getPayload } from 'payload'
import config from '@payload-config'
import { notFound } from 'next/navigation'
import { setRequestLocale } from 'next-intl/server'
import { routing } from '@/i18n/routing'
import { RichText } from '@payloadcms/richtext-lexical/react'
import type { SerializedEditorState } from '@payloadcms/richtext-lexical/lexical'
import { getBrand } from '@/lib/brand-server'
import { brandById, type BrandId } from '@/lib/brands'

export const dynamic = 'force-dynamic'

type BlogPost = {
  id: string | number
  slug: string
  title: string
  excerpt?: string
  publishedAt?: string
  content: SerializedEditorState
  brand?: string | null
  canonicalBrand?: string | null
}

type Props = {
  params: Promise<{ locale: string; slug: string }>
}

async function fetchPost(locale: string, slug: string): Promise<BlogPost | null> {
  const payload = await getPayload({ config })
  const { docs } = await payload.find({
    collection: 'posts',
    locale: locale as (typeof routing.locales)[number],
    where: {
      and: [
        { slug: { equals: slug } },
        { status: { equals: 'published' } },
      ],
    },
    limit: 1,
    depth: 2,
    overrideAccess: true,
  })
  return (docs[0] as unknown as BlogPost) ?? null
}

// Determine which brand's base URL should own the canonical link for a post.
// Rules: explicit `canonicalBrand` wins; otherwise single-brand posts canonical
// to that brand; 'both' (or missing) defaults by locale — en-* → overleap, else kaitu.
function resolveCanonicalBrand(
  locale: string,
  postBrand: string | null | undefined,
  canonicalBrand: string | null | undefined,
): BrandId {
  if (canonicalBrand === 'kaitu' || canonicalBrand === 'overleap') return canonicalBrand;
  if (postBrand === 'kaitu' || postBrand === 'overleap') return postBrand;
  return locale.startsWith('en-') ? 'overleap' : 'kaitu';
}

export default async function BlogDetailPage({ params }: Props) {
  const { locale, slug } = await params
  setRequestLocale(locale as (typeof routing.locales)[number])

  const currentBrand = await getBrand()
  const post = await fetchPost(locale, slug)
  if (!post) notFound()

  // Respect brand visibility: if this post is pinned to a different brand, 404 on this host.
  if (post.brand && post.brand !== 'both' && post.brand !== currentBrand.id) {
    notFound()
  }

  return (
    <article className="prose dark:prose-invert mx-auto max-w-3xl px-4 py-12">
      <h1>{post.title}</h1>
      {post.publishedAt && (
        <time dateTime={post.publishedAt}>
          {new Date(post.publishedAt).toLocaleDateString(locale)}
        </time>
      )}
      <RichText data={post.content} />
    </article>
  )
}

export async function generateMetadata({ params }: Props) {
  const { locale, slug } = await params
  const post = await fetchPost(locale, slug)
  if (!post) return {}

  const currentBrand = await getBrand()
  const canonicalBrandId = resolveCanonicalBrand(locale, post.brand, post.canonicalBrand)
  const canonicalUrl = `${brandById(canonicalBrandId).baseUrl}/${locale}/blog/${post.slug}`

  return {
    title: `${post.title} | ${currentBrand.displayName}`,
    description: post.excerpt,
    alternates: {
      canonical: canonicalUrl,
    },
  }
}
