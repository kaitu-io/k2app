import { Button } from '@/components/ui/button';
import { Link } from '@/i18n/routing';

export interface Step { key: string; number: string; label: string; detail: string }

export default function OverleapSteps({ title, steps, cta }: { title: string; steps: Step[]; cta: string }) {
  return (
    <section id="steps" className="py-20 px-4 sm:px-6 lg:px-8 bg-card/60">
      <div className="max-w-4xl mx-auto">
        <h2 className="text-3xl font-bold text-center mb-14">{title}</h2>
        <ol className="grid md:grid-cols-3 gap-10">
          {steps.map((s) => (
            <li key={s.key} className="flex flex-col items-center text-center">
              <div className="w-12 h-12 rounded-full bg-primary/10 border border-primary/30 flex items-center justify-center mb-5">
                <span className="text-primary font-bold">{s.number}</span>
              </div>
              <p className="font-semibold text-foreground mb-2">{s.label}</p>
              <p className="text-sm text-muted-foreground">{s.detail}</p>
            </li>
          ))}
        </ol>
        <div className="mt-12 text-center">
          <Button asChild size="lg" className="font-semibold min-w-[180px]"><Link href="/purchase">{cta}</Link></Button>
        </div>
      </div>
    </section>
  );
}
