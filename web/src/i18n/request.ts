import {getRequestConfig} from 'next-intl/server';
import {hasLocale} from 'next-intl';
import {routing} from './routing';
import {BRAND_NAMESPACES} from '../../messages/namespaces';
import {siteBrand} from '../lib/brands';

export default getRequestConfig(async ({requestLocale}) => {
  const brand = siteBrand();
  const requested = await requestLocale;
  // 品牌默认语言兜底（不是 routing.defaultLocale = zh-CN：overleap 构建绝不落到中文）。
  const locale = hasLocale(routing.locales, requested) && (brand.allowedLocales as readonly string[]).includes(requested)
    ? requested
    : brand.defaultLocale;

  // 只加载本品牌的 namespace（messages/namespaces.ts BRAND_NAMESPACES）。
  const messages: Record<string, unknown> = {};

  await Promise.all(
    BRAND_NAMESPACES[brand.id].map(async (ns) => {
      try {
        const nsMessages = (await import(`../../messages/${locale}/${ns}.json`)).default;
        messages[ns] = nsMessages;
      } catch {
        // 缺文件回落到品牌默认语言——同品牌、同语系，绝不回落到另一品牌的 locale。
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
