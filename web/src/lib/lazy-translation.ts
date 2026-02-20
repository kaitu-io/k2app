/**
 * Lazy Translation Service
 *
 * Translates article content on-demand when accessed in a non-source locale.
 * Translations are cached back to Payload CMS database.
 */

import { getPayload } from 'payload'
import config from '@payload-config'
import { translateContent, translateLexicalContent } from '@/payload/hooks/ai-content-hook'

// Source locale - articles are written in this language
const SOURCE_LOCALE = 'zh-CN'

// Supported locales
type Locale = 'zh-CN' | 'zh-TW' | 'zh-HK' | 'en-US' | 'en-GB' | 'en-AU' | 'ja'

interface Article {
  id: string
  title: string
  summary?: string | null
  content?: unknown
  path: string
  category?: string | null
  tags?: Array<{ tag: string }> | null
  publishedAt?: string | null
  featuredImage?: { url?: string; alt?: string } | null
  seo?: {
    seoTitle?: string | null
    seoDescription?: string | null
    ogImage?: { url?: string } | null
  } | null
}

/**
 * Get article with lazy translation support
 *
 * 1. Fetch article in requested locale
 * 2. If content is same as source (no translation), translate and save
 * 3. Return translated content
 */
export async function getArticleWithLazyTranslation(
  path: string,
  locale: string
): Promise<Article | null> {
  try {
    const payload = await getPayload({ config })

    // Fetch in requested locale
    // Note: Access control automatically filters by _status: 'published' for unauthenticated requests
    const articles = await payload.find({
      collection: 'articles',
      where: {
        path: { equals: path },
      },
      locale: locale as Locale,
      limit: 1,
    })

    const article = articles.docs[0] as Article | undefined
    if (!article) return null

    // If requesting source locale, no translation needed
    if (locale === SOURCE_LOCALE) {
      return article
    }

    // Check if AI translation is enabled
    if (process.env.AI_ENABLED !== 'true' || !process.env.AI_API_KEY) {
      return article
    }

    // Fetch source locale version to compare
    const sourceArticles = await payload.find({
      collection: 'articles',
      where: {
        path: { equals: path },
      },
      locale: SOURCE_LOCALE,
      limit: 1,
    })

    const sourceArticle = sourceArticles.docs[0] as Article | undefined
    if (!sourceArticle) return article

    // Check if translation is needed (content matches source = no translation exists)
    const needsTranslation =
      article.title === sourceArticle.title ||
      JSON.stringify(article.content) === JSON.stringify(sourceArticle.content)

    if (!needsTranslation) {
      // Translation already exists
      return article
    }

    console.log(`[LazyTranslation] Translating article "${path}" to ${locale}`)

    // Translate fields
    const [translatedTitle, translatedSummary, translatedContent] = await Promise.all([
      translateContent(sourceArticle.title, SOURCE_LOCALE, locale),
      sourceArticle.summary
        ? translateContent(sourceArticle.summary, SOURCE_LOCALE, locale)
        : Promise.resolve(null),
      sourceArticle.content
        ? translateLexicalContent(sourceArticle.content, SOURCE_LOCALE, locale)
        : Promise.resolve(null),
    ])

    // Translate SEO fields if they exist
    let translatedSeo = article.seo
    if (sourceArticle.seo?.seoTitle || sourceArticle.seo?.seoDescription) {
      const [seoTitle, seoDescription] = await Promise.all([
        sourceArticle.seo.seoTitle
          ? translateContent(sourceArticle.seo.seoTitle, SOURCE_LOCALE, locale)
          : Promise.resolve(null),
        sourceArticle.seo.seoDescription
          ? translateContent(sourceArticle.seo.seoDescription, SOURCE_LOCALE, locale)
          : Promise.resolve(null),
      ])
      translatedSeo = {
        ...article.seo,
        seoTitle: seoTitle || article.seo?.seoTitle,
        seoDescription: seoDescription || article.seo?.seoDescription,
      }
    }

    // Save translation back to database (async, don't wait)
    saveTranslation(article.id, locale, {
      title: translatedTitle,
      summary: translatedSummary,
      content: translatedContent,
      seo: translatedSeo,
    }).catch(err => console.error('[LazyTranslation] Failed to save:', err))

    // Return translated content immediately
    return {
      ...article,
      title: translatedTitle,
      summary: translatedSummary,
      content: translatedContent,
      seo: translatedSeo,
    }
  } catch (error) {
    console.error('[LazyTranslation] Error:', error)
    return null
  }
}

/**
 * Save translation back to Payload CMS
 */
async function saveTranslation(
  articleId: string,
  locale: string,
  data: {
    title: string
    summary?: string | null
    content?: unknown
    seo?: Article['seo']
  }
): Promise<void> {
  try {
    const payload = await getPayload({ config })

    await payload.update({
      collection: 'articles',
      id: articleId,
      locale: locale as Locale,
      data: {
        title: data.title,
        summary: data.summary || undefined,
        content: data.content,
        seo: data.seo ? {
          seoTitle: data.seo.seoTitle || undefined,
          seoDescription: data.seo.seoDescription || undefined,
        } : undefined,
      },
    })

    console.log(`[LazyTranslation] Saved translation for article ${articleId} in ${locale}`)
  } catch (error) {
    console.error('[LazyTranslation] Failed to save translation:', error)
    throw error
  }
}
