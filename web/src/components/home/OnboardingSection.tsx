import { Link } from '@/i18n/routing';
import { Button } from '@/components/ui/button';

export interface OnboardingStep {
  key: string;
  number: string;
  label: string;
  detail: string;
}

interface OnboardingSectionProps {
  title: string;
  steps: OnboardingStep[];
  ctaText: string;
}

export default function OnboardingSection({ title, steps, ctaText }: OnboardingSectionProps) {
  return (
    <section className="relative z-10 py-20 px-4 sm:px-6 lg:px-8 bg-[rgba(5,5,8,0.6)] backdrop-blur-sm">
      <div className="max-w-4xl mx-auto">
        <div className="text-center mb-14">
          <h2 className="text-3xl font-bold font-mono">{title}</h2>
        </div>
        <div className="grid md:grid-cols-3 gap-10">
          {steps.map((step) => (
            <div key={step.key} className="flex flex-col items-center text-center">
              <div className="w-12 h-12 rounded-full bg-primary/10 border border-primary/30 flex items-center justify-center mb-5">
                <span className="text-primary font-mono font-bold">{step.number}</span>
              </div>
              <p className="font-semibold text-foreground mb-2">{step.label}</p>
              <p className="text-sm text-muted-foreground">{step.detail}</p>
            </div>
          ))}
        </div>
        <div className="mt-12 text-center">
          <Button asChild size="lg" className="font-bold font-mono min-w-[180px]">
            <Link href="/purchase">{ctaText}</Link>
          </Button>
        </div>
      </div>
    </section>
  );
}
