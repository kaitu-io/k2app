import type { CollectionAfterChangeHook } from 'payload'

const SOURCE_LOCALE = 'zh-CN'

// Per-locale fields cleared when the zh-CN source changes. lazyTranslate.ts
// detects an empty `title` (via fallbackLocale: null) and triggers translation
// on first read.
const TRANSLATABLE_FIELDS = ['title', 'excerpt', 'content'] as const

export const autoTranslate: CollectionAfterChangeHook = async ({
  collection, doc, req,
}) => {
  if (req.locale !== SOURCE_LOCALE) return doc
  if (!req.payload.config.localization) return doc

  // Lazy-translation model: on zh-CN write, null out every other locale's
  // translatable fields so SSR re-fetches see an empty translation and
  // synchronously translate that single locale on demand. Calling six
  // OpenRouter operations synchronously inside the request transaction
  // exceeds Amplify's 30s SSR Lambda timeout; deferring to read-time keeps
  // each request bounded to one translation.
  //
  // We go straight to db.updateOne to bypass collection validation (the
  // localized title/content fields are required, so payload.update would
  // reject the null values). Re-entry into this hook is impossible because
  // db.updateOne does not fire collection hooks.
  const targets = req.payload.config.localization.locales
    .map(l => l.code)
    .filter(code => code !== SOURCE_LOCALE)

  const cleared: Record<string, null> = {}
  for (const field of TRANSLATABLE_FIELDS) cleared[field] = null

  for (const locale of targets) {
    try {
      await req.payload.db.updateOne({
        collection: collection.slug,
        id: doc.id,
        locale,
        data: cleared,
        req,
      })
    } catch (e) {
      req.payload.logger.error({
        msg: 'autoTranslate: failed to clear stale translation',
        collection: collection.slug,
        id: doc.id,
        locale,
        err: e,
      })
    }
  }

  return doc
}
