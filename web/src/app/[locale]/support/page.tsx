import type { Metadata } from 'next';
import { getTranslations, setRequestLocale } from 'next-intl/server';
import { routing } from '@/i18n/routing';
import { generateMetadata as generateBaseMetadata } from '../metadata';
import SupportClient from './SupportClient';
import { getBrand } from '@/lib/brand-server';
import { siteBrand } from '@/lib/brands';

interface Props {
  params: Promise<{ locale: string }>;
}

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { locale } = await params;
  const t = await getTranslations({ locale: locale as (typeof routing.locales)[number], namespace: 'guide-parents' });
  const brand = await getBrand();

  return generateBaseMetadata(
    locale,
    '/support',
    {
      title: t('meta.title'),
      description: t('meta.description'),
    },
    brand
  );
}

// Only this brand's locales. The parent layout 404s off-brand locales anyway,
// so generating the full routing.locales set just prerendered dead shells.
export function generateStaticParams() {
  return siteBrand().allowedLocales.map((locale) => ({ locale }));
}

export default async function SupportPage({ params }: Props) {
  const { locale } = await params;
  setRequestLocale(locale as (typeof routing.locales)[number]);
  const brand = await getBrand();

  const t = await getTranslations({ locale: locale as (typeof routing.locales)[number], namespace: 'guide-parents' });

  // FAQ structured data for GEO SEO
  const faqKeys = [
    'multiDevice', 'verifyCode', 'paymentSafety', 'wechatPay',
    'windowsBlueScreen', 'macPassword', 'androidInstall',
    'globalMode', 'connectionFailed', 'platforms',
  ];

  const faqJsonLd = {
    '@context': 'https://schema.org',
    '@type': 'FAQPage',
    mainEntity: faqKeys.map((key) => ({
      '@type': 'Question',
      name: t(`faq.items.${key}.question`),
      acceptedAnswer: {
        '@type': 'Answer',
        text: t(`faq.items.${key}.answer`),
      },
    })),
  };

  // Only brands that actually have a guide video advertise one. overleap has no
  // recording yet (Brand.guideVideoUrl === ''), and pointing its VideoObject at
  // the kaitu asset would both leak the brand and lie to search engines.
  const videoJsonLd = brand.guideVideoUrl
    ? {
        '@context': 'https://schema.org',
        '@type': 'VideoObject',
        name: t('video.title'),
        description: t('video.description'),
        contentUrl: brand.guideVideoUrl,
        uploadDate: '2026-03-22',
        duration: 'PT6M36S',
        publisher: {
          '@type': 'Organization',
          name: brand.displayName,
          url: brand.baseUrl,
        },
      }
    : null;

  // JSON-LD content is from trusted i18n translations, safe for inline script
  const faqScript = JSON.stringify(faqJsonLd);

  return (
    <>
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{ __html: faqScript }}
      />
      {videoJsonLd && (
        <script
          type="application/ld+json"
          dangerouslySetInnerHTML={{ __html: JSON.stringify(videoJsonLd) }}
        />
      )}
      <SupportClient />
    </>
  );
}
