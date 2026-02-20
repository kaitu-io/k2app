/* THIS FILE WAS GENERATED AUTOMATICALLY BY PAYLOAD. */
/* DO NOT MODIFY IT BECAUSE IT COULD BE REWRITTEN AT ANY TIME. */
import '@payloadcms/next/css'
import { NotFoundPage, generatePageMetadata } from '@payloadcms/next/views'
import { importMap } from '../importMap'

import configPromise from '../../../../../../payload.config'

export const generateMetadata = async () =>
  generatePageMetadata({ config: configPromise, params: Promise.resolve({ segments: ['not-found'] }), searchParams: Promise.resolve({}) })

const NotFound = async () =>
  NotFoundPage({
    config: configPromise,
    importMap,
    params: Promise.resolve({ segments: ['not-found'] }),
    searchParams: Promise.resolve({})
  })

export default NotFound
