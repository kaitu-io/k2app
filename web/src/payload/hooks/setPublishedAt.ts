import type { CollectionBeforeChangeHook } from 'payload'

export const setPublishedAt: CollectionBeforeChangeHook = async ({
  data, originalDoc,
}) => {
  const wasPublished = originalDoc?.status === 'published'
  const isPublished = data.status === 'published'
  if (!wasPublished && isPublished && !data.publishedAt) {
    data.publishedAt = new Date().toISOString()
  }
  return data
}
