import type { Metadata } from 'next';
import { generateMetadata as generateBaseMetadata } from '../metadata';

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

export default function LoginLayout({ children }: { children: React.ReactNode }) {
  return children;
}
