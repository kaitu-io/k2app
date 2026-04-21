import type { CollectionConfig } from 'payload'
import { isAdmin } from '../access/isAdmin.ts'

export const Media: CollectionConfig = {
  slug: 'media',
  upload: {
    mimeTypes: ['image/*'],
  },
  access: {
    read: ({ req }) => Boolean(req.user),
    create: isAdmin,
    update: isAdmin,
    delete: isAdmin,
  },
  fields: [
    { name: 'alt', type: 'text', localized: true },
  ],
}
