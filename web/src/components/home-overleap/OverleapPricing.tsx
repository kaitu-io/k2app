import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { Link } from '@/i18n/routing';
import { Check } from 'lucide-react';

export interface PricingPlan { key: string; name: string; price: string; period: string; note: string; featured: boolean }

interface Props { title: string; subtitle: string; plans: PricingPlan[]; includes: string[]; cta: string; currencyNote?: string }

export default function OverleapPricing({ title, subtitle, plans, includes, cta, currencyNote }: Props) {
  return (
    <section id="pricing" className="py-20 px-4 sm:px-6 lg:px-8 bg-card/60">
      <div className="max-w-4xl mx-auto">
        <div className="text-center mb-12">
          <h2 className="text-3xl font-bold mb-3">{title}</h2>
          <p className="text-muted-foreground">{subtitle}</p>
        </div>
        <div className="grid md:grid-cols-2 gap-6 mb-10">
          {plans.map((plan) => (
            <Card key={plan.key} className={`p-7 bg-card ${plan.featured ? 'border-primary shadow-[0_0_0_1px_var(--primary)]' : 'border-border'}`}>
              <p className="text-sm font-semibold text-muted-foreground mb-3">{plan.name}</p>
              <p className="flex items-baseline gap-2 mb-1">
                <span className="text-4xl font-bold text-foreground">{plan.price}</span>
                <span className="text-sm text-muted-foreground">{plan.period}</span>
              </p>
              <p className="text-sm text-secondary mb-6">{plan.note}</p>
              <Button asChild className="w-full font-semibold" variant={plan.featured ? 'default' : 'outline'}>
                <Link href="/purchase">{cta}</Link>
              </Button>
            </Card>
          ))}
        </div>
        <ul className="grid sm:grid-cols-2 gap-3 max-w-2xl mx-auto">
          {includes.map((item) => (
            <li key={item} className="flex items-center gap-2 text-sm text-foreground">
              <Check className="w-4 h-4 text-secondary shrink-0" />{item}
            </li>
          ))}
        </ul>
        {currencyNote && (
          <p className="mt-8 text-center text-xs text-muted-foreground">{currencyNote}</p>
        )}
      </div>
    </section>
  );
}
