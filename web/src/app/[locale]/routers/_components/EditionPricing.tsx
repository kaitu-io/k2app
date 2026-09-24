import { CheckCircle, Router, Smartphone } from 'lucide-react';
import { Link } from '@/i18n/routing';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { formatShipsFrom, formatUsd, routerOffer } from '@/lib/router-edition';
import type { RoutersT } from './translator';

export function EditionPricing({ t, locale }: { t: RoutersT; locale: string }) {
  const offer = routerOffer();
  const shipsFrom = formatShipsFrom(offer.shipsFrom, locale);

  const firstYear = formatUsd(offer.firstYear);
  const renewal = formatUsd(offer.renewal);
  const includes = [
    t('edition.product.includes1'),
    t('edition.product.includes2'),
    t('edition.product.includes3'),
    t('edition.product.includes4'),
  ];

  return (
    <section id="pricing" className="py-16 px-4 sm:px-6 lg:px-8 scroll-mt-20">
      <div className="max-w-3xl mx-auto">
        <h2 className="text-3xl font-bold text-foreground text-center mb-10">
          {t('edition.product.pricingTitle')}
        </h2>

        <Card className="p-8" data-presale={offer.presale ? 'true' : 'false'}>
          <div className="text-center mb-6">
            {offer.presale && (
              <p className="inline-block rounded-full bg-primary/10 px-3 py-1 text-xs font-semibold text-primary mb-3">
                {t('edition.product.presaleBadge', { date: shipsFrom })}
              </p>
            )}
            <div className="flex items-baseline justify-center gap-3">
              <span className="text-4xl font-black text-foreground">
                {t('edition.product.firstYear', { price: firstYear })}
              </span>
              {offer.originFirstYear !== undefined && (
                <s
                  className="text-lg text-muted-foreground"
                  aria-label={t('edition.product.originPrice', { price: formatUsd(offer.originFirstYear) })}
                >
                  {formatUsd(offer.originFirstYear)}
                </s>
              )}
            </div>
            <p className="text-muted-foreground mt-1">{t('edition.product.firstYearIncludes')}</p>
            <p className="text-sm text-muted-foreground mt-2">
              {t('edition.product.renewal', { price: renewal })}
            </p>
            <p className="text-sm text-foreground/80 mt-3">
              {offer.presale
                ? t('edition.product.presaleNote', { date: shipsFrom })
                : t('edition.product.serviceStartsNote')}
            </p>
          </div>

          <ul className="space-y-2.5 mb-8 max-w-md mx-auto">
            {includes.map((item, i) => (
              <li key={i} className="flex items-start text-foreground/90">
                <CheckCircle className="w-5 h-5 text-green-600 mr-2 mt-0.5 flex-shrink-0" />
                <span>{item}</span>
              </li>
            ))}
          </ul>

          <div className="flex flex-col items-center gap-3">
            <Button asChild size="lg" className="w-full max-w-xs">
              <Link href="/purchase/router">
                {offer.presale
                  ? t('edition.product.presaleCta', { price: firstYear })
                  : t('edition.product.buyNow')}
              </Link>
            </Button>
          </div>
        </Card>

        {/* 对比段：为什么大多数家庭该选路由器版而不是逐设备装 App */}
        <div className="mt-12">
          <h3 className="text-xl font-bold text-foreground text-center mb-6">
            {t('edition.product.compareTitle')}
          </h3>
          <div className="grid sm:grid-cols-2 gap-4">
            <Card className="p-5 flex items-start gap-3">
              <Smartphone className="w-6 h-6 text-muted-foreground mt-0.5 flex-shrink-0" />
              <p className="text-sm text-foreground/80 leading-relaxed">{t('edition.product.compareApp')}</p>
            </Card>
            <Card className="p-5 flex items-start gap-3 border-2 border-green-500 bg-green-50 dark:bg-green-900/20">
              <Router className="w-6 h-6 text-green-600 mt-0.5 flex-shrink-0" />
              <p className="text-sm text-foreground/90 leading-relaxed">{t('edition.product.compareRouter')}</p>
            </Card>
          </div>
        </div>
      </div>
    </section>
  );
}
