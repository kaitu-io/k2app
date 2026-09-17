'use client';

import { useTranslations } from 'next-intl';
import { Terminal, HelpCircle } from 'lucide-react';
import { Link } from '@/i18n/routing';
import { StepShell } from './StepShell';

export function Step3InstallK2r() {
  const t = useTranslations('routers');
  const wt = useTranslations('routers.wizard.step3');

  // 生产 k2r 是无面板构建：真实命令带着这台账户专属的凭证，只能在「我的路由器」
  // 生成后复制——这里展示的是占位形态，不是可以直接照抄运行的完整命令，所以不给
  // 复制按钮，并标注「示例」。
  const command = t('edition.diy.commandPlaceholder');
  const troubleshooting = wt.raw('troubleshooting.items') as { q: string; a: string }[];

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
          <span
            data-testid="k2r-command-example-label"
            className="px-2 py-0.5 rounded border border-zinc-700 text-xs font-medium text-zinc-400"
          >
            {wt('exampleLabel')}
          </span>
        </div>
        <pre className="px-4 py-4 text-sm text-zinc-100 overflow-x-auto">
          <code>$ {command}</code>
        </pre>
      </div>

      <p className="text-sm text-muted-foreground mb-6 max-w-3xl">
        <Link href="/account/router" className="text-primary underline underline-offset-4 hover:text-primary/80">
          {t('edition.diy.commandHint')}
        </Link>
      </p>

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
