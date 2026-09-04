/**
 * 站点结构配置（lib/site/<brand>.ts）里引用的每个 i18n key 都必须在该品牌默认语言的
 * 消息文件里真实存在。Header/Footer 的渲染测试跑在全局 mock 的 next-intl 上（回显 key），
 * 看不见缺文案；这里静态核对，缺一个 key 线上就是一段 "nav.nav.xxx" 原文。
 */
import { describe, expect, it } from 'vitest';
import fs from 'node:fs';
import path from 'node:path';
import { KAITU, OVERLEAP, type Brand } from '../src/lib/brands';
import { siteConfigFor, type NavItem } from '../src/lib/site';

const MESSAGES = path.resolve(__dirname, '../messages');

function lookup(locale: string, fullKey: string): unknown {
  const [ns, ...rest] = fullKey.split('.');
  const file = path.join(MESSAGES, locale, `${ns}.json`);
  if (!fs.existsSync(file)) return undefined;
  return rest.reduce<unknown>(
    (acc, part) => (acc && typeof acc === 'object' ? (acc as Record<string, unknown>)[part] : undefined),
    JSON.parse(fs.readFileSync(file, 'utf8')),
  );
}

function* navKeys(items: NavItem[]): Generator<string> {
  for (const item of items) {
    yield item.labelKey;
    if (item.children) yield* navKeys(item.children);
  }
}

function allKeys(brand: Brand): string[] {
  const site = siteConfigFor(brand.id);
  const keys = new Set<string>([...navKeys(site.nav.primary), site.nav.cta.labelKey]);
  for (const col of site.footer) {
    keys.add(col.titleKey);
    for (const k of navKeys(col.items)) keys.add(k);
  }
  return [...keys];
}

describe.each([KAITU, OVERLEAP])('site config keys resolve for $id', (brand) => {
  it.each(brand.allowedLocales)('%s has every nav/footer key', (locale) => {
    const missing = allKeys(brand).filter((k) => typeof lookup(locale, k) !== 'string');
    expect(missing).toEqual([]);
  });

  it('seo defaults cover the default locale', () => {
    const { seo } = siteConfigFor(brand.id);
    expect(seo.defaultTitle[brand.defaultLocale]).toBeTruthy();
    expect(seo.defaultDescription[brand.defaultLocale]).toBeTruthy();
  });

  it('static routes are unique and locale-free', () => {
    const { staticRoutes } = siteConfigFor(brand.id);
    expect(new Set(staticRoutes).size).toBe(staticRoutes.length);
    for (const r of staticRoutes) expect(r).not.toMatch(/^\/(en|zh|ja)/);
  });
});
