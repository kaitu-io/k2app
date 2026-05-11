'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { AlertTriangle, ExternalLink } from 'lucide-react';
import { StepShell } from './StepShell';

interface PlatformGuide {
  title: string;
  intro: string;
  steps: string[];
  resourcesLabel: string;
  resources: { label: string; url: string }[];
}

export function Step2InstallOS() {
  const t = useTranslations('routers');
  const wt = useTranslations('routers.wizard.step2');
  const [tab, setTab] = useState<'arm' | 'x86'>('arm');

  const arm = wt.raw('arm') as PlatformGuide;
  const x86 = wt.raw('x86') as PlatformGuide;
  const guide = tab === 'arm' ? arm : x86;

  return (
    <StepShell
      id="step-2"
      number={2}
      stepLabel={t('wizard.step', { n: 2 })}
      title={wt('title')}
      subtitle={wt('subtitle')}
      background="muted"
    >
      {/* Warning banner */}
      <div className="mb-6 p-4 rounded-lg bg-amber-500/10 border border-amber-500/30 flex gap-3">
        <AlertTriangle className="w-5 h-5 text-amber-600 dark:text-amber-400 shrink-0 mt-0.5" />
        <p className="text-sm text-foreground leading-relaxed">{wt('warning')}</p>
      </div>

      {/* Tabs */}
      <div className="inline-flex rounded-lg bg-card border border-border p-1 mb-6">
        {(['arm', 'x86'] as const).map((key) => (
          <button
            key={key}
            type="button"
            onClick={() => setTab(key)}
            className={`px-4 py-2 rounded-md text-sm font-medium transition-colors ${
              tab === key
                ? 'bg-primary text-primary-foreground'
                : 'text-muted-foreground hover:text-foreground'
            }`}
          >
            {wt(`tabs.${key}`)}
          </button>
        ))}
      </div>

      {/* Active guide */}
      <div className="bg-card border border-border rounded-xl p-6 sm:p-8">
        <h3 className="text-xl font-bold text-foreground mb-3">{guide.title}</h3>
        <p className="text-muted-foreground mb-6 leading-relaxed">{guide.intro}</p>

        <ol className="space-y-3 mb-6">
          {guide.steps.map((step, i) => (
            <li key={i} className="flex gap-3">
              <span className="flex-shrink-0 inline-flex items-center justify-center w-6 h-6 rounded-full bg-primary/15 text-primary text-sm font-semibold">
                {i + 1}
              </span>
              <span className="text-foreground/90 leading-relaxed pt-0.5">{step}</span>
            </li>
          ))}
        </ol>

        <div className="pt-4 border-t border-border">
          <h4 className="text-sm font-semibold text-foreground mb-2">{guide.resourcesLabel}</h4>
          <ul className="space-y-1">
            {guide.resources.map((r) => (
              <li key={r.url}>
                <a
                  href={r.url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="inline-flex items-center gap-1.5 text-sm text-primary hover:underline"
                >
                  {r.label}
                  <ExternalLink className="w-3 h-3" />
                </a>
              </li>
            ))}
          </ul>
        </div>
      </div>
    </StepShell>
  );
}
