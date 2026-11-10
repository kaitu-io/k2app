import type { ReactNode } from 'react';
import ClientMessages from '@/i18n/ClientMessages';
import AccountShell from './AccountShell';

// force-dynamic stays on the layout (route segment config is read from here).
export const dynamic = 'force-dynamic';

// Server wrapper: the client shell (sidebar, auth guard) and every account page
// read messages from the "account" group (src/i18n/client-messages.generated.ts).
export default async function AccountLayout({
  children,
  params,
}: {
  children: ReactNode;
  params: Promise<{ locale: string }>;
}) {
  const { locale } = await params;
  return (
    <ClientMessages locale={locale} group="account">
      <AccountShell>{children}</AccountShell>
    </ClientMessages>
  );
}
