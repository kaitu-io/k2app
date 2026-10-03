/**
 * 站点结构配置（lib/site/kaitu.ts）里引用的每个 i18n key 都必须在每个 locale 的
 * 消息文件里真实存在。Header/Footer 的渲染测试跑在全局 mock 的 next-intl 上（回显 key），
 * 看不见缺文案；这里静态核对，缺一个 key 线上就是一段 "nav.nav.xxx" 原文。
 */
import { describe, expect, it } from 'vitest';
import fs from 'node:fs';
import path from 'node:path';
import { KAITU } from '../src/lib/brands';
import { siteConfig, type NavItem } from '../src/lib/site';

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

function allKeys(): string[] {
  const site = siteConfig();
  const keys = new Set<string>([...navKeys(site.nav.primary), site.nav.cta.labelKey]);
  for (const col of site.footer) {
    keys.add(col.titleKey);
    for (const k of navKeys(col.items)) keys.add(k);
  }
  return [...keys];
}

describe('site config keys resolve', () => {
  const brand = KAITU;
  it.each(brand.allowedLocales)('%s has every nav/footer key', (locale) => {
    const missing = allKeys().filter((k) => typeof lookup(locale, k) !== 'string');
    expect(missing).toEqual([]);
  });

  it('seo defaults cover the default locale', () => {
    const { seo } = siteConfig();
    expect(seo.defaultTitle[brand.defaultLocale]).toBeTruthy();
    expect(seo.defaultDescription[brand.defaultLocale]).toBeTruthy();
  });

  it('static routes are unique and locale-free', () => {
    const { staticRoutes } = siteConfig();
    expect(new Set(staticRoutes).size).toBe(staticRoutes.length);
    for (const r of staticRoutes) expect(r).not.toMatch(/^\/(en|zh|ja)/);
  });
});

/**
 * 导航结构的形态约束。产品站的顶栏是 3–6 个一级项 + 一个 CTA；同一个文案 key 在顶栏和
 * 页脚里必须指向同一路径；
 * 兼容跳转路径（/changelog → /releases）不得再被链接。
 */
describe('nav structure', () => {
  const site = siteConfig();

  it('primary nav has 3–6 items and every item is a link', () => {
    expect(site.nav.primary.length).toBeGreaterThanOrEqual(3);
    expect(site.nav.primary.length).toBeLessThanOrEqual(6);
    for (const item of site.nav.primary) {
      expect(item.href, item.labelKey).toMatch(/^(\/|https?:\/\/)/);
      if (item.children) expect(item.children.length).toBeGreaterThanOrEqual(2);
    }
  });

  it('a label resolves to one href across header and footer', () => {
    const seen = new Map<string, string>();
    const conflicts: string[] = [];
    const visit = (item: NavItem) => {
      const prev = seen.get(item.labelKey);
      if (prev !== undefined && prev !== item.href) conflicts.push(`${item.labelKey}: ${prev} vs ${item.href}`);
      seen.set(item.labelKey, item.href);
      item.children?.forEach(visit);
    };
    site.nav.primary.forEach(visit);
    visit(site.nav.cta);
    site.footer.forEach((col) => col.items.forEach(visit));
    expect(conflicts).toEqual([]);
  });

  it('no link targets a redirect-only path', () => {
    const REDIRECT_ONLY = ['/changelog'];
    const all: string[] = [];
    const collect = (item: NavItem) => {
      all.push(item.href.split('#')[0]);
      item.children?.forEach(collect);
    };
    site.nav.primary.forEach(collect);
    site.footer.forEach((col) => col.items.forEach(collect));
    expect(all.filter((h) => REDIRECT_ONLY.includes(h))).toEqual([]);
  });
});
