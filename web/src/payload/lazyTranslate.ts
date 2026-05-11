import { createHash } from 'crypto'
import type { Payload } from 'payload'
import { translateOperation } from '@payload-enchants/translator'

export const SOURCE_LOCALE = 'zh-CN'
export const DEFAULT_TIMEOUT_MS = 25_000
export const DEFAULT_RESOLVER = 'openai'

type PoolClient = {
  query: (sql: string, params?: unknown[]) => Promise<{ rows: Array<Record<string, unknown>> }>
  release: () => void
}

type PoolLike = {
  connect: () => Promise<PoolClient>
}

export type LazyTranslateArgs = {
  payload: Payload
  collectionSlug: string
  docId: number | string
  locale: string
  /** Skip lock + translation if the read-back probe shows the doc is already translated. */
  isTranslated?: (probe: Record<string, unknown>) => boolean
  /** Timeout for the underlying translateOperation call. Default 25s, well below Amplify's 30s SSR cap. */
  timeoutMs?: number
}

export type LazyTranslateResult =
  | { status: 'translated' }
  | { status: 'already-translated' }
  | { status: 'locked-by-other' }
  | { status: 'timeout' }
  | { status: 'error', error: unknown }

/**
 * Translate a single locale on demand, guarded by a Postgres advisory lock so
 * concurrent reads do not double-translate (and double-bill) the same doc.
 *
 * Caller responsibilities:
 * - Detect "needs translation" before calling (e.g. probe field with
 *   `fallbackLocale: false` and check title/content are empty).
 * - After this returns `translated` or `already-translated`, re-fetch the
 *   doc to read the populated locale.
 * - On `locked-by-other`, `timeout`, or `error`, fall back to the source
 *   locale value (caller's normal find with default fallbackLocale will do
 *   this automatically).
 */
export async function lazyTranslate(args: LazyTranslateArgs): Promise<LazyTranslateResult> {
  const { payload, collectionSlug, docId, locale, isTranslated, timeoutMs = DEFAULT_TIMEOUT_MS } = args

  if (locale === SOURCE_LOCALE) {
    return { status: 'already-translated' }
  }

  const lockKey = makeLockKey(collectionSlug, docId, locale)

  // Postgres advisory locks are session-scoped — we must acquire + release on
  // the same connection. translateOperation runs through Payload's pool, which
  // borrows separate connections; that's fine because session locks block
  // other sessions regardless of which connection runs the inner query.
  // payload.db.pool is the pg Pool exposed by @payloadcms/db-postgres; the
  // generic DatabaseAdapter type doesn't surface it, so we treat it as unknown
  // and feature-detect at runtime to stay agnostic across adapters.
  const pool = (payload.db as unknown as { pool?: PoolLike }).pool
  if (!pool || typeof pool.connect !== 'function') {
    payload.logger.warn({
      msg: 'lazyTranslate: pool unavailable, translating without lock',
      collection: collectionSlug,
      id: docId,
      locale,
    })
    return await translateWithTimeout(args, timeoutMs)
  }

  const client = await pool.connect()
  try {
    const { rows } = await client.query('SELECT pg_try_advisory_lock($1) AS locked', [lockKey])
    const got = rows?.[0]?.locked === true
    if (!got) {
      return { status: 'locked-by-other' }
    }

    // Double-check: another worker may have completed the translation between
    // the caller's probe and our lock acquisition. Skip the OpenRouter call if
    // so.
    if (isTranslated) {
      try {
        const probe = await payload.findByID({
          collection: collectionSlug,
          id: docId,
          locale,
          fallbackLocale: false as never,
          depth: 0,
          overrideAccess: true,
        }) as Record<string, unknown>
        if (isTranslated(probe)) {
          return { status: 'already-translated' }
        }
      } catch (e) {
        payload.logger.warn({
          msg: 'lazyTranslate: re-check probe failed, continuing to translate',
          collection: collectionSlug,
          id: docId,
          locale,
          err: e,
        })
      }
    }

    return await translateWithTimeout(args, timeoutMs)
  } finally {
    try {
      await client.query('SELECT pg_advisory_unlock($1)', [lockKey])
    } catch (e) {
      payload.logger.warn({
        msg: 'lazyTranslate: failed to release advisory lock',
        collection: collectionSlug,
        id: docId,
        locale,
        err: e,
      })
    }
    client.release()
  }
}

async function translateWithTimeout(args: LazyTranslateArgs, timeoutMs: number): Promise<LazyTranslateResult> {
  const { payload, collectionSlug, docId, locale } = args
  let timer: NodeJS.Timeout | undefined
  try {
    const timeoutPromise = new Promise<'__timeout__'>((resolve) => {
      timer = setTimeout(() => resolve('__timeout__'), timeoutMs)
    })
    const translatePromise = translateOperation({
      collectionSlug,
      id: docId,
      locale,
      localeFrom: SOURCE_LOCALE,
      resolver: DEFAULT_RESOLVER,
      update: true,
      payload,
    })

    const result = await Promise.race([translatePromise, timeoutPromise])
    if (result === '__timeout__') {
      payload.logger.error({
        msg: 'lazyTranslate: translation timed out, caller will fall back to source locale',
        collection: collectionSlug,
        id: docId,
        locale,
        timeoutMs,
      })
      return { status: 'timeout' }
    }
    return { status: 'translated' }
  } catch (error) {
    payload.logger.error({
      msg: 'lazyTranslate: translation failed',
      collection: collectionSlug,
      id: docId,
      locale,
      err: error,
    })
    return { status: 'error', error }
  } finally {
    if (timer) clearTimeout(timer)
  }
}

// Constants need BigInt() form because the tsconfig target is < ES2020.
const SIGNED_MAX = BigInt('0x7fffffffffffffff') // 2^63 - 1
const TWO_TO_64 = BigInt('0x10000000000000000') // 2^64

/** Stable 64-bit signed int derived from (collection, id, locale). pg advisory locks take bigint. */
export function makeLockKey(collectionSlug: string, docId: number | string, locale: string): bigint {
  const hash = createHash('sha1')
    .update(`payload-translate:${collectionSlug}:${docId}:${locale}`)
    .digest()
  // Take the first 8 bytes, interpret as signed 64-bit int.
  const u64 = hash.readBigUInt64BE(0)
  // Convert to signed range by reinterpreting the top bit.
  return u64 > SIGNED_MAX ? u64 - TWO_TO_64 : u64
}
