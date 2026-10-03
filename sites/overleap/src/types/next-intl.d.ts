// Typed messages: en-GB is the master, so a key that does not exist there is a
// compile error instead of a raw key rendered in production.
import type common from '../../messages/en-GB/common.json';
import type nav from '../../messages/en-GB/nav.json';
import type landing from '../../messages/en-GB/landing.json';
import type download from '../../messages/en-GB/download.json';
import type help from '../../messages/en-GB/help.json';
import type pricing from '../../messages/en-GB/pricing.json';
import type purchase from '../../messages/en-GB/purchase.json';
import type auth from '../../messages/en-GB/auth.json';
import type account from '../../messages/en-GB/account.json';
import type errors from '../../messages/en-GB/errors.json';
import type legal from '../../messages/en-GB/legal.json';
import type chat from '../../messages/en-GB/chat.json';
import type { Locale } from '@/lib/site';

declare module 'next-intl' {
  interface AppConfig {
    Locale: Locale;
    Messages: {
      common: typeof common;
      nav: typeof nav;
      landing: typeof landing;
      download: typeof download;
      help: typeof help;
      pricing: typeof pricing;
      purchase: typeof purchase;
      auth: typeof auth;
      account: typeof account;
      errors: typeof errors;
      legal: typeof legal;
      chat: typeof chat;
    };
  }
}
