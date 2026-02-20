import type { CollectionConfig } from 'payload'

export const Media: CollectionConfig = {
  slug: 'media',
  admin: {
    useAsTitle: 'filename',
    description: 'Media files (images, documents, etc.)',
    group: 'Content',
  },
  access: {
    read: () => true, // Public read access for media
    create: ({ req: { user } }) => Boolean(user),
    update: ({ req: { user } }) => Boolean(user),
    delete: ({ req: { user } }) => user?.role === 'admin',
  },
  upload: {
    // Disable local storage - we use S3/R2
    disableLocalStorage: true,
    // Image resizing options
    imageSizes: [
      {
        name: 'thumbnail',
        width: 150,
        height: 150,
        position: 'centre',
      },
      {
        name: 'card',
        width: 480,
        height: 320,
        position: 'centre',
      },
      {
        name: 'hero',
        width: 1200,
        height: 630,
        position: 'centre',
      },
    ],
    mimeTypes: ['image/*', 'application/pdf'],
    adminThumbnail: 'thumbnail',
  },
  fields: [
    {
      name: 'alt',
      type: 'text',
      label: 'Alt Text',
      required: true,
      localized: true,
    },
    {
      name: 'caption',
      type: 'textarea',
      label: 'Caption',
      localized: true,
    },
  ],
}
