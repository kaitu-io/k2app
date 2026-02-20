import type { CollectionConfig } from 'payload'
import { kaituAuthStrategy } from '../auth/kaitu-auth-strategy'

export const Users: CollectionConfig = {
  slug: 'users',
  admin: {
    useAsTitle: 'email',
    description: 'CMS administrators and editors',
  },
  auth: {
    // 使用 Kaitu 自定义认证策略
    strategies: [kaituAuthStrategy],
    // 本地策略默认启用，用于 API 创建用户
  },
  fields: [
    {
      name: 'name',
      type: 'text',
      label: 'Name',
    },
    {
      name: 'kaituUserId',
      type: 'text',
      label: 'Kaitu User ID',
      unique: true,
      index: true,
      admin: {
        description: '关联的 Kaitu 系统用户 ID',
        readOnly: true,
      },
    },
    {
      name: 'role',
      type: 'select',
      options: [
        { label: 'Admin', value: 'admin' },
        { label: 'Editor', value: 'editor' },
      ],
      defaultValue: 'editor',
      required: true,
    },
  ],
  access: {
    // Only admins can create new users
    create: ({ req: { user } }) => {
      return user?.role === 'admin'
    },
    // Users can read their own profile, admins can read all
    read: ({ req: { user } }) => {
      if (user?.role === 'admin') return true
      return {
        id: {
          equals: user?.id,
        },
      }
    },
    // Users can update their own profile, admins can update all
    update: ({ req: { user } }) => {
      if (user?.role === 'admin') return true
      return {
        id: {
          equals: user?.id,
        },
      }
    },
    // Only admins can delete users
    delete: ({ req: { user } }) => {
      return user?.role === 'admin'
    },
  },
}
