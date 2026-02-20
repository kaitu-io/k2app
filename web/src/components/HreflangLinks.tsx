import { routing } from '@/i18n/routing';

interface HreflangLinksProps {
  pathname: string;
}

export default function HreflangLinks({ pathname }: HreflangLinksProps) {
  const baseUrl = process.env.NEXT_PUBLIC_BASE_URL || 'https://kaitu.io';
  
  // Generate hreflang links for all locales
  const hreflangLinks = routing.locales.map(locale => {
    const url = `${baseUrl}/${locale}${pathname === '/' ? '' : pathname}`;
    return (
      <link
        key={locale}
        rel="alternate"
        hrefLang={locale.toLowerCase()}
        href={url}
      />
    );
  });
  
  // Add x-default for the default locale
  const defaultUrl = `${baseUrl}/${routing.defaultLocale}${pathname === '/' ? '' : pathname}`;
  hreflangLinks.push(
    <link
      key="x-default"
      rel="alternate"
      hrefLang="x-default"
      href={defaultUrl}
    />
  );
  
  return <>{hreflangLinks}</>;
}