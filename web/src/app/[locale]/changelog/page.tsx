import { redirect } from '@/i18n/routing';
import { routing } from '@/i18n/routing';

/**
 * Changelog page — redirects to /releases for backward compatibility.
 * v0.3.22 webapp embeds /changelog?embed=true via iframe; this redirect
 * ensures it lands on the new /releases page with query params preserved.
 */
export default async function ChangelogPage({
  params,
  searchParams,
}: {
  params: Promise<{ locale: string }>;
  searchParams: Promise<{ [key: string]: string | undefined }>;
}) {
  const { locale } = await params;
  const sp = await searchParams;

  // Preserve embed and theme query params through redirect
  const query: Record<string, string> = {};
  if (sp.embed) query.embed = sp.embed;
  if (sp.theme) query.theme = sp.theme;
  if (sp.auth_token) query.auth_token = sp.auth_token;

  redirect({
    href: {
      pathname: '/releases',
      query: Object.keys(query).length > 0 ? query : undefined,
    },
    locale: locale as (typeof routing.locales)[number],
  });
}
