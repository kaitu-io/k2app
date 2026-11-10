import type { ReactNode } from 'react';
import type { Metadata } from 'next';
import ClientMessages from '@/i18n/ClientMessages';

// License-key redeem pages (/g, /g/<code>): the code is in the URL, nothing here belongs in the index.
// Child pages that return their own metadata (title) inherit this `robots`.
export const metadata: Metadata = { robots: { index: false, follow: false } };

// Client-side messages for this route tree: BASE + the "g" group
// (src/i18n/client-messages.generated.ts, derived from what its client
// components use).
export default async function RouteGLayout({
  children,
  params,
}: {
  children: ReactNode;
  params: Promise<{ locale: string }>;
}) {
  const { locale } = await params;
  return (
    <ClientMessages locale={locale} group="g">
      {children}
    </ClientMessages>
  );
}
