import { describe, it, expect, vi, beforeEach } from 'vitest'
import { CenterApiClient, createCenterClient, createCmsClient } from './center-api.ts'
import type { Config } from './config.ts'

const mockConfig: Config = {
  center: {
    url: 'https://api.example.com',
    accessKey: 'test-secret-key',
  },
  cms: {
    url: 'http://localhost:3000',
  },
  ssh: {
    privateKeyPath: '/home/user/.ssh/id_rsa',
    user: 'root',
    port: 22,
  },
}

describe('CenterApiClient', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('test_center_api_auth_header — sends X-Access-Key header with configured access_key', async () => {
    const capturedInit: RequestInit[] = []
    const mockFetch = vi.fn((_url: string | URL | Request, init?: RequestInit) => {
      capturedInit.push(init ?? {})
      return Promise.resolve(
        new Response(JSON.stringify({ code: 0, data: { ok: true } }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      )
    })

    const client = new CenterApiClient(mockConfig.center.url, mockConfig.center.accessKey, mockFetch)
    await client.request('/api/test')

    expect(capturedInit).toHaveLength(1)
    const headers = capturedInit[0]?.headers as Record<string, string>
    expect(headers['X-Access-Key']).toBe('test-secret-key')
  })

  it('test_center_api_request_url — constructs correct URL from config base URL + path', async () => {
    const capturedUrls: string[] = []
    const mockFetch = vi.fn((url: string | URL | Request, _init?: RequestInit) => {
      capturedUrls.push(typeof url === 'string' ? url : url.toString())
      return Promise.resolve(
        new Response(JSON.stringify({ code: 0, data: {} }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      )
    })

    const client = new CenterApiClient(mockConfig.center.url, mockConfig.center.accessKey, mockFetch)
    await client.request('/api/users')

    expect(capturedUrls).toHaveLength(1)
    expect(capturedUrls[0]).toBe('https://api.example.com/api/users')
  })

  it('test_center_api_request_url — works with paths that do not start with slash', async () => {
    const capturedUrls: string[] = []
    const mockFetch = vi.fn((url: string | URL | Request, _init?: RequestInit) => {
      capturedUrls.push(typeof url === 'string' ? url : url.toString())
      return Promise.resolve(
        new Response(JSON.stringify({ code: 0, data: {} }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      )
    })

    const client = new CenterApiClient(mockConfig.center.url, mockConfig.center.accessKey, mockFetch)
    await client.request('api/nodes')

    expect(capturedUrls[0]).toBe('https://api.example.com/api/nodes')
  })

  it('test_center_api_request_url — passes through extra fetch options', async () => {
    const capturedInit: RequestInit[] = []
    const mockFetch = vi.fn((_url: string | URL | Request, init?: RequestInit) => {
      capturedInit.push(init ?? {})
      return Promise.resolve(
        new Response(JSON.stringify({ code: 0 }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        })
      )
    })

    const client = new CenterApiClient(mockConfig.center.url, mockConfig.center.accessKey, mockFetch)
    await client.request('/api/resource', {
      method: 'POST',
      body: JSON.stringify({ name: 'test' }),
    })

    expect(capturedInit[0]?.method).toBe('POST')
    expect(capturedInit[0]?.body).toBe(JSON.stringify({ name: 'test' }))
  })
})

describe('CenterApiClient — multiple targets', () => {
  const testConfig = (): Config => ({
    center: { url: 'https://center.test', accessKey: 'ktu_x' },
    cms: { url: 'https://kaitu.test' },
    ssh: { privateKeyPath: '/x', user: 'u', port: 22 },
  })

  it('creates a CMS client pointing at cms.url with same access key', async () => {
    const fetchFn = vi.fn().mockResolvedValue({
      ok: true, json: async () => ({ docs: [{ id: 1 }] }),
    })
    const client = createCmsClient(testConfig(), fetchFn)
    await client.request('/payload/api/posts')
    expect(fetchFn).toHaveBeenCalledWith(
      'https://kaitu.test/payload/api/posts',
      expect.objectContaining({
        headers: expect.objectContaining({ 'X-Access-Key': 'ktu_x' }),
      }),
    )
  })

  it('throws with Payload error body preserved on HTTP 4xx', async () => {
    const fetchFn = vi.fn().mockResolvedValue({
      ok: false,
      status: 400,
      json: async () => ({ errors: [{ message: 'The following field is invalid: title' }] }),
    })
    const client = createCmsClient(testConfig(), fetchFn)
    await expect(
      client.request('/payload/api/posts', { method: 'POST', body: '{}' })
    ).rejects.toThrow(/HTTP 400.*following field is invalid: title/)
  })

  it('throws with plain HTTP status when body is not JSON', async () => {
    const fetchFn = vi.fn().mockResolvedValue({
      ok: false,
      status: 502,
      json: async () => { throw new Error('not json') },
    })
    const client = createCmsClient(testConfig(), fetchFn)
    await expect(
      client.request('/payload/api/posts')
    ).rejects.toThrow(/HTTP 502/)
  })

  it('preserves Center {message} on HTTP 4xx (non-envelope error)', async () => {
    const fetchFn = vi.fn().mockResolvedValue({
      ok: false,
      status: 401,
      json: async () => ({ message: 'invalid access key' }),
    })
    const client = createCenterClient(testConfig(), fetchFn)
    await expect(
      client.request('/app/my-permissions')
    ).rejects.toThrow(/HTTP 401.*invalid access key/)
  })
})
