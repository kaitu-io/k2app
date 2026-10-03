import { Link } from '@/i18n/routing';
import { Button } from '@/components/ui/button';

export interface Step {
  key: string;
  number: string;
  label: string;
  detail: string;
}

export default function Steps({ title, steps, cta }: { title: string; steps: Step[]; cta: string }) {
  return (
    <section id="steps" className="bg-card/60 px-4 py-20 sm:px-6 lg:px-8">
      <div className="mx-auto max-w-4xl">
        <h2 className="mb-14 text-center text-3xl font-bold">{title}</h2>
        <ol className="grid gap-10 md:grid-cols-3">
          {steps.map((s) => (
            <li key={s.key} className="flex flex-col items-center text-center">
              <div className="mb-5 flex h-12 w-12 items-center justify-center rounded-full border border-primary/30 bg-primary/10">
                <span className="font-bold text-primary">{s.number}</span>
              </div>
              <p className="mb-2 font-semibold text-foreground">{s.label}</p>
              <p className="text-sm text-muted-foreground">{s.detail}</p>
            </li>
          ))}
        </ol>
        <div className="mt-12 text-center">
          <Button asChild size="lg" className="min-w-[180px] font-semibold">
            <Link href="/purchase">{cta}</Link>
          </Button>
        </div>
      </div>
    </section>
  );
}
