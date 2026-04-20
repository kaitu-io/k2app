import { getPayload } from 'payload'
import config from '@payload-config'
import { notFound } from 'next/navigation'
import { setRequestLocale } from 'next-intl/server'
import { routing } from '@/i18n/routing'
import { RichText } from '@payloadcms/richtext-lexical/react'

type Props = {
  params: Promise<{ locale: string; slug: string }>
}

export default async function BlogDetailPage({ params }: Props) {
  const { locale, slug } = await params
  setRequestLocale(locale as (typeof routing.locales)[number])

  const payload = await getPayload({ config })
  const { docs } = await payload.find({
    collection: 'posts',
    locale: locale as any,
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

  const post = docs[0]
  if (!post) notFound()

  return (
    <article className="prose dark:prose-invert mx-auto max-w-3xl px-4 py-12">
      <h1>{(post as any).title}</h1>
      {(post as any).publishedAt && (
        <time dateTime={(post as any).publishedAt}>
          {new Date((post as any).publishedAt).toLocaleDateString(locale)}
        </time>
      )}
      <RichText data={(post as any).content} />
    </article>
  )
}

export async function generateMetadata({ params }: Props) {
  const { locale, slug } = await params
  const payload = await getPayload({ config })
  const { docs } = await payload.find({
    collection: 'posts',
    locale: locale as any,
    where: { slug: { equals: slug }, status: { equals: 'published' } },
    limit: 1,
    overrideAccess: true,
  })
  const post = docs[0] as any
  if (!post) return {}
  return {
    title: `${post.title} | Kaitu`,
    description: post.excerpt,
  }
}
