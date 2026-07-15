import { brandConfig } from '../brand';

const COMMON_FAQ_KEYS = [
  'connection',
  'appNotWorking',
  'nodeChoice',
  'wifiSwitch',
  'deviceRemoved',
  'updateIssue',
  'loginFailed',
] as const;

// Kaitu-only stories (legacy ANC client, China App Store availability).
// Their locale strings live in src/i18n/brand/kaitu/<lang>/ticket.json —
// they are not bundled into overleap builds at all.
const KAITU_FAQ_KEYS = ['allNationConnect', 'chinaAppStore'] as const;

export type FaqKey =
  | (typeof COMMON_FAQ_KEYS)[number]
  | (typeof KAITU_FAQ_KEYS)[number];

export const FAQ_KEYS: readonly FaqKey[] =
  brandConfig.id === 'kaitu' ? [...COMMON_FAQ_KEYS, ...KAITU_FAQ_KEYS] : [...COMMON_FAQ_KEYS];
