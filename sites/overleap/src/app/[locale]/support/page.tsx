import { localeOf } from '@/lib/locale-param';
import type { Metadata } from 'next';
import { getTranslations, setRequestLocale } from 'next-intl/server';
import { Mail } from 'lucide-react';
import { Link } from '@/i18n/routing';
import SiteShell from '@/components/SiteShell';
import Faq from '@/components/marketing/Faq';
import JsonLd from '@/components/marketing/JsonLd';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { pageMetadata } from '@/lib/metadata';
import { priceVars } from '@/lib/pricing';
import { SITE } from '@/lib/site';
import ChatWidgetLazy from '@/components/chat/ChatWidgetLazy';

export const dynamic = 'force-static';

// The home FAQ (landing namespace); the help page adds the account-and-billing questions.
const FAQ_KEYS = [
  'logs', 'isp', 'legal', 'publicWifi', 'travel', 'ech',
  'platforms', 'devices', 'pricing', 'payment', 'cancel',
] as const;
const BILLING_KEYS = ['manage', 'charge', 'refund', 'devices'] as const;
const STEP_KEYS = ['step1', 'step2', 'step3'] as const;

type Props = { params: Promise<{ locale: string }> };

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const locale = await localeOf(params);
  const t = await getTranslations({ locale, namespace: 'help' });
  return pageMetadata(locale, '/support', {
    title: `${t('meta.title')} | ${SITE.name}`,
    description: t('meta.description', { brand: SITE.name }),
  });
}

export default async function SupportPage({ params }: Props) {
  const locale = await localeOf(params);
  setRequestLocale(locale);
  const t = await getTranslations({ locale, namespace: 'help' });
  const tl = await getTranslations({ locale, namespace: 'landing' });
  // The home FAQ's {yearly} {monthly} must be filled here too, or the placeholders show.
  const vars = { brand: SITE.name, email: SITE.contactEmail, ...priceVars(locale) };

  const billingItems = BILLING_KEYS.map((key) => ({
    key,
    question: t(`billing.items.${key}.question`),
    answer: t(`billing.items.${key}.answer`, vars),
  }));
  const faqItems = FAQ_KEYS.map((key) => ({
    key,
    question: tl(`faq.items.${key}.question`),
    answer: tl(`faq.items.${key}.answer`, vars),
  }));

  const faqPage = {
    '@context': 'https://schema.org',
    '@type': 'FAQPage',
    mainEntity: [...billingItems, ...faqItems].map((i) => ({
      '@type': 'Question',
      name: i.question,
      acceptedAnswer: { '@type': 'Answer', text: i.answer },
    })),
  };

  return (
    <SiteShell locale={locale}>
      <JsonLd data={faqPage} />

      <section className="mx-auto max-w-5xl px-4 pb-8 pt-14 sm:px-6 lg:px-8">
        <h1 className="mb-3 text-4xl font-bold">{t('title')}</h1>
        <p className="max-w-2xl text-muted-foreground">{t('intro')}</p>
      </section>

      <section id="getting-started" className="mx-auto max-w-5xl px-4 py-8 sm:px-6 lg:px-8">
        <h2 className="mb-6 text-2xl font-semibold">{t('gettingStarted.title')}</h2>
        <div className="mb-6 grid gap-5 md:grid-cols-3">
          {STEP_KEYS.map((k, i) => (
            <Card key={k} className="gap-0 border-border bg-card p-6">
              <span className="mb-3 inline-flex h-7 w-7 items-center justify-center rounded-full bg-primary/15 text-xs font-bold text-primary">{i + 1}</span>
              <p className="mb-1 font-semibold">{t(`gettingStarted.${k}.title`)}</p>
              <p className="text-sm text-muted-foreground">{t(`gettingStarted.${k}.body`, vars)}</p>
            </Card>
          ))}
        </div>
        <Button asChild variant="outline">
          <Link href="/install">{t('gettingStarted.download')}</Link>
        </Button>
      </section>

      <section id="billing" className="mx-auto max-w-5xl px-4 py-8 sm:px-6 lg:px-8">
        <h2 className="mb-6 text-2xl font-semibold">{t('billing.title')}</h2>
        <dl className="mb-6 space-y-5">
          {billingItems.map((i) => (
            <div key={i.key}>
              <dt className="mb-1 font-semibold">{i.question}</dt>
              <dd className="text-sm text-muted-foreground">{i.answer}</dd>
            </div>
          ))}
        </dl>
        <Button asChild variant="outline">
          <Link href="/account">{t('billing.account')}</Link>
        </Button>
      </section>

      <Faq title={t('faq.title')} subtitle={t('faq.subtitle', vars)} items={faqItems} />

      <section id="contact" className="mx-auto max-w-5xl px-4 py-14 sm:px-6 lg:px-8">
        <Card className="flex flex-col gap-6 border-primary/40 bg-card p-8 md:flex-row md:items-center">
          <div className="flex-1">
            <h2 className="mb-2 text-2xl font-semibold">{t('contact.title')}</h2>
            <p className="text-muted-foreground">{t('contact.body', vars)}</p>
          </div>
          <Button asChild size="lg" className="font-semibold">
            <a href={`mailto:${SITE.contactEmail}`}>
              <Mail />
              {t('contact.button')}
            </a>
          </Button>
        </Card>
      </section>
      <ChatWidgetLazy />
    </SiteShell>
  );
}
