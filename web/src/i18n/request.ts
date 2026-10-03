import {getRequestConfig} from 'next-intl/server';
import {hasLocale} from 'next-intl';
import {routing} from './routing';
import {namespaces} from '../../messages/namespaces';
import {siteBrand} from '../lib/brands';

export default getRequestConfig(async ({requestLocale}) => {
  const brand = siteBrand();
  const requested = await requestLocale;
  const locale = hasLocale(routing.locales, requested) ? requested : brand.defaultLocale;

  const messages: Record<string, unknown> = {};

  await Promise.all(
    namespaces.map(async (ns) => {
      try {
        const nsMessages = (await import(`../../messages/${locale}/${ns}.json`)).default;
        messages[ns] = nsMessages;
      } catch {
        // 缺文件回落到默认语言（zh-CN）。
        const fallbackMessages = (await import(`../../messages/${brand.defaultLocale}/${ns}.json`)).default;
        messages[ns] = fallbackMessages;
      }
    })
  );

  return {
    locale,
    messages
  };
});
