import type { ReactNode } from 'react';
import { NextIntlClientProvider } from 'next-intl';
import { getMessages, setRequestLocale } from 'next-intl/server';
import { routing } from './routing';
import { BASE_MESSAGE_PATHS, GROUP_MESSAGE_PATHS } from './client-messages.generated';
import { pickMessages } from './pick-messages';

/**
 * The client-side message provider for a page tree.
 *
 * Why not `<NextIntlClientProvider messages={all}>`: every prerendered page
 * embeds whatever the provider is given, in both its .html and its .rsc. With
 * all 25 namespaces that was 300-450 KB per page (≈ 55 KB gzip), and it pushed
 * the Amplify build output over its 220 MiB cap (job 145, 2026-11-10).
 *
 * Server components translate on the server (`getTranslations`) and ship text,
 * so only code under a "use client" boundary needs messages. Which paths that is
 * comes from `client-messages.generated.ts` (scripts/i18n/gen-client-messages.mjs
 * walks the import graph); tests/client-messages.test.ts keeps it current.
 *
 * - Root layout: `<ClientMessages locale>` = BASE (shell + Header/Footer).
 * - A route directory that needs more: its layout wraps in
 *   `<ClientMessages locale group="<dir>">` = BASE + that group's extras. A
 *   nested provider REPLACES the parent's messages, hence the union.
 *
 * `locale` is passed explicitly (not read from the request) so a nested layout
 * stays statically renderable.
 */
export default async function ClientMessages({
  locale,
  group,
  children,
}: {
  locale: string;
  group?: string;
  children: ReactNode;
}) {
  const extra = group ? GROUP_MESSAGE_PATHS[group] : undefined;
  if (group && !extra) {
    throw new Error(
      `ClientMessages: unknown group "${group}" — run node scripts/i18n/gen-client-messages.mjs`
    );
  }
  const typedLocale = locale as (typeof routing.locales)[number];
  setRequestLocale(typedLocale);
  const all = (await getMessages({ locale: typedLocale })) as Record<string, unknown>;
  const messages = pickMessages(all, [...BASE_MESSAGE_PATHS, ...(extra ?? [])]);
  return (
    <NextIntlClientProvider locale={typedLocale} messages={messages}>
      {children}
    </NextIntlClientProvider>
  );
}
