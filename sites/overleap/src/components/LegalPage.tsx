import path from 'path';
import { readFile } from 'fs/promises';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import SiteShell from './SiteShell';
import type { Locale } from '@/lib/site';

export type LegalDoc = 'privacy-policy' | 'terms-of-service' | 'delete-account';

/**
 * Renders public/legal/<doc>.md (English master; every locale reads it —
 * an untranslated legal text is readable, a half-translated one is a liability).
 * The body is marked lang="en" dir="ltr" so it stays left-to-right inside a
 * right-to-left page.
 * The files are filed with Apple and Google, so their URLs must stay stable.
 */
export default async function LegalPage({
  locale,
  doc,
  title,
  subtitle,
}: {
  locale: Locale;
  doc: LegalDoc;
  title: string;
  subtitle: string;
}) {
  const content = await readFile(path.join(process.cwd(), 'public/legal', `${doc}.md`), 'utf-8');
  return (
    <SiteShell locale={locale}>
      <article className="mx-auto max-w-3xl px-4 py-16 sm:px-6">
        <header className="mb-10 border-b pb-8">
          <h1 className="text-4xl font-bold tracking-tight">{title}</h1>
          <p className="mt-3 text-lg text-muted-foreground">{subtitle}</p>
        </header>
        <div className="prose max-w-none" lang="en" dir="ltr">
          {/* Documents use ### for sections; the page owns the only <h1>, so shift down by one level. */}
          <ReactMarkdown
            remarkPlugins={[remarkGfm]}
            components={{ h3: ({ children }) => <h2>{children}</h2>, h4: ({ children }) => <h3>{children}</h3> }}
          >
            {content}
          </ReactMarkdown>
        </div>
      </article>
    </SiteShell>
  );
}
