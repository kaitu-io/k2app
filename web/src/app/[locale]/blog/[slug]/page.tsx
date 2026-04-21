import { getPayload } from 'payload'
import config from '@payload-config'
import { notFound } from 'next/navigation'
import { setRequestLocale } from 'next-intl/server'
import { routing } from '@/i18n/routing'
import { RichText } from '@payloadcms/richtext-lexical/react'
import type { SerializedEditorState } from '@payloadcms/richtext-lexical/lexical'

export const dynamic = 'force-dynamic'

type BlogPost = {
  id: string | number
  slug: string
  title: string
  excerpt?: string
  publishedAt?: string
  content: SerializedEditorState
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

export default async function BlogDetailPage({ params }: Props) {
  const { locale, slug } = await params
  setRequestLocale(locale as (typeof routing.locales)[number])

  const post = await fetchPost(locale, slug)
  if (!post) notFound()

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
  return {
    title: `${post.title} | Kaitu`,
    description: post.excerpt,
  }
}
