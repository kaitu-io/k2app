import { getTranslations } from 'next-intl/server';
import { Card } from '@/components/ui/card';
import { Router, Smartphone, CheckCircle } from 'lucide-react';

export async function VsClient() {
  const t = await getTranslations('routers.vsClient');
  const routerItems = t.raw('router.items') as string[];
  const clientItems = t.raw('client.items') as string[];

  return (
    <section className="py-16 px-4 sm:px-6 lg:px-8">
      <div className="max-w-7xl mx-auto">
        <div className="text-center mb-10">
          <h2 className="text-3xl font-bold text-foreground mb-3">{t('title')}</h2>
          <p className="text-muted-foreground text-lg">{t('subtitle')}</p>
        </div>

        <div className="grid lg:grid-cols-2 gap-6 max-w-5xl mx-auto">
          <Card className="p-7 border-2 border-green-500 bg-green-50 dark:bg-green-900/20">
            <div className="flex items-center mb-5">
              <Router className="w-9 h-9 text-green-600 mr-3" />
              <div>
                <h3 className="text-lg font-bold text-green-800 dark:text-green-300">
                  {t('router.title')}
                </h3>
                <p className="text-sm text-green-600 dark:text-green-400">{t('router.tag')}</p>
              </div>
            </div>
            <ul className="space-y-2.5">
              {routerItems.map((item, i) => (
                <li key={i} className="flex items-start text-foreground/90">
                  <CheckCircle className="w-5 h-5 text-green-600 mr-2 mt-0.5 flex-shrink-0" />
                  <span>{item}</span>
                </li>
              ))}
            </ul>
          </Card>

          <Card className="p-7">
            <div className="flex items-center mb-5">
              <Smartphone className="w-9 h-9 text-muted-foreground mr-3" />
              <div>
                <h3 className="text-lg font-bold text-foreground">{t('client.title')}</h3>
                <p className="text-sm text-muted-foreground">{t('client.tag')}</p>
              </div>
            </div>
            <ul className="space-y-2.5">
              {clientItems.map((item, i) => (
                <li key={i} className="flex items-start text-muted-foreground">
                  <span className="w-5 mr-2 mt-0.5 flex-shrink-0 text-center">·</span>
                  <span>{item}</span>
                </li>
              ))}
            </ul>
          </Card>
        </div>
      </div>
    </section>
  );
}
