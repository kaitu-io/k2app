import { routing } from '@/i18n/routing';
import { ownerBrand, brandById, KAITU } from '@/lib/brands';

interface HreflangLinksProps {
  pathname: string;
}

/**
 * Emits one `<link rel="alternate" hreflang>` per supported locale, each
 * pointing to the host that owns that locale:
 *   - zh-* → https://kaitu.io
 *   - en-*, ja → https://overleap.io
 *
 * `x-default` points to zh-CN on kaitu.io (Chinese is the product's default
 * locale — see brand architecture doc).
 */
export default function HreflangLinks({ pathname }: HreflangLinksProps) {
  const suffix = pathname === '/' ? '' : pathname;

  const hreflangLinks = routing.locales.map(locale => {
    const ownerBaseUrl = brandById(ownerBrand(locale)).baseUrl;
    const url = `${ownerBaseUrl}/${locale}${suffix}`;
    return (
      <link
        key={locale}
        rel="alternate"
        hrefLang={locale.toLowerCase()}
        href={url}
      />
    );
  });

  // x-default → zh-CN on kaitu.io
  const defaultUrl = `${KAITU.baseUrl}/${KAITU.defaultLocale}${suffix}`;
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
