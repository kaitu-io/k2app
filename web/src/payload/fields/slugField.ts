import type { Field } from 'payload'

export const slugField = (): Field => ({
  name: 'slug',
  type: 'text',
  required: true,
  unique: true,
  index: true,
  custom: { translatorSkip: true },
  admin: {
    description: 'URL-safe identifier, e.g. "getting-started"',
  },
  validate: (value: unknown) => {
    if (typeof value !== 'string') return 'Slug must be a string'
    if (!/^[a-z0-9-]+$/.test(value)) return 'Only lowercase letters, digits, and dashes'
    return true
  },
})
