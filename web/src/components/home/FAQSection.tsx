import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from '@/components/ui/accordion';

export interface FAQItem {
  key: string;
  question: string;
  answer: string;
}

interface FAQSectionProps {
  sectionTitle: string;
  sectionSubtitle: string;
  items: FAQItem[];
}

export default function FAQSection({
  sectionTitle,
  sectionSubtitle,
  items,
}: FAQSectionProps) {
  return (
    <section
      id="faq"
      className="relative z-10 py-20 px-4 sm:px-6 lg:px-8"
      aria-labelledby="faq-heading"
    >
      <div className="max-w-3xl mx-auto">
        <div className="text-center mb-12">
          <h2 id="faq-heading" className="text-3xl font-bold font-mono mb-4">
            {sectionTitle}
          </h2>
          <p className="text-muted-foreground text-base leading-relaxed">
            {sectionSubtitle}
          </p>
        </div>

        <Accordion type="single" collapsible className="w-full">
          {items.map((item) => (
            <AccordionItem key={item.key} value={item.key}>
              <AccordionTrigger className="text-base font-medium text-left">
                {item.question}
              </AccordionTrigger>
              <AccordionContent className="text-sm text-muted-foreground leading-relaxed whitespace-pre-line">
                {item.answer}
              </AccordionContent>
            </AccordionItem>
          ))}
        </Accordion>
      </div>
    </section>
  );
}
