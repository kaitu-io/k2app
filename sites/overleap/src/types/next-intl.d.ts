// Typed messages: en-GB is the master, so a key that does not exist there is a
// compile error instead of a raw key rendered in production.
import type common from '../../messages/en-GB/common.json';
import type nav from '../../messages/en-GB/nav.json';
import type home from '../../messages/en-GB/home.json';
import type auth from '../../messages/en-GB/auth.json';
import type account from '../../messages/en-GB/account.json';
import type errors from '../../messages/en-GB/errors.json';
import type legal from '../../messages/en-GB/legal.json';
import type { Locale } from '@/lib/site';

declare module 'next-intl' {
  interface AppConfig {
    Locale: Locale;
    Messages: {
      common: typeof common;
      nav: typeof nav;
      home: typeof home;
      auth: typeof auth;
      account: typeof account;
      errors: typeof errors;
      legal: typeof legal;
    };
  }
}
