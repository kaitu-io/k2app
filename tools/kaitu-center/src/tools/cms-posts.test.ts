import { describe, it, expect, vi } from 'vitest'
import { cmsPostsTools } from './cms-posts.js'
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

function makeCmsClient(response: unknown) {
  return {
    request: vi.fn().mockResolvedValue(response),
  } as unknown as CenterApiClient
}

function makeRejectingCmsClient(err: Error) {
  return {
    request: vi.fn().mockRejectedValue(err),
  } as unknown as CenterApiClient
}

/** Wrap a cms mock as the full ApiClients; center should never be called. */
function clientsWithCms(cms: CenterApiClient) {
  return {
    center: { request: vi.fn() } as unknown as CenterApiClient,
    cms,
  }
}

describe('cms-posts tools', () => {
  it('exports seven tools with expected names', () => {
    const names = cmsPostsTools.map((t) => t.name).sort()
    expect(names).toEqual([
      'create_post',
      'delete_post',
      'get_post',
      'list_posts',
      'publish_post',
      'unpublish_post',
      'update_post',
    ])
    for (const t of cmsPostsTools) {
      expect(t.group).toBe('cms')
    }
  })

  it('list_posts passes locale + status filters as where[…]', async () => {
    const cms = makeCmsClient({ docs: [], totalDocs: 0 })
    const tool = cmsPostsTools.find((t) => t.name === 'list_posts')!
    const server = createMockServer()
    tool.register(server as any, clientsWithCms(cms))

    await server._tools['list_posts'].handler({ locale: 'zh-CN', status: 'published' })

    const call = (cms.request as any).mock.calls[0][0] as string
    expect(call).toContain('/payload/api/posts')
    expect(call).toContain('locale=zh-CN')
    expect(call).toContain('where%5Bstatus%5D%5Bequals%5D=published')
  })

  it('create_post POSTs with locale=zh-CN and body fields', async () => {
    const cms = makeCmsClient({ id: 123, title: 'Hello' })
    const tool = cmsPostsTools.find((t) => t.name === 'create_post')!
    const server = createMockServer()
    tool.register(server as any, clientsWithCms(cms))

    await server._tools['create_post'].handler({
      title: 'Hello',
      slug: 'hello',
      content: {},
      showOnKaitu: true,
      showOnOverleap: false,
    })

    const [path, opts] = (cms.request as any).mock.calls[0] as [string, RequestInit]
    expect(path).toBe('/payload/api/posts?locale=zh-CN')
    expect(opts.method).toBe('POST')
    const body = JSON.parse(opts.body as string)
    expect(body).toMatchObject({
      title: 'Hello',
      slug: 'hello',
      showOnKaitu: true,
      showOnOverleap: false,
    })
  })

  it('publish_post PATCHes status=published to :id', async () => {
    const cms = makeCmsClient({ id: 42, status: 'published' })
    const tool = cmsPostsTools.find((t) => t.name === 'publish_post')!
    const server = createMockServer()
    tool.register(server as any, clientsWithCms(cms))

    await server._tools['publish_post'].handler({ id: 42 })

    const [path, opts] = (cms.request as any).mock.calls[0] as [string, RequestInit]
    expect(path).toBe('/payload/api/posts/42')
    expect(opts.method).toBe('PATCH')
    expect(JSON.parse(opts.body as string)).toEqual({ status: 'published' })
  })

  it('list_posts surfaces upstream HTTP errors via thrown Error', async () => {
    const cms = makeRejectingCmsClient(
      new Error('GET https://cms.example.com/payload/api/posts → HTTP 403: forbidden'),
    )
    const tool = cmsPostsTools.find((t) => t.name === 'list_posts')!
    const server = createMockServer()
    tool.register(server as any, clientsWithCms(cms))

    const result = (await server._tools['list_posts'].handler({})) as any
    const parsed = JSON.parse(result.content[0].text)
    expect(parsed.error).toContain('HTTP 403')
    expect(parsed.error).toContain('forbidden')
  })
})
