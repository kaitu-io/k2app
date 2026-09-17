import { getTranslations } from 'next-intl/server';
import { PackageCheck, Cable, Wifi } from 'lucide-react';

export async function EditionHowItWorks() {
  const t = await getTranslations('routers');

  const steps = [
    { Icon: PackageCheck, title: t('edition.product.how1Title'), body: t('edition.product.how1Body') },
    { Icon: Cable, title: t('edition.product.how2Title'), body: t('edition.product.how2Body') },
    { Icon: Wifi, title: t('edition.product.how3Title'), body: t('edition.product.how3Body') },
  ];

  return (
    <section id="how" className="py-16 px-4 sm:px-6 lg:px-8 bg-muted scroll-mt-20">
      <div className="max-w-5xl mx-auto">
        <h2 className="text-3xl font-bold text-foreground text-center mb-10">
          {t('edition.product.howTitle')}
        </h2>
        <div className="grid sm:grid-cols-3 gap-6">
          {steps.map((s, i) => (
            <div key={i} className="bg-card border border-border rounded-xl p-6 text-center">
              <span className="inline-flex items-center justify-center w-12 h-12 rounded-full bg-primary/15 text-primary mb-4">
                <s.Icon className="w-6 h-6" />
              </span>
              <h3 className="font-semibold text-foreground mb-2">{s.title}</h3>
              <p className="text-sm text-muted-foreground leading-relaxed">{s.body}</p>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}
