import { siteBrand, type Brand, type BrandId } from '../brands';
import { KAITU_SITE } from './kaitu';
import { OVERLEAP_SITE } from './overleap';
import type { SiteConfig } from './types';

export type { SiteConfig, NavItem, FooterColumn, ContentCategoryDef } from './types';
export { isExternalHref } from './types';

export function siteConfigFor(id: BrandId): SiteConfig {
  return id === 'overleap' ? OVERLEAP_SITE : KAITU_SITE;
}

/** 当前构建品牌的站点结构。 */
export function siteConfig(brand: Brand = siteBrand()): SiteConfig {
  return siteConfigFor(brand.id);
}

/** 填充 seo / 分类模板里的 `{wordmark}` / `{displayName}` 占位。 */
export function fillBrandTemplate(template: string, brand: Brand): string {
  return template.replaceAll('{wordmark}', brand.wordmark).replaceAll('{displayName}', brand.displayName);
}
