'use client';

/**
 * K2Sidebar
 *
 * Server-side data is pre-fetched by the layout and passed as props so the
 * sidebar itself can be a lightweight Client Component that only needs
 * `usePathname()` for active-link highlighting.
 */
import { ChevronDown } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { usePathname, Link } from '@/i18n/routing';
import type { K2PostGroup } from '@/lib/k2-posts';

interface K2SidebarProps {
  /** Grouped k2 posts produced by `getK2Posts()` in the Server Component layout. */
  groups: K2PostGroup[];
  /** Map of section keys to translated labels, e.g. `{ "getting-started": "入门" }`. */
  sectionLabels: Record<string, string>;
  /** Current locale, passed from Server Component. */
  locale: string;
}

interface K2SidebarListProps extends K2SidebarProps {
  pathname: string;
}

function K2SidebarList({
  groups,
  sectionLabels,
  locale,
  pathname,
}: K2SidebarListProps): React.ReactElement {
  return (
    <ul className="space-y-6">
      {groups.map((group) => {
        const label = sectionLabels[group.section] ?? group.section;
        return (
          <li key={group.section}>
            <p className="mb-2 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
              {label}
            </p>
            <ul className="space-y-1">
              {group.posts.map((post) => {
                const href = `/${locale}/${post.slug}`;
                const isActive = pathname === href || pathname.endsWith(`/${post.slug}`);
                return (
                  <li key={post.slug}>
                    <Link
                      href={`/${post.slug}`}
                      className={
                        isActive
                          ? 'block rounded-md px-3 py-1.5 text-sm font-medium bg-muted text-foreground'
                          : 'block rounded-md px-3 py-1.5 text-sm text-muted-foreground hover:bg-muted hover:text-foreground transition-colors'
                      }
                    >
                      {post.title}
                    </Link>
                  </li>
                );
              })}
            </ul>
          </li>
        );
      })}
    </ul>
  );
}

/**
 * Sidebar navigation for the /k2/ documentation section.
 *
 * Renders a collapsible disclosure on mobile (<md) and a fixed sidebar on
 * desktop (≥md). Active link is highlighted based on current pathname.
 */
export default function K2Sidebar({
  groups,
  sectionLabels,
  locale,
}: K2SidebarProps): React.ReactElement {
  const pathname = usePathname();
  const t = useTranslations('k2');

  return (
    <>
      {/* Mobile: collapsible disclosure */}
      <details className="group md:hidden mb-6 rounded-md border border-border bg-card">
        <summary className="cursor-pointer list-none px-4 py-3 text-sm font-medium flex items-center justify-between">
          <span>{t('mobileNav.toggleLabel')}</span>
          <ChevronDown className="h-4 w-4 transition-transform group-open:rotate-180" />
        </summary>
        <nav
          aria-label="K2 documentation navigation"
          className="px-4 pb-4"
        >
          <K2SidebarList
            groups={groups}
            sectionLabels={sectionLabels}
            locale={locale}
            pathname={pathname}
          />
        </nav>
      </details>

      {/* Desktop: fixed sidebar */}
      <nav
        aria-label="K2 documentation navigation"
        className="hidden md:block w-64 shrink-0 pr-6"
      >
        <K2SidebarList
          groups={groups}
          sectionLabels={sectionLabels}
          locale={locale}
          pathname={pathname}
        />
      </nav>
    </>
  );
}
