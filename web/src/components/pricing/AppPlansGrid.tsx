'use client';

/**
 * 定价页 App 版套餐卡。服务端先用 lib/site 的静态快照渲染（SEO / 首屏有价格），
 * 挂载后拉一次 /api/plans，按 pid 用线上价覆盖——真相源是 Center 的 plans 表。
 * 拉取失败保持快照，不弹错：定价页是营销页，不是结账页。
 *
 * 文案全部由服务端页面翻译好再传进来（含 `{price}` / `{percent}` 模板），本组件不碰 next-intl。
 */
import { useEffect, useState } from 'react';
import { CheckCircle } from 'lucide-react';
import { Link } from '@/i18n/routing';
import { Button } from '@/components/ui/button';
import { Card } from '@/components/ui/card';
import { api } from '@/lib/api';
import type { AppPlanSnapshot } from '@/lib/site/types';
import { formatUsd } from '@/lib/router-edition';

export interface AppPlansLabels {
  /** pid → 套餐名（如「2年套餐」）。 */
  planNames: Record<string, string>;
  popular: string;
  /** 含 `{price}`。 */
  perMonth: string;
  /** 含 `{price}`。 */
  origin: string;
  /** 含 `{percent}`。 */
  save: string;
  cta: string;
  includes: string[];
}

interface Props {
  initial: AppPlanSnapshot[];
  labels: AppPlansLabels;
}

function fill(template: string, values: Record<string, string | number>): string {
  return template.replace(/\{(\w+)\}/g, (_, k) => String(values[k] ?? `{${k}}`));
}

/** 用线上套餐覆盖快照：pid 命中才覆盖价格 / 原价 / 高亮，线上没有的 pid 保留快照。 */
export function mergeLivePlans(
  snapshot: AppPlanSnapshot[],
  live: Array<{ pid: string; price: number; originPrice: number; month: number; highlight: boolean }>,
): AppPlanSnapshot[] {
  const byPid = new Map(live.map((p) => [p.pid, p]));
  return snapshot.map((s) => {
    const l = byPid.get(s.pid);
    return l ? { ...s, price: l.price, originPrice: l.originPrice, months: l.month, highlight: l.highlight } : s;
  });
}

export default function AppPlansGrid({ initial, labels }: Props) {
  const [plans, setPlans] = useState<AppPlanSnapshot[]>(initial);

  useEffect(() => {
    let cancelled = false;
    api
      .getPlans({ autoRedirectToAuth: false })
      .then((data) => {
        if (cancelled || !Array.isArray(data.items) || data.items.length === 0) return;
        setPlans((prev) => mergeLivePlans(prev, data.items));
      })
      .catch((err) => {
        console.warn('[Pricing] live plans unavailable, keeping snapshot:', err);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <div>
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4" data-testid="app-plans">
        {plans
          .slice()
          .sort((a, b) => a.months - b.months)
          .map((plan) => {
            const perMonth = formatUsd(Math.round(plan.price / plan.months));
            const discount = plan.originPrice > plan.price;
            const percent = discount ? Math.round((1 - plan.price / plan.originPrice) * 100) : 0;
            return (
              <Card
                key={plan.pid}
                data-pid={plan.pid}
                data-highlight={plan.highlight ? 'true' : 'false'}
                className={`relative flex flex-col p-6 ${plan.highlight ? 'border-primary shadow-[0_0_0_1px_var(--primary)]' : 'border-border'}`}
              >
                {plan.highlight && (
                  <span className="absolute -top-3 left-1/2 -translate-x-1/2 rounded-full bg-primary px-3 py-0.5 text-xs font-semibold text-primary-foreground">
                    {labels.popular}
                  </span>
                )}
                <p className="text-sm font-semibold text-muted-foreground mb-3">
                  {labels.planNames[plan.pid] ?? plan.pid}
                </p>
                <p className="flex items-baseline gap-2 mb-1">
                  <span className="text-3xl font-bold text-foreground">{formatUsd(plan.price)}</span>
                  {discount && (
                    <s className="text-sm text-muted-foreground" aria-label={fill(labels.origin, { price: formatUsd(plan.originPrice) })}>
                      {formatUsd(plan.originPrice)}
                    </s>
                  )}
                </p>
                <p className="text-sm text-muted-foreground mb-1">{fill(labels.perMonth, { price: perMonth })}</p>
                {discount && (
                  <p className="text-xs font-semibold text-primary mb-4">{fill(labels.save, { percent })}</p>
                )}
                <Button asChild className="mt-auto w-full font-semibold" variant={plan.highlight ? 'default' : 'outline'}>
                  <Link href="/purchase">{labels.cta}</Link>
                </Button>
              </Card>
            );
          })}
      </div>
      <ul className="mt-8 grid gap-3 sm:grid-cols-2 max-w-2xl mx-auto">
        {labels.includes.map((item) => (
          <li key={item} className="flex items-start gap-2 text-sm text-foreground">
            <CheckCircle className="w-4 h-4 text-green-600 mt-0.5 shrink-0" />
            {item}
          </li>
        ))}
      </ul>
    </div>
  );
}
