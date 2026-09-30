'use client';

import { ChevronDown } from 'lucide-react';
import { useTranslations } from 'next-intl';
import { Link, usePathname } from '@/i18n/routing';
import type { K2PostGroup } from '@/lib/k2-posts';
import type k2Messages from '../../messages/en-GB/k2.json';

type SectionKey = keyof typeof k2Messages.sections;

function List({ groups, pathname }: { groups: K2PostGroup[]; pathname: string }) {
  const t = useTranslations('k2');
  return (
    <ul className="space-y-6">
      {groups.map((g) => (
        <li key={g.section}>
          <p className="mb-2 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
            {t.has(`sections.${g.section as SectionKey}`) ? t(`sections.${g.section as SectionKey}`) : g.section}
          </p>
          <ul className="space-y-1">
            {g.posts.map((p) => {
              const href = `/${p.slug}`;
              const active = pathname === href;
              return (
                <li key={p.slug}>
                  <Link
                    href={href}
                    aria-current={active ? 'page' : undefined}
                    className={
                      active
                        ? 'block rounded-md bg-muted px-3 py-1.5 text-sm font-medium text-foreground'
                        : 'block rounded-md px-3 py-1.5 text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground'
                    }
                  >
                    {p.title}
                  </Link>
                </li>
              );
            })}
          </ul>
        </li>
      ))}
    </ul>
  );
}

export default function K2Sidebar({ groups }: { groups: K2PostGroup[] }) {
  const pathname = usePathname();
  const t = useTranslations('k2');
  return (
    <>
      <details className="group mb-6 rounded-md border bg-card md:hidden">
        <summary className="flex cursor-pointer list-none items-center justify-between px-4 py-3 text-sm font-medium">
          {t('mobileNav')}
          <ChevronDown className="h-4 w-4 transition-transform group-open:rotate-180" />
        </summary>
        <nav aria-label={t('navLabel')} className="px-4 pb-4">
          <List groups={groups} pathname={pathname} />
        </nav>
      </details>
      <nav aria-label={t('navLabel')} className="hidden w-60 shrink-0 md:block">
        <List groups={groups} pathname={pathname} />
      </nav>
    </>
  );
}
