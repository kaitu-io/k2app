'use client';

import { useState } from 'react';
import { useTranslations } from 'next-intl';
import { Copy, Check, Terminal, HelpCircle } from 'lucide-react';
import { StepShell } from './StepShell';

export function Step3InstallK2r() {
  const t = useTranslations('routers');
  const wt = useTranslations('routers.wizard.step3');
  const [copied, setCopied] = useState(false);

  const command = wt('command');
  const troubleshooting = wt.raw('troubleshooting.items') as { q: string; a: string }[];

  const onCopy = async () => {
    try {
      await navigator.clipboard.writeText(command);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      /* clipboard not available — silently ignore */
    }
  };

  return (
    <StepShell
      id="step-3"
      number={3}
      stepLabel={t('wizard.step', { n: 3 })}
      title={wt('title')}
      subtitle={wt('subtitle')}
    >
      {/* Command block */}
      <div className="bg-zinc-950 dark:bg-black rounded-xl overflow-hidden border border-zinc-800 mb-6">
        <div className="flex items-center justify-between px-4 py-2 bg-zinc-900 border-b border-zinc-800">
          <div className="flex items-center gap-2 text-zinc-400 text-sm">
            <Terminal className="w-4 h-4" />
            <span>SSH</span>
          </div>
          <button
            type="button"
            onClick={onCopy}
            className="inline-flex items-center gap-1.5 px-3 py-1 rounded-md text-xs font-medium text-zinc-300 hover:bg-zinc-800 transition-colors"
          >
            {copied ? (
              <>
                <Check className="w-3.5 h-3.5 text-green-400" />
                {wt('copiedLabel')}
              </>
            ) : (
              <>
                <Copy className="w-3.5 h-3.5" />
                {wt('copyButton')}
              </>
            )}
          </button>
        </div>
        <pre className="px-4 py-4 text-sm text-zinc-100 overflow-x-auto">
          <code>$ {command}</code>
        </pre>
      </div>

      <p className="text-foreground/80 mb-8 leading-relaxed max-w-3xl">{wt('explanation')}</p>

      {/* Troubleshooting */}
      <div>
        <h3 className="text-base font-semibold text-foreground mb-3 flex items-center gap-2">
          <HelpCircle className="w-4 h-4" />
          {wt('troubleshooting.label')}
        </h3>
        <div className="space-y-3">
          {troubleshooting.map((item, i) => (
            <details
              key={i}
              className="group rounded-lg border border-border bg-card px-4 py-3"
            >
              <summary className="cursor-pointer text-sm font-medium text-foreground hover:text-primary transition-colors list-none flex items-center justify-between">
                <span>{item.q}</span>
                <span className="text-muted-foreground group-open:rotate-180 transition-transform">
                  ▾
                </span>
              </summary>
              <p className="mt-2 text-sm text-foreground/80 leading-relaxed">{item.a}</p>
            </details>
          ))}
        </div>
      </div>
    </StepShell>
  );
}
