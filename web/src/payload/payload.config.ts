import { postgresAdapter } from '@payloadcms/db-postgres'
import { lexicalEditor } from '@payloadcms/richtext-lexical'
import { translator, copyResolver, openAIResolver } from '@payload-enchants/translator'
import { s3Storage } from '@payloadcms/storage-s3'
import { buildTranslationPrompt } from './translator/customPrompt.ts'
import path from 'path'
import { buildConfig } from 'payload'
import { fileURLToPath } from 'url'
import sharp from 'sharp'

import { Admins } from './collections/Admins.ts'
import { Categories } from './collections/Categories.ts'
import { Tags } from './collections/Tags.ts'
import { Media } from './collections/Media.ts'
import { Posts } from './collections/Posts.ts'

const filename = fileURLToPath(import.meta.url)
const dirname = path.dirname(filename)

export default buildConfig({
  admin: {
    user: 'admins',
    importMap: { baseDir: path.resolve(dirname) },
    components: {
      beforeNavLinks: [
        { path: '@/payload/components/BackToManager', exportName: 'BackToManager' },
      ],
    },
  },
  routes: {
    admin: '/manager/cms',
    api: '/payload/api',
  },
  collections: [Admins, Categories, Tags, Media, Posts],
  localization: {
    locales: [
      { code: 'zh-CN', label: '简体中文' },
      { code: 'en-US', label: 'English (US)' },
      { code: 'en-GB', label: 'English (UK)' },
      { code: 'en-AU', label: 'English (AU)' },
      { code: 'zh-TW', label: '繁體中文 (台灣)' },
      { code: 'zh-HK', label: '繁體中文 (香港)' },
      { code: 'ja', label: '日本語' },
    ],
    defaultLocale: 'zh-CN',
    fallback: true,
  },
  editor: lexicalEditor(),
  secret: process.env.PAYLOAD_SECRET || '',
  typescript: {
    outputFile: path.resolve(dirname, 'payload-types.ts'),
  },
  db: postgresAdapter({
    pool: { connectionString: process.env.DATABASE_URL || '' },
    push: false,  // Use explicit migrations (src/migrations/) — push:true does not auto-sync at runtime in serverless
  }),
  sharp,
  plugins: [
    s3Storage({
      collections: {
        media: {
          prefix: 'media',
          generateFileURL: ({ filename, prefix }) => {
            const cdn = (process.env.CDN_URL || 'https://media.kaitu.io').replace(/\/$/, '')
            return prefix ? `${cdn}/${prefix}/${filename}` : `${cdn}/${filename}`
          },
        },
      },
      bucket: process.env.S3_BUCKET || 'kaitu-cms-media',
      config: {
        region: process.env.S3_REGION || 'ap-northeast-1',
        credentials: {
          accessKeyId: process.env.S3_ACCESS_KEY_ID || '',
          secretAccessKey: process.env.S3_SECRET_ACCESS_KEY || '',
        },
      },
    }),
    translator({
      collections: ['posts', 'categories', 'tags'],
      globals: [],
      resolvers: [
        openAIResolver({
          apiKey: process.env.TRANSLATOR_API_KEY || '',
          baseUrl: process.env.TRANSLATOR_BASE_URL || 'https://openrouter.ai/api',
          model: process.env.TRANSLATOR_MODEL || 'google/gemini-2.5-flash',
          prompt: buildTranslationPrompt,
          chunkLength: 50,
        }),
        copyResolver(),
      ],
    }),
  ],
})
