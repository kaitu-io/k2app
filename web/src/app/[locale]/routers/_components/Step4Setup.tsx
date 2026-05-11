import { getTranslations } from 'next-intl/server';
import { Image as ImageIcon } from 'lucide-react';
import { StepShell } from './StepShell';

export async function Step4Setup() {
  const t = await getTranslations('routers');
  const wt = await getTranslations('routers.wizard.step4');
  const steps = wt.raw('steps') as { title: string; body: string }[];

  return (
    <StepShell
      id="step-4"
      number={4}
      stepLabel={t('wizard.step', { n: 4 })}
      title={wt('title')}
      subtitle={wt('subtitle')}
      background="muted"
    >
      <ol className="grid sm:grid-cols-2 gap-5 mb-8">
        {steps.map((s, i) => (
          <li
            key={i}
            className="bg-card border border-border rounded-lg p-5 flex gap-4"
          >
            <span className="flex-shrink-0 inline-flex items-center justify-center w-8 h-8 rounded-full bg-primary/15 text-primary font-semibold">
              {i + 1}
            </span>
            <div>
              <h3 className="font-semibold text-foreground mb-1">{s.title}</h3>
              <p className="text-sm text-foreground/80 leading-relaxed">{s.body}</p>
            </div>
          </li>
        ))}
      </ol>

      {/* Screenshot placeholder — replace once gateway admin UI screenshot is captured */}
      <div className="aspect-[16/9] max-w-3xl mx-auto rounded-xl border-2 border-dashed border-border bg-card flex flex-col items-center justify-center text-muted-foreground">
        <ImageIcon className="w-12 h-12 mb-2 opacity-50" />
        <p className="text-sm">{wt('screenshotPlaceholder')}</p>
      </div>
    </StepShell>
  );
}
