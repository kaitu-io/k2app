import { localeOf } from '@/lib/locale-param';
import { setRequestLocale } from 'next-intl/server';
import SiteShell from '@/components/SiteShell';
import K2Sidebar from '@/components/K2Sidebar';
import { getK2Groups } from '@/lib/k2-posts';

export default async function K2Layout({
  children,
  params,
}: {
  children: React.ReactNode;
  params: Promise<{ locale: string }>;
}) {
  const locale = await localeOf(params);
  setRequestLocale(locale);
  return (
    <SiteShell locale={locale}>
      <div className="mx-auto flex max-w-6xl flex-col gap-8 px-4 py-10 sm:px-6 md:flex-row">
        <K2Sidebar groups={getK2Groups(locale)} />
        <div className="min-w-0 flex-1">{children}</div>
      </div>
    </SiteShell>
  );
}
