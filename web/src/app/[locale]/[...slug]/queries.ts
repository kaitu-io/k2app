import type { Payload } from 'payload'
import type { routing } from '@/i18n/routing'

type Locale = (typeof routing.locales)[number]

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
  const { docs } = await payload.find({
    collection: 'posts',
    locale,
    where: {
      and: [
        { slug: { equals: postSlug } },
        { status: { equals: 'published' } },
        { category: { equals: categoryId } },
      ],
    },
    limit: 1,
    depth: 2,
    overrideAccess: true,
  })
  return (docs[0] as unknown as PostDoc) ?? null
}

export async function listPostsInCategory(
  payload: Payload,
  locale: Locale,
  categoryId: number | string,
  brandVisibilityField: 'showOnKaitu' | 'showOnOverleap',
): Promise<PostListItem[]> {
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
