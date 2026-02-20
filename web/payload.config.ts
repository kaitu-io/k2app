import { buildConfig } from 'payload'
import { postgresAdapter } from '@payloadcms/db-postgres'
import { lexicalEditor } from '@payloadcms/richtext-lexical'
import { s3Storage } from '@payloadcms/storage-s3'
import sharp from 'sharp'
import path from 'path'
import { fileURLToPath } from 'url'

// Collections
import { Users } from './src/payload/collections/Users'
import { Media } from './src/payload/collections/Media'
import { Articles } from './src/payload/collections/Articles'

const filename = fileURLToPath(import.meta.url)
const dirname = path.dirname(filename)

export default buildConfig({
  // Custom routes - nested under /manager for backend management
  routes: {
    admin: '/manager/cms',      // Payload admin at /manager/cms
    api: '/manager/cms/api',    // Payload API at /manager/cms/api
  },
  admin: {
    user: Users.slug,
    importMap: {
      baseDir: path.resolve(dirname),
    },
  },
  collections: [Users, Media, Articles],
  editor: lexicalEditor(),
  secret: process.env.PAYLOAD_SECRET || 'your-secret-key-here-at-least-32-chars',
  typescript: {
    outputFile: path.resolve(dirname, 'src/payload/payload-types.ts'),
  },
  db: postgresAdapter({
    pool: {
      connectionString: process.env.DATABASE_URI || '',
    },
    // Auto-push schema changes (creates tables automatically)
    push: true,
  }),
  sharp,
  // Localization configuration matching the existing i18n setup
  localization: {
    locales: [
      { label: '简体中文', code: 'zh-CN' },
      { label: '繁體中文 (台灣)', code: 'zh-TW' },
      { label: '繁體中文 (香港)', code: 'zh-HK' },
      { label: 'English (US)', code: 'en-US' },
      { label: 'English (UK)', code: 'en-GB' },
      { label: 'English (AU)', code: 'en-AU' },
      { label: '日本語', code: 'ja' },
    ],
    defaultLocale: 'zh-CN',
    fallback: true,
  },
  plugins: [
    // S3/R2 storage for media uploads
    s3Storage({
      collections: {
        media: {
          prefix: 'media',
          generateFileURL: ({ filename }) => {
            const cdnBaseUrl = process.env.S3_CDN_URL || process.env.S3_ENDPOINT || ''
            return `${cdnBaseUrl}/media/${filename}`
          },
        },
      },
      bucket: process.env.S3_BUCKET || '',
      config: {
        // On Amplify: uses IAM role automatically (no credentials needed)
        // For R2/external S3: provide explicit credentials
        ...(process.env.S3_ACCESS_KEY_ID && process.env.S3_SECRET_ACCESS_KEY
          ? {
              credentials: {
                accessKeyId: process.env.S3_ACCESS_KEY_ID,
                secretAccessKey: process.env.S3_SECRET_ACCESS_KEY,
              },
            }
          : {}),
        region: process.env.S3_REGION || 'us-east-1',
        // Only set endpoint for non-AWS S3 (R2, MinIO, etc.)
        ...(process.env.S3_ENDPOINT ? { endpoint: process.env.S3_ENDPOINT } : {}),
        // forcePathStyle only needed for S3-compatible services, not native AWS
        ...(process.env.S3_ENDPOINT ? { forcePathStyle: true } : {}),
      },
    }),
  ],
})
