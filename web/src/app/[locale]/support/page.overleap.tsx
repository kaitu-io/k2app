import type { Metadata } from 'next';
import { getTranslations, setRequestLocale } from 'next-intl/server';
import { routing } from '@/i18n/routing';
import { Link } from '@/i18n/routing';
import { siteBrand } from '@/lib/brands';
import { siteConfig } from '@/lib/site';
import { displayCurrency, formatMinor } from '@/lib/pricing';
import Header from '@/components/Header';
import Footer from '@/components/Footer';
import OverleapFAQ from '@/components/home-overleap/OverleapFAQ';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Mail } from 'lucide-react';
import { generateMetadata as generateBaseMetadata } from '../metadata';

type Locale = (typeof routing.locales)[number];

// 与首页同一组 FAQ（landing namespace）；帮助页只多"账户与账单"四条。
const FAQ_KEYS = [
  'logs', 'isp', 'legal', 'publicWifi', 'travel', 'ech',
  'selfHost', 'platforms', 'devices', 'pricing', 'payment', 'cancel',
] as const;
const BILLING_KEYS = ['manage', 'charge', 'refund', 'devices'] as const;
const STEP_KEYS = ['step1', 'step2', 'step3'] as const;

export function generateStaticParams() {
  return siteBrand().allowedLocales.map((locale) => ({ locale }));
}

export async function generateMetadata({ params }: { params: Promise<{ locale: string }> }): Promise<Metadata> {
  const { locale: rawLocale } = await params;
  const locale = rawLocale as Locale;
  const brand = siteBrand();
  const t = await getTranslations({ locale, namespace: 'help' });
  const vars = { brand: brand.displayName };
  return generateBaseMetadata(locale, '/support', {
    title: `${t('help.meta.title')} | ${brand.displayName}`,
    description: t('help.meta.description', vars),
  }, brand);
}

/** 首页 FAQ 的 {yearly} {monthly} 插值在帮助页同样要填，否则原样露出占位。 */
function priceVars(locale: string) {
  const pricing = siteConfig().pricing;
  if (!pricing) return { yearly: '', monthly: '', currency: '' };
  const currency = displayCurrency(locale);
  const cur = pricing.yearly[currency] === undefined ? 'usd' : currency;
  return {
    yearly: formatMinor(pricing.yearly[cur], cur, locale),
    monthly: formatMinor(pricing.monthly[cur], cur, locale),
    currency: cur.toUpperCase(),
  };
}

// overleap 构建专属帮助页（page.overleap.tsx）。kaitu 的家长指南在 page.kaitu.tsx。
export default async function SupportPage({ params }: { params: Promise<{ locale: string }> }) {
  const { locale: rawLocale } = await params;
  const locale = rawLocale as Locale;
  setRequestLocale(locale);
  const brand = siteBrand();
  const t = await getTranslations({ locale, namespace: 'help' });
  const tl = await getTranslations({ locale, namespace: 'landing' });
  const vars = { brand: brand.displayName, email: brand.contactEmail, ...priceVars(locale) };

  const billingItems = BILLING_KEYS.map((key) => ({
    key,
    question: t(`help.billing.items.${key}.question`, vars),
    answer: t(`help.billing.items.${key}.answer`, vars),
  }));
  const faqItems = FAQ_KEYS.map((key) => ({
    key,
    question: tl(`landing.faq.items.${key}.question`, vars),
    answer: tl(`landing.faq.items.${key}.answer`, vars),
  }));

  const faqJsonLd = {
    '@context': 'https://schema.org',
    '@type': 'FAQPage',
    mainEntity: [...billingItems, ...faqItems].map((i) => ({
      '@type': 'Question',
      name: i.question,
      acceptedAnswer: { '@type': 'Answer', text: i.answer },
    })),
  };

  return (
    <div className="min-h-screen bg-background text-foreground">
      <script type="application/ld+json" suppressHydrationWarning dangerouslySetInnerHTML={{ __html: JSON.stringify(faqJsonLd) }} />
      <Header />

      <main>
        <section className="max-w-5xl mx-auto px-4 sm:px-6 lg:px-8 pt-14 pb-8">
          <h1 className="text-4xl font-bold mb-3">{t('help.title')}</h1>
          <p className="text-muted-foreground max-w-2xl">{t('help.intro')}</p>
        </section>

        <section id="getting-started" className="max-w-5xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
          <h2 className="text-2xl font-semibold mb-6">{t('help.gettingStarted.title')}</h2>
          <div className="grid md:grid-cols-3 gap-5 mb-6">
            {STEP_KEYS.map((k, i) => (
              <Card key={k} className="p-6 bg-card border-border">
                <span className="inline-flex w-7 h-7 rounded-full bg-primary/15 text-primary text-xs font-bold items-center justify-center mb-3">{i + 1}</span>
                <p className="font-semibold mb-1">{t(`help.gettingStarted.${k}.title`)}</p>
                <p className="text-sm text-muted-foreground">{t(`help.gettingStarted.${k}.body`, vars)}</p>
              </Card>
            ))}
          </div>
          <Button asChild variant="outline">
            <Link href="/install">{t('help.gettingStarted.download')}</Link>
          </Button>
        </section>

        <section id="billing" className="max-w-5xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
          <h2 className="text-2xl font-semibold mb-6">{t('help.billing.title')}</h2>
          <dl className="space-y-5 mb-6">
            {billingItems.map((i) => (
              <div key={i.key}>
                <dt className="font-semibold mb-1">{i.question}</dt>
                <dd className="text-sm text-muted-foreground">{i.answer}</dd>
              </div>
            ))}
          </dl>
          <Button asChild variant="outline">
            <Link href="/account">{t('help.billing.account')}</Link>
          </Button>
        </section>

        <OverleapFAQ title={t('help.faq.title')} subtitle={t('help.faq.subtitle', vars)} items={faqItems} />

        <section id="contact" className="max-w-5xl mx-auto px-4 sm:px-6 lg:px-8 py-14">
          <Card className="p-8 bg-card border-primary/40 flex flex-col md:flex-row md:items-center gap-6">
            <div className="flex-1">
              <h2 className="text-2xl font-semibold mb-2">{t('help.contact.title')}</h2>
              <p className="text-muted-foreground">{t('help.contact.body', vars)}</p>
            </div>
            <Button asChild size="lg" className="font-semibold">
              <a href={`mailto:${brand.contactEmail}`}>
                <Mail className="w-4 h-4 mr-2" />
                {t('help.contact.button')}
              </a>
            </Button>
          </Card>
        </section>
      </main>

      <Footer />
    </div>
  );
}
