import { ReactNode } from 'react';

/**
 * Visual scaffolding for a single wizard step. Renders the step number badge,
 * title, optional subtitle, and the step's body content.
 */
export function StepShell({
  id,
  number,
  stepLabel,
  title,
  subtitle,
  children,
  background,
}: {
  id: string;
  number: number;
  stepLabel: string;
  title: string;
  subtitle?: string;
  children: ReactNode;
  background?: 'card' | 'muted';
}) {
  return (
    <section
      id={id}
      className={`py-16 px-4 sm:px-6 lg:px-8 scroll-mt-20 ${
        background === 'muted' ? 'bg-muted' : ''
      }`}
    >
      <div className="max-w-7xl mx-auto">
        <div className="mb-10">
          <div className="flex items-center gap-3 mb-3">
            <span className="inline-flex items-center justify-center w-10 h-10 rounded-full bg-primary text-primary-foreground font-bold text-lg">
              {number}
            </span>
            <span className="text-sm font-medium text-muted-foreground uppercase tracking-wide">
              {stepLabel}
            </span>
          </div>
          <h2 className="text-3xl sm:text-4xl font-bold text-foreground mb-3">{title}</h2>
          {subtitle && (
            <p className="text-lg text-muted-foreground max-w-3xl leading-relaxed">{subtitle}</p>
          )}
        </div>
        {children}
      </div>
    </section>
  );
}
