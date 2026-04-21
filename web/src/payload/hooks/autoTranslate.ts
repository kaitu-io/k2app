import type { CollectionAfterChangeHook } from 'payload'
import { translateOperation } from '@payload-enchants/translator'

const SOURCE_LOCALE = 'zh-CN'

export const autoTranslate: CollectionAfterChangeHook = async ({
  collection, doc, req,
}) => {
  if (req.locale !== SOURCE_LOCALE) return doc
  if (!req.payload.config.localization) return doc

  const targets = req.payload.config.localization.locales
    .filter(l => l.code !== SOURCE_LOCALE)

  const results = await Promise.allSettled(
    targets.map(({ code }) =>
      translateOperation({
        collectionSlug: collection.slug,
        id: doc.id,
        locale: code,
        localeFrom: SOURCE_LOCALE,
        resolver: 'openai',
        update: true,
        req,
      }),
    ),
  )

  for (let i = 0; i < results.length; i++) {
    const r = results[i]
    if (r.status === 'rejected') {
      req.payload.logger.error({
        msg: 'autoTranslate: locale failed',
        collection: collection.slug,
        id: doc.id,
        locale: targets[i].code,
        err: r.reason,
      })
    }
  }

  return doc
}
