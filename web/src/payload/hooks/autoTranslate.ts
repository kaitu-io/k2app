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

  // Serial, not parallel: each translateOperation ends in payload.update({ req })
  // which reuses the outer req.transactionID. Concurrent queries on a single
  // Postgres transaction trigger "another command already in progress" — one
  // update aborts the txn, the enclosing create is rolled back, and the doc
  // silently disappears. Sequential execution keeps every update on the same
  // txn one at a time so nothing races.
  for (const target of targets) {
    try {
      await translateOperation({
        collectionSlug: collection.slug,
        id: doc.id,
        locale: target.code,
        localeFrom: SOURCE_LOCALE,
        resolver: 'openai',
        update: true,
        req,
      })
    } catch (e) {
      req.payload.logger.error({
        msg: 'autoTranslate: locale failed',
        collection: collection.slug,
        id: doc.id,
        locale: target.code,
        err: e,
      })
    }
  }

  return doc
}
