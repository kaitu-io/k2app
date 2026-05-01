import { getTranslations } from 'next-intl/server';
import { HelpCircle } from 'lucide-react';

export async function RoutersFAQ() {
  const t = await getTranslations('routers.faq');
  const items = t.raw('items') as { q: string; a: string }[];

  return (
    <section className="py-16 px-4 sm:px-6 lg:px-8">
      <div className="max-w-3xl mx-auto">
        <div className="text-center mb-10">
          <HelpCircle className="w-10 h-10 text-primary mx-auto mb-3" />
          <h2 className="text-3xl font-bold text-foreground">{t('title')}</h2>
        </div>
        <div className="space-y-3">
          {items.map((item, i) => (
            <details
              key={i}
              className="group rounded-lg border border-border bg-card px-5 py-4"
            >
              <summary className="cursor-pointer text-base font-semibold text-foreground hover:text-primary transition-colors list-none flex items-center justify-between gap-4">
                <span>{item.q}</span>
                <span className="text-muted-foreground group-open:rotate-180 transition-transform shrink-0">
                  ▾
                </span>
              </summary>
              <p className="mt-3 text-foreground/80 leading-relaxed">{item.a}</p>
            </details>
          ))}
        </div>
      </div>
    </section>
  );
}
