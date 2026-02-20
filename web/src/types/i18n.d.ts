import { routing } from '@/i18n/routing';

type Messages = typeof import('../messages/en-US.json');

declare global {
  // Use type safe message keys with `next-intl`
  interface IntlMessages extends Messages {
    // Required to prevent empty interface error
    [key: string]: string | object;
  }
}

// Module augmentation for next-intl
declare module 'next-intl' {
  interface AppConfig {
    Locale: (typeof routing.locales)[number];
    Messages: Messages;
  }
}