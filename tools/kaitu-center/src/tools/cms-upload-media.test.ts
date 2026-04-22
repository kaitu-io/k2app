import { describe, it, expect, vi, beforeAll, afterAll } from 'vitest'
import * as fs from 'node:fs'
import * as path from 'node:path'
import * as os from 'node:os'
import { registerUploadMedia } from './cms-upload-media.js'
import type { CenterApiClient } from '../center-api.js'

vi.mock('../audit.js', () => ({
  audit: vi.fn().mockResolvedValue(undefined),
}))

class FakeServer {
  private handlers = new Map<string, (p: Record<string, unknown>) => Promise<unknown>>()
  tool(name: string, _d: string, _p: unknown, h: (p: Record<string, unknown>) => Promise<unknown>) {
    this.handlers.set(name, h)
  }
  invoke(name: string, params: Record<string, unknown>) {
    const handler = this.handlers.get(name)
    if (!handler) throw new Error(`no handler: ${name}`)
    return handler(params)
  }
}

describe('upload_media', () => {
  const tmpFile = path.join(os.tmpdir(), `kaitu-cms-upload-test-${process.pid}.png`)
  beforeAll(() => {
    fs.writeFileSync(tmpFile, Buffer.from([0x89, 0x50, 0x4E, 0x47]))
  })
  afterAll(() => {
    try { fs.unlinkSync(tmpFile) } catch { /* ignore */ }
  })

  it('posts multipart body with file + alt to /payload/api/media', async () => {
    const requestMock = vi.fn().mockResolvedValue({
      doc: { id: 99, url: 'https://media.kaitu.io/test.png', filename: 'test.png' },
      message: 'Media created',
    })
    const cms = { request: requestMock } as unknown as CenterApiClient

    const server = new FakeServer()
    registerUploadMedia(server as unknown as Parameters<typeof registerUploadMedia>[0], cms)
    const result = await server.invoke('upload_media', { file_path: tmpFile, alt: 'Test' }) as {
      content: Array<{ text: string }>
    }

    expect(requestMock).toHaveBeenCalledOnce()
    const [callPath, opts] = requestMock.mock.calls[0] as [string, RequestInit & { headers: Record<string, string> }]
    expect(callPath).toContain('/payload/api/media')
    expect(opts.method).toBe('POST')
    expect(opts.body).toBeInstanceOf(FormData)
    expect(opts.headers['Content-Type']).toBe('')

    const parsed = JSON.parse(result.content[0].text) as { doc: { id: number; url: string } }
    expect(parsed.doc).toMatchObject({ id: 99, url: expect.stringContaining('test.png') })
  })

  it('returns error when file does not exist', async () => {
    const cms = { request: vi.fn() } as unknown as CenterApiClient
    const server = new FakeServer()
    registerUploadMedia(server as unknown as Parameters<typeof registerUploadMedia>[0], cms)
    const result = await server.invoke('upload_media', { file_path: '/nonexistent/file.png' }) as {
      content: Array<{ text: string }>
    }
    const parsed = JSON.parse(result.content[0].text) as { error: string }
    expect(parsed.error).toMatch(/ENOENT|no such file/i)
    expect(cms.request).not.toHaveBeenCalled()
  })

  it('surfaces upstream HTTP errors as error text', async () => {
    const requestMock = vi.fn().mockRejectedValue(
      new Error('POST https://kaitu.io/payload/api/media → HTTP 413: Payload Too Large')
    )
    const cms = { request: requestMock } as unknown as CenterApiClient

    const server = new FakeServer()
    registerUploadMedia(server as unknown as Parameters<typeof registerUploadMedia>[0], cms)
    const result = await server.invoke('upload_media', { file_path: tmpFile }) as {
      content: Array<{ text: string }>
    }
    const parsed = JSON.parse(result.content[0].text) as { error: string }
    expect(parsed.error).toContain('HTTP 413')
  })
})
