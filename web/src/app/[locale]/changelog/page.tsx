import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import { routing } from '@/i18n/routing';
import ChangelogClient from './ChangelogClient';

type Locale = (typeof routing.locales)[number];

export const dynamic = 'force-static';

/**
 * Generate metadata for the changelog page (used by Next.js for <head> tags).
 * Requires server-side translation to produce locale-aware title/description.
 */
export async function generateMetadata({
  params,
}: {
  params: Promise<{ locale: string }>;
}): Promise<Metadata> {
  const { locale: rawLocale } = await params;
  const locale = rawLocale as Locale;
  const t = await getTranslations({ locale, namespace: 'changelog' });

  return {
    title: t('title'),
    description: t('subtitle'),
  };
}

/**
 * Changelog page Server Component — SSR-converted from client component.
 *
 * The server shell is minimal: sets locale and renders ChangelogClient.
 * ChangelogClient is the full page replacement — it handles runtime fetch
 * of /changelog.json, accordion expand/collapse, and embed mode detection.
 * Uses async params per Next.js 15 pattern.
 */
export default async function ChangelogPage({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const { locale: rawLocale } = await params;
  const locale = rawLocale as Locale;
  setRequestLocale(locale);

  return <ChangelogClient />;
}
