import { redirect } from 'next/navigation';

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
  const queryParts: string[] = [];
  if (sp.embed) queryParts.push(`embed=${sp.embed}`);
  if (sp.theme) queryParts.push(`theme=${sp.theme}`);
  if (sp.auth_token) queryParts.push(`auth_token=${sp.auth_token}`);
  const query = queryParts.length > 0 ? `?${queryParts.join('&')}` : '';

  redirect(`/${locale}/releases${query}`);
}
