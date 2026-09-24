import { getTranslations } from 'next-intl/server';
import { Link, routing } from '@/i18n/routing';
import { ChevronRight } from 'lucide-react';
import type { Brand } from '@/lib/brands';

export interface BreadcrumbItem {
  label: string;
  /** 站内路径（不含 locale 前缀）。最后一项通常不给 href，表示当前页。 */
  href?: string;
}

interface BreadcrumbProps {
  locale: string;
  brand: Brand;
  /** 首页之后的路径段；首页由组件自动补在最前面。 */
  items: BreadcrumbItem[];
  className?: string;
}

/**
 * 内容页面包屑（服务端组件）：`首页 > 分区 > 当前页`。
 *
 * 同时输出 schema.org BreadcrumbList，让搜索结果里显示层级而不是裸 URL。
 * 位置信号是导航改造的一部分（顶栏高亮说"在哪个分区"，面包屑说"在分区里的哪一页"）。
 */
export default async function Breadcrumb({ locale, brand, items, className }: BreadcrumbProps) {
  const t = await getTranslations({ locale: locale as (typeof routing.locales)[number], namespace: 'nav' });
  const trail: BreadcrumbItem[] = [{ label: t('nav.home'), href: '/' }, ...items];

  const jsonLd = {
    '@context': 'https://schema.org',
    '@type': 'BreadcrumbList',
    itemListElement: trail.map((item, index) => ({
      '@type': 'ListItem',
      position: index + 1,
      name: item.label,
      ...(item.href ? { item: `${brand.baseUrl}/${locale}${item.href === '/' ? '' : item.href}` } : {}),
    })),
  };

  return (
    <>
      {/* 内容来自构建期 markdown 与文案文件（非用户输入）；`<` 仍转义，防止标题里出现 </script>。 */}
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{ __html: JSON.stringify(jsonLd).replace(/</g, '\\u003c') }}
      />
      <nav aria-label={t('nav.breadcrumb')} className={className}>
        <ol className="flex flex-wrap items-center gap-1 text-sm text-muted-foreground">
          {trail.map((item, index) => {
            const last = index === trail.length - 1;
            return (
              <li key={`${index}:${item.href ?? item.label}`} className="flex items-center gap-1 min-w-0">
                {index > 0 && <ChevronRight aria-hidden className="w-3.5 h-3.5 shrink-0" />}
                {item.href && !last ? (
                  <Link href={item.href} className="hover:text-foreground transition-colors truncate">
                    {item.label}
                  </Link>
                ) : (
                  <span aria-current={last ? 'page' : undefined} className={last ? 'text-foreground truncate' : 'truncate'}>
                    {item.label}
                  </span>
                )}
              </li>
            );
          })}
        </ol>
      </nav>
    </>
  );
}
