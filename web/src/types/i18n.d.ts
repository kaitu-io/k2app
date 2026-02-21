import { routing } from '@/i18n/routing';

declare global {
  // Messages are split across namespace JSON files (messages/{locale}/*.json)
  // loaded dynamically at runtime. Use permissive typing.
  // eslint-disable-next-line @typescript-eslint/no-empty-object-type
  interface IntlMessages {}
}

// Module augmentation for next-intl
declare module 'next-intl' {
  interface AppConfig {
    Locale: (typeof routing.locales)[number];
  }
}