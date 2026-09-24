import type { getTranslations } from 'next-intl/server';
import { CheckCircle, Router } from 'lucide-react';
import { Link } from '@/i18n/routing';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { formatShipsFrom, formatUsd, routerOffer } from '@/lib/router-edition';

type PricingT = Awaited<ReturnType<typeof getTranslations<'pricing'>>>;

/** 定价页的路由器版一张卡：价格与预售状态同 /routers（routerOffer 按日期切换）。
 *  同步组件：翻译器由页面 await 后传入，页面树里不再有嵌套的 async 组件。 */
export default function RouterPlanCard({ t, locale }: { t: PricingT; locale: string }) {
  const offer = routerOffer();
  const includes = t.raw('pricing.router.includes') as string[];

  return (
    <Card className="p-6 sm:p-8 grid gap-6 md:grid-cols-[1fr_auto] md:items-center" data-testid="router-plan" data-presale={offer.presale ? 'true' : 'false'}>
      <div>
        <div className="flex items-center gap-2 mb-2">
          <Router className="w-5 h-5 text-blue-600" aria-hidden />
          <h3 className="text-xl font-bold text-foreground">{t('pricing.router.title')}</h3>
          {offer.presale && (
            <span className="rounded-full bg-primary/10 px-2.5 py-0.5 text-xs font-semibold text-primary">
              {t('pricing.router.presaleBadge', { date: formatShipsFrom(offer.shipsFrom, locale) })}
            </span>
          )}
        </div>
        <p className="text-muted-foreground mb-4">{t('pricing.router.subtitle')}</p>
        <ul className="grid gap-2 sm:grid-cols-3">
          {includes.map((item) => (
            <li key={item} className="flex items-start gap-2 text-sm text-foreground">
              <CheckCircle className="w-4 h-4 text-green-600 mt-0.5 shrink-0" />
              {item}
            </li>
          ))}
        </ul>
      </div>
      <div className="md:text-right md:min-w-56">
        <p className="flex items-baseline gap-2 md:justify-end">
          <span className="text-3xl font-bold text-foreground">{t('pricing.router.firstYear', { price: formatUsd(offer.firstYear) })}</span>
          {offer.originFirstYear !== undefined && (
            <s className="text-sm text-muted-foreground" aria-label={t('pricing.router.origin', { price: formatUsd(offer.originFirstYear) })}>
              {formatUsd(offer.originFirstYear)}
            </s>
          )}
        </p>
        <p className="text-sm text-muted-foreground">{t('pricing.router.firstYearIncludes')}</p>
        <p className="text-sm text-muted-foreground">{t('pricing.router.renewal', { price: formatUsd(offer.renewal) })}</p>
        {offer.presale && <p className="text-xs text-foreground/80 mt-2">{t('pricing.router.presaleNote')}</p>}
        <Button asChild className="mt-4 w-full md:w-auto font-semibold">
          <Link href="/routers">{t('pricing.router.cta')}</Link>
        </Button>
      </div>
    </Card>
  );
}
