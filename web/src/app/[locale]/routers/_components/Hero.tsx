import { Router } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Link } from '@/i18n/routing';
import { getTranslations } from 'next-intl/server';

export async function Hero() {
  const t = await getTranslations('routers');
  return (
    <section className="py-20 px-4 sm:px-6 lg:px-8">
      <div className="max-w-5xl mx-auto text-center">
        <Router className="w-16 h-16 text-blue-600 mx-auto mb-6" />
        <h1 className="text-4xl sm:text-5xl lg:text-6xl font-bold text-foreground mb-6">
          {t('title')}
        </h1>
        <p className="text-xl text-muted-foreground mb-10 max-w-3xl mx-auto leading-relaxed">
          {t('subtitle')}
        </p>
        <div className="flex flex-col sm:flex-row gap-3 justify-center">
          <Button asChild size="lg">
            <a href="#step-1">{t('heroCta.primary')}</a>
          </Button>
          <Button asChild size="lg" variant="outline">
            <Link href="/install?platform=router&nodownload">
              {t('heroCta.secondary')}
            </Link>
          </Button>
        </div>
      </div>
    </section>
  );
}
