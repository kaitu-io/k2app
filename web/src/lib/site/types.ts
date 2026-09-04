/**
 * 站点结构配置（spec 2026-09-04-overleap-site-decoupling §2）。
 *
 * 导航 / 页脚 / sitemap 静态路由 / 内容分类 / SEO 默认文案按品牌各一份，Header、Footer、
 * sitemap、content-posts、metadata 只读配置渲染——新增或删除一个品牌的页面只改该品牌的
 * 配置文件，不碰共享组件，也不再有 `brand.features.x && <li>` 式的条件渲染。
 *
 * 配置里只放 i18n key 与路径，绝不放品牌展示词（tests/brand-guard.test.ts 的 src 扫描）。
 */
import type { ALL_LOCALES } from '../brands';

export type Locale = (typeof ALL_LOCALES)[number];

export interface NavItem {
  /** next-intl key，全路径写法（如 'nav.nav.pricing'）。 */
  labelKey: string;
  /** 站内路径（交给 i18n Link 加 locale 前缀）或外链（http(s):// / mailto:）。
   *  `mailto:{contactEmail}` 由 Footer 用 Brand.contactEmail 填充——配置文件不放品牌域名。 */
  href: string;
  /** 有子项时渲染为下拉（桌面）/ 折叠段（移动）。 */
  children?: NavItem[];
}

export interface FooterColumn {
  titleKey: string;
  items: NavItem[];
}

export interface ContentCategoryDef {
  /** 按 locale 的显示名；品牌默认语言作回落。 */
  name: Partial<Record<Locale, string>>;
  /** 列表页 meta description。 */
  description?: Partial<Record<Locale, string>>;
}

export interface SiteConfig {
  nav: {
    /** 顶栏左侧主导航。 */
    primary: NavItem[];
    /** 右侧主 CTA。 */
    cta: NavItem;
  };
  footer: FooterColumn[];
  /** sitemap 静态路由（'' = 首页）。只列该品牌构建里真实存在的页面。 */
  staticRoutes: string[];
  /** `[locale]/[...slug]` 目录页服务的内容分类（slug → 定义）。 */
  contentCategories: Record<string, ContentCategoryDef>;
  /**
   * 页面级 metadata 的默认 title / description（页面自己不给 override 时用）。
   * 模板支持 `{wordmark}` / `{displayName}` 占位，由 metadata.ts 用 Brand 注册表填充。
   */
  seo: {
    defaultTitle: Partial<Record<Locale, string>>;
    defaultDescription: Partial<Record<Locale, string>>;
  };
}

export function isExternalHref(href: string): boolean {
  return /^(https?:)?\/\//.test(href) || href.startsWith('mailto:');
}
