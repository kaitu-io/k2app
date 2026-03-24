import { describe, it, expect, vi, beforeEach } from 'vitest'
import { CenterApiClient } from './center-api.ts'
import type { Config } from './config.ts'

const mockConfig: Config = {
  center: {
    url: 'https://api.example.com',
    accessKey: 'test-secret-key',
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

    const client = new CenterApiClient(mockConfig, mockFetch)
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

    const client = new CenterApiClient(mockConfig, mockFetch)
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

    const client = new CenterApiClient(mockConfig, mockFetch)
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

    const client = new CenterApiClient(mockConfig, mockFetch)
    await client.request('/api/resource', {
      method: 'POST',
      body: JSON.stringify({ name: 'test' }),
    })

    expect(capturedInit[0]?.method).toBe('POST')
    expect(capturedInit[0]?.body).toBe(JSON.stringify({ name: 'test' }))
  })
})
