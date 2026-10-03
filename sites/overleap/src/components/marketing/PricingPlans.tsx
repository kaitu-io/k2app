import { Check } from 'lucide-react';
import { Link } from '@/i18n/routing';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { PRICING } from '@/lib/site';

export interface PricingPlan {
  key: 'yearly' | 'monthly';
  name: string;
  price: string;
  period: string;
  note: string;
  featured: boolean;
}

interface Props {
  title: string;
  subtitle: string;
  plans: PricingPlan[];
  includes: string[];
  cta: string;
  currencyNote: string;
}

/** Static price table (home + /pricing). Each card preselects its plan on /purchase. */
export default function PricingPlans({ title, subtitle, plans, includes, cta, currencyNote }: Props) {
  return (
    <section id="pricing" className="scroll-mt-16 bg-card/60 px-4 py-20 sm:px-6 lg:px-8">
      <div className="mx-auto max-w-4xl">
        <div className="mb-12 text-center">
          <h2 className="mb-3 text-3xl font-bold">{title}</h2>
          <p className="text-muted-foreground">{subtitle}</p>
        </div>
        <div className="mb-10 grid gap-6 md:grid-cols-2">
          {plans.map((plan) => (
            <Card
              key={plan.key}
              data-testid={`price-card-${plan.key}`}
              className={`gap-0 bg-card p-7 ${plan.featured ? 'border-primary shadow-[0_0_0_1px_var(--primary)]' : 'border-border'}`}
            >
              <p className="mb-3 text-sm font-semibold text-muted-foreground">{plan.name}</p>
              <p className="mb-1 flex items-baseline gap-2">
                <span className="text-4xl font-bold text-foreground">{plan.price}</span>
                <span className="text-sm text-muted-foreground">{plan.period}</span>
              </p>
              <p className="mb-6 text-sm text-secondary">{plan.note}</p>
              <Button asChild className="w-full font-semibold" variant={plan.featured ? 'default' : 'outline'}>
                <Link href={{ pathname: '/purchase', query: { plan: PRICING.pids[plan.key] } }}>{cta}</Link>
              </Button>
            </Card>
          ))}
        </div>
        <ul className="mx-auto grid max-w-2xl gap-3 sm:grid-cols-2">
          {includes.map((item) => (
            <li key={item} className="flex items-center gap-2 text-sm text-foreground">
              <Check className="h-4 w-4 shrink-0 text-secondary" />
              {item}
            </li>
          ))}
        </ul>
        <p className="mt-8 text-center text-xs text-muted-foreground">{currencyNote}</p>
      </div>
    </section>
  );
}
