import { HelpCircle } from 'lucide-react';
import { formatShipsFrom, routerOffer } from '@/lib/router-edition';
import type { RoutersT } from './translator';

interface FaqItem {
  q: string;
  a: string;
}

/** t.raw 拿到的数组不走 ICU 插值，这里只把 `{date}` 换成发货日。 */
function fillDate(items: FaqItem[], date: string): FaqItem[] {
  return items.map((item) => ({ q: item.q.replaceAll('{date}', date), a: item.a.replaceAll('{date}', date) }));
}

export function EditionFAQ({ t, locale }: { t: RoutersT; locale: string }) {
  const offer = routerOffer();
  const shipsFrom = formatShipsFrom(offer.shipsFrom, locale);
  // 预售期把「预售什么时候发货」放在最前面；发售后这一条自然消失，其余条目不变。
  const presaleItems = offer.presale ? fillDate(t.raw('edition.product.presaleFaq') as FaqItem[], shipsFrom) : [];
  const items = [...presaleItems, ...(t.raw('edition.product.faq') as FaqItem[])];

  // FAQ structured data for GEO/SEO — same shape as support/page.kaitu.tsx.
  // Content comes from trusted i18n translations, safe for inline script.
  const faqJsonLd = {
    '@context': 'https://schema.org',
    '@type': 'FAQPage',
    mainEntity: items.map((item) => ({
      '@type': 'Question',
      name: item.q,
      acceptedAnswer: {
        '@type': 'Answer',
        text: item.a,
      },
    })),
  };

  return (
    <section className="py-16 px-4 sm:px-6 lg:px-8">
      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={{ __html: JSON.stringify(faqJsonLd) }}
      />
      <div className="max-w-3xl mx-auto">
        <div className="text-center mb-10">
          <HelpCircle className="w-10 h-10 text-primary mx-auto mb-3" />
          <h2 className="text-3xl font-bold text-foreground">{t('edition.product.faqTitle')}</h2>
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
