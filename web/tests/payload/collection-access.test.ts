import { describe, it, expect, vi } from 'vitest'

// @payload-enchants/translator has an ESM dir-import bug that breaks in
// raw Node. `Posts` imports autoTranslate → imports translator, so we stub it.
vi.mock('@payload-enchants/translator', () => ({ translateOperation: vi.fn() }))

import { Posts } from '@/payload/collections/Posts'
import { Categories } from '@/payload/collections/Categories'
import { Tags } from '@/payload/collections/Tags'
import { isAdmin } from '@/payload/access/isAdmin'

// Regression guard: read must be isAdmin so the Payload admin UI can list docs.
// `() => false` silently hides the collection from the sidebar (admin-invisible).
describe('collection access.read', () => {
  const withUser = { req: { user: { id: 1, collection: 'admins' } } } as never
  const noUser = { req: { user: null } } as never

  it.each([
    ['posts', Posts],
    ['categories', Categories],
    ['tags', Tags],
  ])('%s: read is isAdmin — admin sees, anon blocked', (_slug, collection) => {
    const read = collection.access?.read
    expect(read).toBe(isAdmin)
    expect(read?.(withUser)).toBe(true)
    expect(read?.(noUser)).toBe(false)
  })
})
