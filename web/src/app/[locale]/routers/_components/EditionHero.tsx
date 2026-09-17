import { getTranslations } from 'next-intl/server';
import { Router } from 'lucide-react';
import { Link } from '@/i18n/routing';
import { Button } from '@/components/ui/button';

export async function EditionHero() {
  const t = await getTranslations('routers');

  return (
    <section className="py-20 px-4 sm:px-6 lg:px-8">
      <div className="max-w-5xl mx-auto text-center">
        <Router className="w-16 h-16 text-blue-600 mx-auto mb-6" />
        <h1 className="text-4xl sm:text-5xl lg:text-6xl font-bold text-foreground mb-6">
          {t('edition.product.heroTitle')}
        </h1>
        <p className="text-xl text-muted-foreground mb-10 max-w-3xl mx-auto leading-relaxed">
          {t('edition.product.heroSubtitle')}
        </p>
        <div className="flex flex-wrap justify-center gap-4">
          <Button asChild size="lg">
            <Link href="/purchase/router">{t('edition.product.buyNow')}</Link>
          </Button>
          <Button asChild size="lg" variant="outline">
            <a href="#how">{t('edition.product.howItWorksCta')}</a>
          </Button>
        </div>
      </div>
    </section>
  );
}
