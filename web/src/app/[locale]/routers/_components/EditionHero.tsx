import { Router, Truck } from 'lucide-react';
import { Link } from '@/i18n/routing';
import { Button } from '@/components/ui/button';
import { formatShipsFrom, formatUsd, routerOffer } from '@/lib/router-edition';
import type { RoutersT } from './translator';

export function EditionHero({ t, locale }: { t: RoutersT; locale: string }) {
  const offer = routerOffer();
  const shipsFrom = formatShipsFrom(offer.shipsFrom, locale);

  return (
    <section className="py-20 px-4 sm:px-6 lg:px-8">
      <div className="max-w-5xl mx-auto text-center">
        <Router className="w-16 h-16 text-blue-600 mx-auto mb-6" />
        {offer.presale && (
          <p
            data-testid="presale-badge"
            className="inline-flex items-center gap-2 rounded-full border border-primary/30 bg-primary/10 px-4 py-1.5 text-sm font-medium text-primary mb-6"
          >
            <Truck className="w-4 h-4" aria-hidden />
            {t('edition.product.presaleBadge', { date: shipsFrom })}
          </p>
        )}
        <h1 className="text-4xl sm:text-5xl lg:text-6xl font-bold text-foreground mb-6">
          {t('edition.product.heroTitle')}
        </h1>
        <p className="text-xl text-muted-foreground mb-10 max-w-3xl mx-auto leading-relaxed">
          {t('edition.product.heroSubtitle')}
        </p>
        <div className="flex flex-wrap justify-center gap-4">
          <Button asChild size="lg">
            <Link href="/purchase/router">
              {offer.presale
                ? t('edition.product.presaleCta', { price: formatUsd(offer.firstYear) })
                : t('edition.product.buyNow')}
            </Link>
          </Button>
          <Button asChild size="lg" variant="outline">
            <a href="#how">{t('edition.product.howItWorksCta')}</a>
          </Button>
        </div>
      </div>
    </section>
  );
}
