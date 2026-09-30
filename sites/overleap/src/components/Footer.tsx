import Image from 'next/image';
import { getTranslations } from 'next-intl/server';
import { Link } from '@/i18n/routing';
import { FOOTER, SITE, type Locale } from '@/lib/site';

export default async function Footer({ locale }: { locale: Locale }) {
  const t = await getTranslations({ locale, namespace: 'nav' });
  const year = new Date().getFullYear();

  return (
    <footer className="border-t">
      <div className="mx-auto grid max-w-6xl gap-10 px-4 py-12 sm:px-6 md:grid-cols-[2fr_1fr_1fr]">
        <div>
          <div className="flex items-center gap-2 font-semibold">
            <Image src={SITE.logoPath} alt="" width={24} height={24} className="rounded-md" />
            {SITE.name}
          </div>
          <p className="mt-3 max-w-xs text-sm text-muted-foreground">{t('footer.tagline')}</p>
        </div>
        {FOOTER.map((col) => (
          <div key={col.titleKey}>
            <h2 className="text-sm font-semibold">{t(col.titleKey)}</h2>
            <ul className="mt-3 space-y-2 text-sm">
              {col.items.map((item) => (
                <li key={item.href}>
                  {item.external ? (
                    <a href={item.href} className="text-muted-foreground hover:text-foreground" {...(item.href.startsWith('http') ? { target: '_blank', rel: 'noopener noreferrer' } : {})}>
                      {t(item.labelKey)}
                    </a>
                  ) : (
                    <Link href={item.href} className="text-muted-foreground hover:text-foreground">
                      {t(item.labelKey)}
                    </Link>
                  )}
                </li>
              ))}
            </ul>
          </div>
        ))}
      </div>
      <div className="border-t">
        <p className="mx-auto max-w-6xl px-4 py-6 text-xs text-muted-foreground sm:px-6">
          {t('footer.copyright', { year, legalName: SITE.legalName })}
        </p>
      </div>
    </footer>
  );
}
