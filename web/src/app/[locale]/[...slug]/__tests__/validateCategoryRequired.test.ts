import { describe, it, expect, vi } from 'vitest'

// Mock heavy Payload deps that are not needed to test the pure validate function.
// @payload-enchants/translator has a directory-import bug that crashes under vitest's ESM loader.
vi.mock('@payload-enchants/translator', () => ({ translateOperation: vi.fn() }))
vi.mock('payload', () => ({ default: {} }))
vi.mock('@payload-config', () => ({ default: {} }))

import { validateCategoryRequired } from '@/payload/collections/Posts'

describe('validateCategoryRequired', () => {
  it('returns true when status is draft and category is null', () => {
    expect(validateCategoryRequired(null, { siblingData: { status: 'draft' } })).toBe(true)
  })

  it('returns true when status is draft and category is set', () => {
    expect(
      validateCategoryRequired('cat-id', { siblingData: { status: 'draft' } }),
    ).toBe(true)
  })

  it('returns true when status is published and category is set', () => {
    expect(
      validateCategoryRequired('cat-id', { siblingData: { status: 'published' } }),
    ).toBe(true)
  })

  it('returns an error string when status is published and category is null', () => {
    const result = validateCategoryRequired(null, { siblingData: { status: 'published' } })
    expect(typeof result).toBe('string')
    expect(result).toMatch(/category/i)
  })

  it('returns an error string when status is published and category is undefined', () => {
    const result = validateCategoryRequired(undefined, { siblingData: { status: 'published' } })
    expect(typeof result).toBe('string')
  })

  it('returns an error string when status is published and category is empty string', () => {
    const result = validateCategoryRequired('', { siblingData: { status: 'published' } })
    expect(typeof result).toBe('string')
  })
})
