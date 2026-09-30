import { getRequestConfig } from 'next-intl/server';
import { NAMESPACES } from '../../messages/namespaces';
import { DEFAULT_LOCALE, isLocale } from '@/lib/site';

export default getRequestConfig(async ({ requestLocale }) => {
  const requested = await requestLocale;
  const locale = isLocale(requested) ? requested : DEFAULT_LOCALE;

  // Each file is stored under its namespace: messages/<locale>/auth.json
  // → t('auth.xxx') with useTranslations(), or useTranslations('auth') → t('xxx').
  // Every locale must carry every namespace (tests/messages-parity.test.ts);
  // there is deliberately no fallback, so a missing file fails the build.
  const messages: Record<string, unknown> = {};
  await Promise.all(
    NAMESPACES.map(async (ns) => {
      messages[ns] = (await import(`../../messages/${locale}/${ns}.json`)).default;
    }),
  );

  return { locale, messages };
});
