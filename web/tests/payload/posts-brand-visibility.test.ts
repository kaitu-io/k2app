import { describe, it, expect, vi } from 'vitest'

// Translator has an ESM dir-import bug; stub before importing Posts.
vi.mock('@payload-enchants/translator', () => ({ translateOperation: vi.fn() }))

import { Posts } from '@/payload/collections/Posts'

describe('Posts brand visibility fields', () => {
  const field = (name: string) =>
    (Posts.fields as Array<{ name?: string }>).find(f => f.name === name)

  it('has showOnKaitu checkbox with default true', () => {
    const f = field('showOnKaitu') as any
    expect(f).toBeDefined()
    expect(f.type).toBe('checkbox')
    expect(f.defaultValue).toBe(true)
    expect(f.required).toBe(true)
    expect(f.custom?.translatorSkip).toBe(true)
  })

  it('has showOnOverleap checkbox with default true', () => {
    const f = field('showOnOverleap') as any
    expect(f).toBeDefined()
    expect(f.type).toBe('checkbox')
    expect(f.defaultValue).toBe(true)
    expect(f.required).toBe(true)
    expect(f.custom?.translatorSkip).toBe(true)
  })
})
