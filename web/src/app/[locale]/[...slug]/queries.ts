import type { Payload, TypedFallbackLocale } from 'payload'
import type { routing } from '@/i18n/routing'
import { lazyTranslate, SOURCE_LOCALE } from '@/payload/lazyTranslate'

type Locale = (typeof routing.locales)[number]

// Payload's TypedFallbackLocale resolves to the generated locale union and
// rejects literal `false` at the type level even though the runtime accepts it
// (Local API: pass `false` to disable fallback entirely). Cast through unknown
// to bypass — this is the documented way to express "raw locale only".
const NO_FALLBACK = false as unknown as TypedFallbackLocale

export type CategoryDoc = {
  id: number | string
  slug: string
  name: string
  description?: string
}

export type PostDoc = {
  id: number | string
  slug: string
  title: string
  excerpt?: string
  publishedAt?: string
  content: unknown
  showOnKaitu: boolean
  showOnOverleap: boolean
}

export type PostListItem = Pick<PostDoc, 'id' | 'slug' | 'title' | 'excerpt' | 'publishedAt' | 'showOnKaitu' | 'showOnOverleap'>

export async function findCategoryBySlug(
  payload: Payload,
  locale: Locale,
  slug: string,
): Promise<CategoryDoc | null> {
  const { docs } = await payload.find({
    collection: 'categories',
    locale,
    where: { slug: { equals: slug } },
    limit: 1,
    depth: 0,
    overrideAccess: true,
  })
  return (docs[0] as unknown as CategoryDoc) ?? null
}

export async function findPostInCategory(
  payload: Payload,
  locale: Locale,
  categoryId: number | string,
  postSlug: string,
): Promise<PostDoc | null> {
  // Probe with no locale fallback so we can detect a missing translation. slug
  // is not localized, so the where clause matches regardless of translation
  // state.
  const probe = await payload.find({
    collection: 'posts',
    locale,
    fallbackLocale: NO_FALLBACK,
    where: {
      and: [
        { slug: { equals: postSlug } },
        { status: { equals: 'published' } },
        { category: { equals: categoryId } },
      ],
    },
    limit: 1,
    depth: 0,
    overrideAccess: true,
  })
  const probeDoc = probe.docs[0] as Record<string, unknown> | undefined
  if (!probeDoc) return null

  if (locale !== SOURCE_LOCALE && needsTranslation(probeDoc)) {
    await lazyTranslate({
      payload,
      collectionSlug: 'posts',
      docId: probeDoc.id as number | string,
      locale,
      isTranslated: (p) => !needsTranslation(p),
    })
  }

  // Final fetch with default fallback so callers either get the freshly
  // translated locale or the source-locale fallback (translation in progress
  // by another worker, timed out, or errored).
  const final = await payload.findByID({
    collection: 'posts',
    id: probeDoc.id as number | string,
    locale,
    depth: 2,
    overrideAccess: true,
  })
  return final as unknown as PostDoc
}

export async function listPostsInCategory(
  payload: Payload,
  locale: Locale,
  categoryId: number | string,
  brandVisibilityField: 'showOnKaitu' | 'showOnOverleap',
): Promise<PostListItem[]> {
  // Lists do not lazy-translate: a single SSR request listing N posts would
  // serialize N translations and blow past Amplify's 30s SSR Lambda cap.
  // Posts without a cached translation render via the default source-locale
  // fallback; the first detail-page visit for each post triggers translation
  // on-demand, after which subsequent list renders pick up the cached value.
  const { docs } = await payload.find({
    collection: 'posts',
    locale,
    where: {
      and: [
        { status: { equals: 'published' } },
        { category: { equals: categoryId } },
        { [brandVisibilityField]: { equals: true } },
      ],
    },
    sort: '-publishedAt',
    limit: 50,
    depth: 1,
    overrideAccess: true,
  })
  return docs as unknown as PostListItem[]
}

/** A post needs translation when its localized required fields are empty for the requested locale. */
function needsTranslation(doc: Record<string, unknown>): boolean {
  const title = doc.title
  const content = doc.content
  return !title || !content
}
