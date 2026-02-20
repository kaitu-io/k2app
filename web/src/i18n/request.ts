import {getRequestConfig} from 'next-intl/server';
import {hasLocale} from 'next-intl';
import {routing} from './routing';
import {namespaces} from '../../messages/namespaces';

export default getRequestConfig(async ({requestLocale}) => {
  const requested = await requestLocale;
  const locale = hasLocale(routing.locales, requested)
    ? requested
    : routing.defaultLocale;

  // 加载所有 namespace 文件并合并
  const messages: Record<string, unknown> = {};

  await Promise.all(
    namespaces.map(async (ns) => {
      try {
        const nsMessages = (await import(`../../messages/${locale}/${ns}.json`)).default;
        messages[ns] = nsMessages;
      } catch {
        // 回退到默认语言
        const fallbackMessages = (await import(`../../messages/zh-CN/${ns}.json`)).default;
        messages[ns] = fallbackMessages;
      }
    })
  );

  return {
    locale,
    messages
  };
});
