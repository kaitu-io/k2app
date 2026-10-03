import { ChevronDown } from 'lucide-react';

export interface FaqItem {
  key: string;
  question: string;
  answer: string;
}

/**
 * Native <details>: answers are in the server HTML (crawlers and the FAQPage
 * JSON-LD agree with the visible page) and need no client JS.
 */
export default function Faq({ title, subtitle, items }: { title: string; subtitle: string; items: FaqItem[] }) {
  return (
    <section id="faq" className="scroll-mt-16 px-4 py-20 sm:px-6 lg:px-8" aria-labelledby="faq-heading">
      <div className="mx-auto max-w-3xl">
        <div className="mb-12 text-center">
          <h2 id="faq-heading" className="mb-3 text-3xl font-bold">{title}</h2>
          <p className="text-muted-foreground">{subtitle}</p>
        </div>
        <div className="w-full">
          {items.map((item) => (
            <details key={item.key} className="group border-b last:border-b-0">
              <summary className="flex cursor-pointer list-none items-start justify-between gap-4 py-4 text-start text-base font-medium hover:underline [&::-webkit-details-marker]:hidden">
                {item.question}
                <ChevronDown aria-hidden className="mt-1 h-4 w-4 shrink-0 text-muted-foreground transition-transform duration-200 group-open:rotate-180" />
              </summary>
              <p className="whitespace-pre-line pb-4 text-sm leading-relaxed text-muted-foreground">{item.answer}</p>
            </details>
          ))}
        </div>
      </div>
    </section>
  );
}
