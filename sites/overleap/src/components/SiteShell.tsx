import Header from './Header';
import Footer from './Footer';
import type { Locale } from '@/lib/site';

/** Header + main + Footer. Pages render inside `main`. */
export default function SiteShell({ locale, children }: { locale: Locale; children: React.ReactNode }) {
  return (
    <div className="flex min-h-screen flex-col">
      <Header />
      <main className="flex-1">{children}</main>
      <Footer locale={locale} />
    </div>
  );
}
