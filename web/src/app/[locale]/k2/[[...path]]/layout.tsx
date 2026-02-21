/**
 * /k2/ Section Layout
 *
 * Wraps all /k2/[[...path]] pages with a two-column layout:
 *   - Left: K2Sidebar (w-64 fixed)
 *   - Right: main content area (flex-1)
 *
 * Also injects a TechArticle JSON-LD script for SEO.
 */
import type { ReactNode } from 'react';
import { getTranslations, setRequestLocale } from 'next-intl/server';
import Header from '@/components/Header';
import Footer from '@/components/Footer';
import K2Sidebar from '@/components/K2Sidebar';
import { getK2Posts } from '@/lib/k2-posts';
import { routing } from '@/i18n/routing';

interface LayoutParams {
  locale: string;
}

interface K2LayoutProps {
  children: ReactNode;
  params: Promise<LayoutParams>;
}

export default async function K2Layout({
  children,
  params,
}: K2LayoutProps): Promise<React.ReactElement> {
  const { locale } = await params;

  setRequestLocale(locale as (typeof routing.locales)[number]);

  const groups = getK2Posts(locale);
  const t = await getTranslations({ locale, namespace: 'k2' });

  // Build section label map for the sidebar
  const sectionKeys = ['getting-started', 'technical', 'comparison'] as const;
  const sectionLabels: Record<string, string> = {};
  for (const key of sectionKeys) {
    try {
      sectionLabels[key] = t(`sections.${key}`);
    } catch {
      sectionLabels[key] = key;
    }
  }

  // Add labels for any sections present in posts that aren't in the default set
  for (const group of groups) {
    if (!(group.section in sectionLabels)) {
      try {
        sectionLabels[group.section] = t(`sections.${group.section}`);
      } catch {
        sectionLabels[group.section] = group.section;
      }
    }
  }

  const jsonLd = {
    '@context': 'https://schema.org',
    '@type': 'TechArticle',
    publisher: {
      '@type': 'Organization',
      name: 'Kaitu',
      url: 'https://kaitu.io',
    },
  };

  return (
    <div className="min-h-screen bg-background">
      <Header />
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{ __html: JSON.stringify(jsonLd) }}
      />
      <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-12">
        <div className="flex gap-8">
          <K2Sidebar
            groups={groups}
            sectionLabels={sectionLabels}
            locale={locale}
          />
          <main className="min-w-0 flex-1">
            {children}
          </main>
        </div>
      </div>
      <Footer />
    </div>
  );
}
