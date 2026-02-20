import { notFound } from 'next/navigation'
import { Metadata } from 'next'
import { getPayload } from 'payload'
import config from '@payload-config'
import { RichText } from '@payloadcms/richtext-lexical/react'
import Image from 'next/image'
import { getArticleWithLazyTranslation } from '@/lib/lazy-translation'

interface PageProps {
  params: Promise<{
    locale: string
    slug: string[]
  }>
}

// Generate metadata for SEO
export async function generateMetadata({ params }: PageProps): Promise<Metadata> {
  const { locale, slug } = await params
  const path = '/' + slug.join('/')
  const article = await getArticleWithLazyTranslation(path, locale)

  if (!article) {
    return {
      title: 'Not Found',
    }
  }

  const seo = article.seo as { seoTitle?: string; seoDescription?: string; ogImage?: { url?: string } } | undefined

  return {
    title: seo?.seoTitle || article.title,
    description: seo?.seoDescription || article.summary || undefined,
    openGraph: {
      title: seo?.seoTitle || article.title,
      description: seo?.seoDescription || article.summary || undefined,
      images: seo?.ogImage?.url ? [seo.ogImage.url] : undefined,
    },
  }
}

export default async function CMSPage({ params }: PageProps) {
  const { locale, slug } = await params
  const path = '/' + slug.join('/')

  const article = await getArticleWithLazyTranslation(path, locale)

  if (!article) {
    notFound()
  }

  const featuredImage = article.featuredImage as { url?: string; alt?: string } | undefined

  return (
    <article className="container mx-auto px-4 py-8 max-w-4xl">
      {/* Article Header */}
      <header className="mb-8">
        <h1 className="text-4xl font-bold mb-4">{article.title}</h1>

        {article.summary && (
          <p className="text-xl text-muted-foreground mb-4">{article.summary}</p>
        )}

        <div className="flex items-center gap-4 text-sm text-muted-foreground">
          {article.category && (
            <span className="px-2 py-1 bg-muted rounded">{article.category}</span>
          )}
          {article.publishedAt && (
            <time dateTime={article.publishedAt}>
              {new Date(article.publishedAt).toLocaleDateString(locale, {
                year: 'numeric',
                month: 'long',
                day: 'numeric',
              })}
            </time>
          )}
        </div>
      </header>

      {/* Featured Image */}
      {featuredImage?.url && (
        <div className="relative w-full h-64 md:h-96 mb-8 rounded-lg overflow-hidden">
          <Image
            src={featuredImage.url}
            alt={featuredImage.alt || article.title}
            fill
            className="object-cover"
            priority
          />
        </div>
      )}

      {/* Article Content */}
      <div className="prose prose-lg dark:prose-invert max-w-none">
        {article.content ? <RichText data={article.content as Parameters<typeof RichText>[0]['data']} /> : null}
      </div>

      {/* Tags */}
      {article.tags && article.tags.length > 0 && (
        <div className="mt-8 pt-8 border-t">
          <div className="flex flex-wrap gap-2">
            {(article.tags as { tag: string }[]).map((tagItem, index) => (
              <span
                key={index}
                className="px-3 py-1 bg-muted text-sm rounded-full"
              >
                {`#${tagItem.tag}`}
              </span>
            ))}
          </div>
        </div>
      )}
    </article>
  )
}

// Generate static params for published articles (optional, for SSG)
export async function generateStaticParams() {
  // Skip static generation if DATABASE_URI is not configured
  if (!process.env.DATABASE_URI) {
    console.log('DATABASE_URI not set, skipping static params generation')
    return []
  }

  try {
    const payload = await getPayload({ config })

    // Use Payload's built-in _status field from versions.drafts
    const articles = await payload.find({
      collection: 'articles',
      where: {
        _status: { equals: 'published' },
      },
      limit: 100,
    })

    const params: { locale: string; slug: string[] }[] = []
    const locales = ['zh-CN', 'zh-TW', 'zh-HK', 'en-US', 'en-GB', 'en-AU', 'ja']

    for (const article of articles.docs) {
      if (article.path) {
        const slugSegments = article.path.split('/').filter(Boolean)
        for (const locale of locales) {
          params.push({
            locale,
            slug: slugSegments,
          })
        }
      }
    }

    return params
  } catch (error) {
    console.error('Error generating static params:', error)
    return []
  }
}
