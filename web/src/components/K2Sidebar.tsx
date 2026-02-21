'use client';

/**
 * K2Sidebar
 *
 * Server-side data is pre-fetched by the layout and passed as props so the
 * sidebar itself can be a lightweight Client Component that only needs
 * `usePathname()` for active-link highlighting.
 */
import { usePathname } from '@/i18n/routing';
import { Link } from '@/i18n/routing';
import type { K2PostGroup } from '@/lib/k2-posts';

interface K2SidebarProps {
  /** Grouped k2 posts produced by `getK2Posts()` in the Server Component layout. */
  groups: K2PostGroup[];
  /** Map of section keys to translated labels, e.g. `{ "getting-started": "入门" }`. */
  sectionLabels: Record<string, string>;
  /** Current locale, passed from Server Component. */
  locale: string;
}

/**
 * Sidebar navigation for the /k2/ documentation section.
 *
 * Renders section headers and links for each k2/ post. Highlights the link
 * matching the current pathname.
 */
export default function K2Sidebar({
  groups,
  sectionLabels,
  locale,
}: K2SidebarProps): React.ReactElement {
  const pathname = usePathname();

  return (
    <nav
      aria-label="K2 documentation navigation"
      className="w-64 shrink-0 pr-6"
    >
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
    </nav>
  );
}
