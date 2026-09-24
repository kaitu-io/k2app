import { getTranslations, setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import { routing } from '@/i18n/routing';
import Header from '@/components/Header';
import Footer from '@/components/Footer';
import FAQSection from '@/components/home/FAQSection';
import AppPlansGrid from '@/components/pricing/AppPlansGrid';
import RouterPlanCard from '@/components/pricing/RouterPlanCard';
import { getBrand } from '@/lib/brand-server';
import { siteConfig } from '@/lib/site';
import { formatUsd, routerOffer } from '@/lib/router-edition';
import { generateMetadata as generateBaseMetadata } from '../metadata';

type Locale = (typeof routing.locales)[number];

// 每小时重生成：路由器版卡片的预售价 / 发货日按 ROUTER_PRESALE 日期自动切换（同 /routers）。
export const revalidate = 3600;

interface FaqItem {
  q: string;
  a: string;
}

function appEntryPrice(): number {
  const plans = siteConfig().appPlans ?? [];
  return plans.reduce((min, p) => (p.months < min.months ? p : min), plans[0]).price;
}

export async function generateMetadata({
  params,
}: {
  params: Promise<{ locale: string }>;
}): Promise<Metadata> {
  const { locale: rawLocale } = await params;
  const locale = rawLocale as Locale;
  const brand = await getBrand();
  const t = await getTranslations({ locale, namespace: 'pricing' });
  return generateBaseMetadata(
    locale,
    '/pricing',
    {
      // wordmark：中文用户面用中文品牌名，不用 displayName 的拉丁写法。
      title: `${t('pricing.metaTitle')} | ${brand.wordmark}`,
      description: t('pricing.metaDescription', {
        app1y: formatUsd(appEntryPrice()),
        router: formatUsd(routerOffer().firstYear),
      }),
    },
    brand,
  );
}

// kaitu 构建专属定价页（page.kaitu.tsx）：App 版四档 + 路由器版一张卡 + 定价 FAQ。
// overleap 的定价页在 page.overleap.tsx（年付 / 月付两卡）。
export default async function PricingPage({
  params,
}: {
  params: Promise<{ locale: string }>;
}) {
  const { locale: rawLocale } = await params;
  const locale = rawLocale as Locale;
  setRequestLocale(locale);
  const t = await getTranslations({ locale, namespace: 'pricing' });
  const tc = await getTranslations({ locale, namespace: 'common' });
  const appPlans = siteConfig().appPlans ?? [];
  // 套餐名沿用购买页的 common.plan.pid.*（同一个 pid 在两处叫同一个名字）。
  const planNames = Object.fromEntries(
    appPlans.map((p) => [p.pid, tc.has(`plan.pid.${p.pid}`) ? tc(`plan.pid.${p.pid}`) : t('pricing.app.plan', { years: Math.round(p.months / 12) })]),
  );
  const appLabels = {
    planNames,
    popular: t('pricing.app.popular'),
    perMonth: t.raw('pricing.app.perMonth') as string,
    origin: t.raw('pricing.app.origin') as string,
    save: t.raw('pricing.app.save') as string,
    cta: t('pricing.app.cta'),
    includes: t.raw('pricing.app.includes') as string[],
  };
  const faqItems = (t.raw('pricing.faq.items') as FaqItem[]).map((item, i) => ({
    key: `faq-${i}`,
    question: item.q,
    answer: item.a,
  }));

  const faqPageJsonLd = {
    '@context': 'https://schema.org',
    '@type': 'FAQPage',
    mainEntity: faqItems.map((i) => ({
      '@type': 'Question',
      name: i.question,
      acceptedAnswer: { '@type': 'Answer', text: i.answer },
    })),
  };

  return (
    <div className="min-h-screen bg-background">
      <script
        type="application/ld+json"
        suppressHydrationWarning
        dangerouslySetInnerHTML={{ __html: JSON.stringify(faqPageJsonLd).replace(/</g, '\\u003c') }}
      />
      <Header />

      <section className="pt-16 pb-8 px-4 sm:px-6 lg:px-8">
        <div className="max-w-5xl mx-auto text-center">
          <h1 className="text-4xl sm:text-5xl font-bold text-foreground mb-4">{t('pricing.title')}</h1>
          <p className="text-lg text-muted-foreground max-w-2xl mx-auto">{t('pricing.subtitle')}</p>
        </div>
      </section>

      <section id="app" className="py-10 px-4 sm:px-6 lg:px-8 scroll-mt-20" aria-labelledby="pricing-app-heading">
        <div className="max-w-5xl mx-auto">
          <div className="text-center mb-8">
            <h2 id="pricing-app-heading" className="text-2xl font-bold text-foreground mb-2">{t('pricing.app.title')}</h2>
            <p className="text-muted-foreground">{t('pricing.app.subtitle')}</p>
          </div>
          <AppPlansGrid initial={appPlans} labels={appLabels} />
        </div>
      </section>

      <section id="router" className="py-10 px-4 sm:px-6 lg:px-8 scroll-mt-20 bg-muted/40" aria-labelledby="pricing-router-heading">
        <div className="max-w-5xl mx-auto">
          <h2 id="pricing-router-heading" className="sr-only">{t('pricing.router.title')}</h2>
          <RouterPlanCard t={t} locale={locale} />
        </div>
      </section>

      <FAQSection sectionTitle={t('pricing.faq.title')} sectionSubtitle="" items={faqItems} />

      <Footer />
    </div>
  );
}
