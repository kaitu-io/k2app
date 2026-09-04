"use client";

import { useLocale, useTranslations } from 'next-intl';
import { Link } from '@/i18n/routing';
import NextLink from 'next/link';
import Image from 'next/image';
import { useBrand } from '@/hooks/useBrand';
import { useAuth } from '@/contexts/AuthContext';
import { siteConfig, isExternalHref, type NavItem } from '@/lib/site';

/**
 * 页脚：栏目与链接来自 `lib/site/<brand>.ts`（spec 2026-09-04-overleap-site-decoupling §2）。
 * 一个品牌的页脚只能链到该品牌构建里存在的页面——由配置保证，不再有
 * `brand.features.x && <li>` 式的条件渲染（那种写法漏一处就是死链）。
 */
export default function Footer() {
  const brand = useBrand();
  const site = siteConfig(brand);
  const t = useTranslations();
  const locale = useLocale();
  const { user } = useAuth();
  const showTaglineZh = Boolean(brand.taglineZh) && locale.startsWith('zh');

  const renderItem = (item: NavItem) => {
    const text = t(item.labelKey, { brand: brand.wordmark });
    if (isExternalHref(item.href)) {
      const isMail = item.href.startsWith('mailto:');
      return (
        <NextLink
          href={item.href}
          className="hover:text-blue-600"
          {...(isMail ? {} : { target: '_blank', rel: 'noopener noreferrer' })}
        >
          {text}
        </NextLink>
      );
    }
    return (
      <Link href={item.href} className="hover:text-blue-600">
        {text}
      </Link>
    );
  };

  return (
    <footer className="py-12 px-4 sm:px-6 lg:px-8 border-t">
      <div className="max-w-7xl mx-auto">
        <div className={`grid gap-8 ${site.footer.length >= 4 ? 'md:grid-cols-5' : 'md:grid-cols-4'}`}>
          <div>
            <div className="flex items-center space-x-2 mb-4">
              <Image
                src={brand.logoPath}
                alt={`${brand.displayName} Logo`}
                width={32}
                height={32}
                className="rounded-md"
              />
              <span className="text-xl font-bold text-foreground">{brand.wordmark}</span>
            </div>
            <p className="text-muted-foreground text-sm">
              {t('nav.footer.brandDescription', { brand: brand.wordmark })}
            </p>
          </div>

          {site.footer.map((column) => (
            <div key={column.titleKey}>
              <h4 className="font-semibold text-foreground mb-4">{t(column.titleKey)}</h4>
              <ul className="space-y-2 text-sm text-muted-foreground">
                {column.items.map((item) => (
                  <li key={`${item.labelKey}:${item.href}`}>{renderItem(item)}</li>
                ))}
              </ul>
            </div>
          ))}
        </div>

        <div className="mt-8 pt-8 border-t text-center text-sm text-muted-foreground">
          {showTaglineZh && (
            <p className="mb-2 text-muted-foreground/60 italic">{brand.taglineZh}</p>
          )}
          <p>{'©'} {new Date().getFullYear()} {brand.legalName}{'. '}{t('nav.footer.copyright')}</p>
          {user?.isAdmin && (
            <NextLink
              href="/manager"
              className="mt-2 inline-block text-xs text-muted-foreground/30 hover:text-muted-foreground transition-colors"
            >
              {t('nav.nav.adminPanel')}
            </NextLink>
          )}
        </div>
      </div>
    </footer>
  );
}
