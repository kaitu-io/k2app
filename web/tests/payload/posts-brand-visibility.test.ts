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

describe('Posts brand visibility validate', () => {
  const getValidate = () => {
    const f = (Posts.fields as Array<{ name?: string; validate?: any }>)
      .find(f => f.name === 'showOnOverleap')
    return f?.validate
  }

  it('rejects published when both visibility bools are false', () => {
    const validate = getValidate()!
    const result = validate(false, { siblingData: { status: 'published', showOnKaitu: false } })
    expect(result).toMatch(/至少/)
  })

  it('accepts published when at least one is true', () => {
    const validate = getValidate()!
    expect(validate(true, { siblingData: { status: 'published', showOnKaitu: false } })).toBe(true)
    expect(validate(false, { siblingData: { status: 'published', showOnKaitu: true } })).toBe(true)
  })

  it('accepts draft with both false', () => {
    const validate = getValidate()!
    expect(validate(false, { siblingData: { status: 'draft', showOnKaitu: false } })).toBe(true)
  })
})
