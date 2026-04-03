import emailSpellChecker from '@zootools/email-spell-checker';

// Chinese email domains not in the library's built-in 37 domains
const EXTRA_DOMAINS = [
  'qq.com',
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

/**
 * Check for likely email typos. Returns suggested correction or null.
 * Pre-processes common patterns (spaces, number-letter swaps) that
 * the Sift3 algorithm might miss.
 */
export function suggestEmail(email: string): string | null {
  if (!email || !email.includes('@')) return null;

  // Pre-process: remove spaces (catches "qq.co m" → "qq.com")
  const cleaned = email.replace(/\s+/g, '');

  // Pre-process: fix common TLD typos before running spell checker
  // These are hard substitutions the distance algorithm may not catch
  const preFixed = cleaned
    .replace(/@(.+)\.c0m$/i, '@$1.com')
    .replace(/@(.+)\.mcon$/i, '@$1.com')
    .replace(/@(.+)\.con$/i, '@$1.com')
    .replace(/@wq\.com$/i, '@qq.com');

  // If pre-processing changed the email, return the fixed version
  // (compare against original email to catch both space removal + pattern fixes)
  if (preFixed !== email) {
    return preFixed;
  }

  // Run the spell checker (returns undefined when no suggestion)
  const result = emailSpellChecker.run({
    email: cleaned,
    domains: ALL_DOMAINS,
  });

  if (result && result.full !== cleaned) {
    return result.full;
  }

  return null;
}
