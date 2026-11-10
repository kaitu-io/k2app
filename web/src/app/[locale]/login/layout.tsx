import type { Metadata } from 'next';
import { generateMetadata as generateBaseMetadata } from '../metadata';
import ClientMessages from '@/i18n/ClientMessages';

// login/page.tsx is a client component and cannot export metadata. Without
// this layout the page inherits [locale]/layout.tsx's metadata, whose
// pathname is '' at static-generation time — canonical would point at the
// locale homepage.
export async function generateMetadata({
  params,
}: {
  params: Promise<{ locale: string }>;
}): Promise<Metadata> {
  const { locale } = await params;
  return generateBaseMetadata(locale, '/login');
}

// Client messages: BASE + the "login" group (src/i18n/client-messages.generated.ts).
export default async function LoginLayout({
  children,
  params,
}: {
  children: React.ReactNode;
  params: Promise<{ locale: string }>;
}) {
  const { locale } = await params;
  return (
    <ClientMessages locale={locale} group="login">
      {children}
    </ClientMessages>
  );
}
