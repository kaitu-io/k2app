import { getTranslations } from 'next-intl/server';
import { Link } from '@/i18n/routing';
import { StepShell } from './StepShell';

export async function Step4Setup() {
  const t = await getTranslations('routers');
  const wt = await getTranslations('routers.wizard.step4');
  const steps = wt.raw('steps') as { title: string; body: string; href?: string }[];

  return (
    <StepShell
      id="step-4"
      number={4}
      stepLabel={t('wizard.step', { n: 4 })}
      title={wt('title')}
      subtitle={wt('subtitle')}
      background="muted"
    >
      <ol className="grid sm:grid-cols-2 gap-5">
        {steps.map((s, i) => (
          <li
            key={i}
            className="bg-card border border-border rounded-lg p-5 flex gap-4"
          >
            <span className="flex-shrink-0 inline-flex items-center justify-center w-8 h-8 rounded-full bg-primary/15 text-primary font-semibold">
              {i + 1}
            </span>
            <div>
              <h3 className="font-semibold text-foreground mb-1">
                {s.href ? (
                  <Link href={s.href} className="text-primary underline underline-offset-4 hover:text-primary/80">
                    {s.title}
                  </Link>
                ) : (
                  s.title
                )}
              </h3>
              <p className="text-sm text-foreground/80 leading-relaxed">{s.body}</p>
            </div>
          </li>
        ))}
      </ol>
    </StepShell>
  );
}
