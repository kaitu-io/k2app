import { type Brand } from '../brands';
import { KAITU_SITE } from './kaitu';
import type { SiteConfig } from './types';

export type { SiteConfig, NavItem, FooterColumn, ContentCategoryDef } from './types';
export { isExternalHref } from './types';

/** 站点结构（导航 / 页脚 / sitemap 路由 / 内容分类 / SEO 默认 / 定价快照）。 */
export function siteConfig(): SiteConfig {
  return KAITU_SITE;
}

/** 填充 seo / 分类模板里的 `{wordmark}` / `{displayName}` 占位。 */
export function fillBrandTemplate(template: string, brand: Brand): string {
  return template.replaceAll('{wordmark}', brand.wordmark).replaceAll('{displayName}', brand.displayName);
}
