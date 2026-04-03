// Keep in sync with webapp/src/utils/email-suggest.ts
import emailSpellChecker from '@zootools/email-spell-checker';

// Chinese email domains not in the library's built-in POPULAR_DOMAINS
// Note: qq.com is already in POPULAR_DOMAINS, no need to duplicate
const EXTRA_DOMAINS = [
  '163.com',
  '126.com',
  'sina.com',
  'sohu.com',
  'foxmail.com',
  'aliyun.com',
  '139.com',
  'yeah.net',
  'vip.qq.com',
];

// IMPORTANT: domains option REPLACES built-ins, so we must spread POPULAR_DOMAINS
const ALL_DOMAINS = [...emailSpellChecker.POPULAR_DOMAINS, ...EXTRA_DOMAINS];

export function suggestEmail(email: string): string | null {
  if (!email || !email.includes('@')) return null;

  const cleaned = email.replace(/\s+/g, '');

  const preFixed = cleaned
    .replace(/@(.+)\.c0m$/i, '@$1.com')
    .replace(/@(.+)\.mcon$/i, '@$1.com')
    .replace(/@(.+)\.con$/i, '@$1.com')
    .replace(/@wq\.com$/i, '@qq.com');

  if (preFixed !== email) {
    return preFixed;
  }

  const result = emailSpellChecker.run({
    email: cleaned,
    domains: ALL_DOMAINS,
  });

  if (result && result.full !== cleaned) {
    return result.full;
  }

  return null;
}
