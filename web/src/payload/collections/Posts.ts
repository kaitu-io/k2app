import type { CollectionConfig } from 'payload'
import { lexicalEditor } from '@payloadcms/richtext-lexical'
import { slugField } from '../fields/slugField.ts'
import { isAdmin } from '../access/isAdmin.ts'
import { setAuthorFromRequest } from '../hooks/setAuthorFromRequest.ts'
import { setPublishedAt } from '../hooks/setPublishedAt.ts'

export const Posts: CollectionConfig = {
  slug: 'posts',
  admin: {
    useAsTitle: 'title',
    defaultColumns: ['title', 'status', 'publishedAt', 'updatedAt'],
  },
  access: {
    read: () => false,
    create: isAdmin,
    update: isAdmin,
    delete: isAdmin,
  },
  versions: { drafts: true },
  fields: [
    slugField(),
    { name: 'title', type: 'text', required: true, localized: true },
    { name: 'excerpt', type: 'textarea', localized: true },
    {
      name: 'content',
      type: 'richText',
      editor: lexicalEditor(),
      required: true,
      localized: true,
    },
    {
      name: 'coverImage',
      type: 'upload',
      relationTo: 'media',
      custom: { translatorSkip: true },
    },
    {
      name: 'category',
      type: 'relationship',
      relationTo: 'categories',
      custom: { translatorSkip: true },
    },
    {
      name: 'tags',
      type: 'relationship',
      relationTo: 'tags',
      hasMany: true,
      custom: { translatorSkip: true },
    },
    {
      name: 'status',
      type: 'select',
      options: ['draft', 'published'],
      defaultValue: 'draft',
      custom: { translatorSkip: true },
    },
    {
      name: 'publishedAt',
      type: 'date',
      custom: { translatorSkip: true },
    },
    {
      name: 'author',
      type: 'relationship',
      relationTo: 'admins',
      admin: { readOnly: true },
      custom: { translatorSkip: true },
    },
  ],
  hooks: {
    beforeChange: [setAuthorFromRequest, setPublishedAt],
    // afterChange wired in Task 9
  },
}
