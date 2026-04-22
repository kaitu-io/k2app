/**
 * Payload CMS Posts — standalone (non-factory) tools that compose multiple
 * HTTP calls.
 *
 * - `get_post_all_locales` — parallel GET across the 7 configured locales,
 *   aggregated into a {locale: doc} map. Per-locale failures are captured
 *   as `{__error: message}` so one missing translation doesn't kill the
 *   whole fetch.
 *
 * - `retranslate_post` — GET the zh-CN source doc, then PATCH the same fields
 *   back at `?locale=zh-CN`. Re-issuing the write fires Payload's afterChange
 *   hook, which in turn triggers the `autoTranslate` fan-out to the 6 other
 *   locales.
 *
 * Both tools talk to Payload REST via the `cms` client. The client returns
 * the raw body directly and throws on non-2xx — there is no
 * `{code, message, data}` envelope here, unlike the Center Go API.
 */

import { z } from 'zod'
import type { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js'
import type { CenterApiClient } from '../center-api.js'
import { audit } from '../audit.js'

/** The 7 locales configured on the Posts collection (see `web/src/payload.config.ts`). */
const ALL_LOCALES = ['zh-CN', 'en-US', 'en-GB', 'en-AU', 'zh-TW', 'zh-HK', 'ja'] as const

/**
 * Registers the `get_post_all_locales` MCP tool.
 *
 * Fires 7 parallel `GET /payload/api/posts/{id}?locale={loc}` requests via
 * `Promise.all`. Each per-locale fetch is wrapped in try/catch so a 404 on
 * one locale (e.g. an untranslated doc) records `{__error: message}` under
 * that locale key without aborting the others.
 *
 * Returns a `{locale: doc | {__error}}` map as the MCP content payload.
 */
export function registerGetPostAllLocales(server: McpServer, cms: CenterApiClient): void {
  server.tool(
    'get_post_all_locales',
    'Fetch a post in all 7 configured locales in parallel. Returns {locale: doc} map; failed locales record {__error}.',
    {
      id: z.union([z.string(), z.number()]).describe('Post ID'),
      draft: z.boolean().optional().describe('Include draft version'),
    },
    async (params: { id: string | number; draft?: boolean }) => {
      try {
        const id = encodeURIComponent(String(params.id))
        const draftSuffix = params.draft ? '&draft=true' : ''

        const entries = await Promise.all(
          ALL_LOCALES.map(async (locale) => {
            const path = `/payload/api/posts/${id}?locale=${locale}${draftSuffix}`
            try {
              const doc = await cms.request(path)
              return [locale, doc] as const
            } catch (err) {
              const message = err instanceof Error ? err.message : String(err)
              return [locale, { __error: message }] as const
            }
          }),
        )

        const result: Record<string, unknown> = {}
        for (const [locale, doc] of entries) {
          result[locale] = doc
        }

        await audit('get_post_all_locales', params as Record<string, unknown>)
        return {
          content: [
            {
              type: 'text' as const,
              text: JSON.stringify(result, null, 2),
            },
          ],
        }
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err)
        await audit('get_post_all_locales', { ...(params as Record<string, unknown>), error: message })
        return {
          content: [
            {
              type: 'text' as const,
              text: JSON.stringify({ error: message }),
            },
          ],
        }
      }
    },
  )
}

/**
 * Registers the `retranslate_post` MCP tool.
 *
 * Flow:
 *   1. GET `/payload/api/posts/{id}?locale=zh-CN` — fetch the source doc.
 *   2. PATCH the same path with `{title, slug, excerpt, content}` from the
 *      fetched doc. The write re-fires Payload's afterChange hook, which
 *      re-runs `autoTranslate` and fans changes out to the 6 non-zh locales.
 *
 * On GET failure, returns `{error}` without issuing the PATCH. On PATCH
 * failure, surfaces the error the same way.
 */
export function registerRetranslatePost(server: McpServer, cms: CenterApiClient): void {
  server.tool(
    'retranslate_post',
    'Re-fire the autoTranslate afterChange hook on a post by GETting the zh-CN source and PATCHing the same fields back.',
    {
      id: z.union([z.string(), z.number()]).describe('Post ID'),
    },
    async (params: { id: string | number }) => {
      const id = encodeURIComponent(String(params.id))
      const path = `/payload/api/posts/${id}?locale=zh-CN`

      // Step 1: GET the source doc
      let source: Record<string, unknown>
      try {
        source = (await cms.request(path)) as Record<string, unknown>
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err)
        await audit('retranslate_post', { ...(params as Record<string, unknown>), error: message, stage: 'get' })
        return {
          content: [
            {
              type: 'text' as const,
              text: JSON.stringify({ error: message }),
            },
          ],
        }
      }

      // Step 2: PATCH the source fields back to trigger the afterChange hook
      const body = {
        title: source['title'],
        slug: source['slug'],
        excerpt: source['excerpt'],
        content: source['content'],
      }

      try {
        await cms.request(path, {
          method: 'PATCH',
          body: JSON.stringify(body),
        })
      } catch (err) {
        const message = err instanceof Error ? err.message : String(err)
        await audit('retranslate_post', { ...(params as Record<string, unknown>), error: message, stage: 'patch' })
        return {
          content: [
            {
              type: 'text' as const,
              text: JSON.stringify({ error: message }),
            },
          ],
        }
      }

      await audit('retranslate_post', params as Record<string, unknown>)
      return {
        content: [
          {
            type: 'text' as const,
            text: JSON.stringify({ retranslated: true, id: params.id }),
          },
        ],
      }
    },
  )
}
