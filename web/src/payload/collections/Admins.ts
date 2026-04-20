import type { CollectionConfig } from 'payload'
import { centerAuthStrategy } from '../auth/centerAuthStrategy.ts'

export const Admins: CollectionConfig = {
  slug: 'admins',
  auth: {
    disableLocalStrategy: true,
    strategies: [centerAuthStrategy],
  },
  access: {
    read: ({ req }) => Boolean(req.user),
    create: () => false,
    update: () => false,
    delete: () => false,
  },
  fields: [
    { name: 'email', type: 'text' },
    { name: 'centerId', type: 'text', index: true, unique: true },
  ],
}
