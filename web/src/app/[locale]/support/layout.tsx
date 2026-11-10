import type { ReactNode } from 'react';
import ClientMessages from '@/i18n/ClientMessages';

// Client-side messages for this route tree: BASE + the "support" group
// (src/i18n/client-messages.generated.ts, derived from what its client
// components use).
export default async function RouteSupportLayout({
  children,
  params,
}: {
  children: ReactNode;
  params: Promise<{ locale: string }>;
}) {
  const { locale } = await params;
  return (
    <ClientMessages locale={locale} group="support">
      {children}
    </ClientMessages>
  );
}
