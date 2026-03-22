import type { Metadata } from 'next';
import { getTranslations, setRequestLocale } from 'next-intl/server';
import { routing } from '@/i18n/routing';
import { generateMetadata as generateBaseMetadata, baseUrl } from '../metadata';
import SupportClient from './SupportClient';

interface Props {
  params: Promise<{ locale: string }>;
}

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { locale } = await params;
  const t = await getTranslations({ locale: locale as (typeof routing.locales)[number], namespace: 'guide-parents' });

  return generateBaseMetadata(locale, '/support', {
    title: t('meta.title'),
    description: t('meta.description'),
  });
}

export function generateStaticParams() {
  return routing.locales.map((locale) => ({ locale }));
}

export default async function SupportPage({ params }: Props) {
  const { locale } = await params;
  setRequestLocale(locale as (typeof routing.locales)[number]);

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

  const videoJsonLd = {
    '@context': 'https://schema.org',
    '@type': 'VideoObject',
    name: t('video.title'),
    description: t('video.description'),
    contentUrl: 'https://d13jc1jqzlg4yt.cloudfront.net/kaitu/guides/kaitu_guide.mp4',
    uploadDate: '2026-03-22',
    duration: 'PT6M36S',
    publisher: {
      '@type': 'Organization',
      name: 'Kaitu',
      url: baseUrl,
    },
  };

  // JSON-LD content is from trusted i18n translations, safe for inline script
  const faqScript = JSON.stringify(faqJsonLd);
  const videoScript = JSON.stringify(videoJsonLd);

  return (
    <>
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{ __html: faqScript }}
      />
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{ __html: videoScript }}
      />
      <SupportClient />
    </>
  );
}
