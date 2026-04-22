import type { CollectionConfig } from 'payload'
import { lexicalEditor } from '@payloadcms/richtext-lexical'
import { slugField } from '../fields/slugField.ts'
import { isAdmin } from '../access/isAdmin.ts'
import { setAuthorFromRequest } from '../hooks/setAuthorFromRequest.ts'
import { setPublishedAt } from '../hooks/setPublishedAt.ts'
import { autoTranslate } from '../hooks/autoTranslate.ts'

export const Posts: CollectionConfig = {
  slug: 'posts',
  admin: {
    useAsTitle: 'title',
    defaultColumns: ['title', 'status', 'publishedAt', 'updatedAt'],
  },
  access: {
    read: isAdmin,
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
      name: 'showOnKaitu',
      type: 'checkbox',
      defaultValue: true,
      required: true,
      custom: { translatorSkip: true },
      admin: { description: '显示在 Kaitu 品牌站（kaitu.io）' },
    },
    {
      name: 'showOnOverleap',
      type: 'checkbox',
      defaultValue: true,
      required: true,
      custom: { translatorSkip: true },
      admin: { description: '显示在 Overleap 品牌站（overleap.*）' },
      validate: (value: unknown, { siblingData }: { siblingData: Record<string, unknown> }) => {
        const overleap = Boolean(value)
        const kaitu = Boolean(siblingData?.showOnKaitu)
        const published = siblingData?.status === 'published'
        if (published && !overleap && !kaitu) {
          return '发布状态下至少需要勾选一个品牌站点；如要全部隐藏请改为 draft'
        }
        return true
      },
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
    afterChange: [autoTranslate],
  },
}
