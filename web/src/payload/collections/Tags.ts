import type { CollectionConfig } from 'payload'
import { slugField } from '../fields/slugField.ts'
import { isAdmin } from '../access/isAdmin.ts'

export const Tags: CollectionConfig = {
  slug: 'tags',
  admin: { useAsTitle: 'name' },
  access: {
    read: isAdmin,
    create: isAdmin,
    update: isAdmin,
    delete: isAdmin,
  },
  fields: [
    slugField(),
    { name: 'name', type: 'text', required: true, localized: true },
  ],
}
