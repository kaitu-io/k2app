import { describe, it, expect, vi } from 'vitest'
import { registerGetPostAllLocales, registerRetranslatePost } from './cms-post-helpers.js'
import type { CenterApiClient } from '../center-api.js'

vi.mock('../audit.js', () => ({
  audit: vi.fn().mockResolvedValue(undefined),
}))

function createMockServer() {
  const tools: Record<string, { description: string; schema: unknown; handler: Function }> = {}
  return {
    tool: vi.fn((name: string, description: string, schema: unknown, handler: Function) => {
      tools[name] = { description, schema, handler }
    }),
    _tools: tools,
  }
}

describe('get_post_all_locales', () => {
  it('fetches all 7 locales in parallel and returns a {locale: doc} map', async () => {
    const cms = {
      request: vi.fn().mockImplementation((path: string) => {
        const match = path.match(/locale=([a-zA-Z-]+)/)
        const locale = match ? match[1] : 'unknown'
        return Promise.resolve({ id: 5, title: `Title in ${locale}` })
      }),
    } as unknown as CenterApiClient

    const server = createMockServer()
    registerGetPostAllLocales(server as any, cms)

    const result = (await server._tools['get_post_all_locales'].handler({ id: 5 })) as any
    const parsed = JSON.parse(result.content[0].text)

    expect((cms.request as any).mock.calls).toHaveLength(7)
    const expectedLocales = ['zh-CN', 'en-US', 'en-GB', 'en-AU', 'zh-TW', 'zh-HK', 'ja']
    for (const loc of expectedLocales) {
      expect(parsed).toHaveProperty(loc)
      expect(parsed[loc]).toMatchObject({ id: 5, title: `Title in ${loc}` })
    }
  })

  it('records __error per locale when that locale fetch fails, without killing the others', async () => {
    const cms = {
      request: vi.fn().mockImplementation((path: string) => {
        if (path.includes('locale=en-AU')) {
          return Promise.reject(new Error('GET /payload/api/posts/5?locale=en-AU → HTTP 404: Not Found'))
        }
        const match = path.match(/locale=([a-zA-Z-]+)/)
        const locale = match ? match[1] : 'unknown'
        return Promise.resolve({ id: 5, title: `Title in ${locale}` })
      }),
    } as unknown as CenterApiClient

    const server = createMockServer()
    registerGetPostAllLocales(server as any, cms)

    const result = (await server._tools['get_post_all_locales'].handler({ id: 5 })) as any
    const parsed = JSON.parse(result.content[0].text)

    const expectedLocales = ['zh-CN', 'en-US', 'en-GB', 'en-AU', 'zh-TW', 'zh-HK', 'ja']
    expect(Object.keys(parsed).sort()).toEqual([...expectedLocales].sort())

    expect(parsed['en-AU']).toHaveProperty('__error')
    expect(parsed['en-AU'].__error).toContain('HTTP 404')

    for (const loc of expectedLocales.filter((l) => l !== 'en-AU')) {
      expect(parsed[loc]).toMatchObject({ id: 5, title: `Title in ${loc}` })
      expect(parsed[loc].__error).toBeUndefined()
    }
  })
})

describe('retranslate_post', () => {
  it('GETs the zh-CN source then PATCHes the same fields to fire afterChange', async () => {
    const cms = {
      request: vi
        .fn()
        .mockResolvedValueOnce({
          id: 9,
          title: 'Hi',
          slug: 'hi',
          content: { root: {} },
          excerpt: 'hi',
        })
        .mockResolvedValueOnce({ id: 9 }),
    } as unknown as CenterApiClient

    const server = createMockServer()
    registerRetranslatePost(server as any, cms)

    const result = (await server._tools['retranslate_post'].handler({ id: 9 })) as any
    const parsed = JSON.parse(result.content[0].text)

    expect(parsed).toMatchObject({ retranslated: true, id: 9 })

    const calls = (cms.request as any).mock.calls as Array<[string, RequestInit | undefined]>
    expect(calls).toHaveLength(2)

    // Call 1: GET source doc
    expect(calls[0][0]).toBe('/payload/api/posts/9?locale=zh-CN')
    expect(calls[0][1]).toBeUndefined()

    // Call 2: PATCH with the same fields
    expect(calls[1][0]).toBe('/payload/api/posts/9?locale=zh-CN')
    expect(calls[1][1]?.method).toBe('PATCH')
    const body = JSON.parse(calls[1][1]?.body as string)
    expect(body).toEqual({
      title: 'Hi',
      slug: 'hi',
      excerpt: 'hi',
      content: { root: {} },
    })
  })

  it('returns {error} content when the initial GET fails', async () => {
    const cms = {
      request: vi
        .fn()
        .mockRejectedValueOnce(
          new Error('GET https://cms.example.com/payload/api/posts/9 → HTTP 404: Not Found'),
        ),
    } as unknown as CenterApiClient

    const server = createMockServer()
    registerRetranslatePost(server as any, cms)

    const result = (await server._tools['retranslate_post'].handler({ id: 9 })) as any
    const parsed = JSON.parse(result.content[0].text)

    expect(parsed.error).toContain('HTTP 404')
    // Only one request issued — no PATCH after GET failure
    expect((cms.request as any).mock.calls).toHaveLength(1)
  })
})
