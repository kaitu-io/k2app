'use client';

/**
 * 本品牌网页购买面板（Stripe 订阅制）。
 *
 * 另一品牌的 WordGate 流在 PurchaseClient.tsx，两者由 page.tsx 按构建期品牌分流。
 * Stripe 价格解析在服务端（plan → stripe_price_id），本组件只负责：
 * 选套餐 → POST /api/user/stripe/checkout → 同窗口跳 Stripe Checkout。
 * 权益经 webhook 异步入账，success 回跳落在 /account（见 OverleapAccountClient）。
 */
import { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslations } from 'next-intl';
import { useSearchParams } from 'next/navigation';
import { Link } from '@/i18n/routing';
import { useAuth } from '@/contexts/AuthContext';
import { api, ApiError, type Plan, type User } from '@/lib/api';
import { redirectToLogin } from '@/lib/auth';
import { Button } from '@/components/ui/button';
import { Card, CardContent } from '@/components/ui/card';
import MembershipBenefits from '@/components/MembershipBenefits';

const ERROR_CHANNEL_UNAVAILABLE = 405001;

function formatEur(cents: number, digits: number): string {
  return `€${(cents / 100).toFixed(digits)}`;
}

export default function OverleapPurchaseClient() {
  const t = useTranslations('purchase');
  const { isAuthenticated } = useAuth();
  const searchParams = useSearchParams();

  const [plans, setPlans] = useState<Plan[]>([]);
  const [plansLoading, setPlansLoading] = useState(true);
  const [profile, setProfile] = useState<User | null>(null);
  const [selectedPid, setSelectedPid] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const cancelled = searchParams.get('checkout') === 'cancelled';

  useEffect(() => {
    let alive = true;
    api
      .getPlans({ autoRedirectToAuth: false })
      .then((res) => {
        if (!alive) return;
        const appPlans = res.items.filter((p) => (p.product ?? 'app') === 'app');
        setPlans(appPlans);
        const preselect = searchParams.get('plan');
        const fallback = appPlans.find((p) => p.highlight) ?? appPlans[0];
        setSelectedPid(
          preselect && appPlans.some((p) => p.pid === preselect) ? preselect : (fallback?.pid ?? null)
        );
      })
      .catch(() => {
        /* 拉取失败落入 noPlans 空态 */
      })
      .finally(() => {
        if (alive) setPlansLoading(false);
      });
    return () => {
      alive = false;
    };
    // 只在首载取套餐；plan 预选参数在同一次解析
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    if (!isAuthenticated) {
      setProfile(null);
      return;
    }
    let alive = true;
    api
      .getUserProfile({ autoRedirectToAuth: false })
      .then((u) => {
        if (alive) setProfile(u);
      })
      .catch(() => {
        /* 档案取不到不阻塞购买 */
      });
    return () => {
      alive = false;
    };
  }, [isAuthenticated]);

  const sorted = useMemo(() => [...plans].sort((a, b) => b.month - a.month), [plans]);
  const monthly = useMemo(() => plans.find((p) => p.month === 1), [plans]);
  const activeSub = profile?.subscriptions?.[0] ?? null;

  const handleSubscribe = useCallback(async () => {
    if (!selectedPid) return;
    if (!isAuthenticated) {
      redirectToLogin(`/purchase?plan=${selectedPid}`);
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      const { url } = await api.createStripeCheckout(selectedPid, { autoRedirectToAuth: false });
      window.location.assign(url);
    } catch (err) {
      setError(
        err instanceof ApiError && err.code === ERROR_CHANNEL_UNAVAILABLE
          ? t('stripe.channelUnavailable')
          : t('stripe.genericError')
      );
      setSubmitting(false);
    }
  }, [selectedPid, isAuthenticated, t]);

  if (activeSub) {
    return (
      <div className="container mx-auto max-w-2xl px-4 py-12" data-testid="overleap-purchase">
        <Card data-testid="subscribed-card">
          <CardContent className="flex flex-col items-center gap-4 py-10 text-center">
            <p className="text-lg font-medium">{t('stripe.alreadySubscribed')}</p>
            <Button asChild>
              <Link href="/account">{t('stripe.manageInAccount')}</Link>
            </Button>
          </CardContent>
        </Card>
      </div>
    );
  }

  return (
    <div className="container mx-auto max-w-2xl px-4 py-12" data-testid="overleap-purchase">
      <h1 className="mb-6 text-2xl font-bold">{t('stripe.title')}</h1>

      {cancelled && (
        <div
          className="mb-6 rounded-lg border border-amber-300 bg-amber-50 px-4 py-3 text-sm text-amber-900 dark:border-amber-700 dark:bg-amber-950 dark:text-amber-200"
          data-testid="cancelled-banner"
        >
          {t('stripe.checkoutCancelled')}
        </div>
      )}

      {plansLoading ? (
        <div className="flex justify-center py-16">
          <div className="h-8 w-8 animate-spin rounded-full border-2 border-primary border-t-transparent" />
        </div>
      ) : sorted.length === 0 ? (
        <p className="py-16 text-center text-muted-foreground">{t('stripe.noPlans')}</p>
      ) : (
        <>
          <div className="mb-6 grid gap-4 sm:grid-cols-2">
            {sorted.map((p) => {
              const isAnnual = p.month === 12;
              const selected = p.pid === selectedPid;
              const savePercent =
                isAnnual && monthly
                  ? Math.round((1 - p.price / (monthly.price * 12)) * 100)
                  : null;
              return (
                <button
                  key={p.pid}
                  type="button"
                  onClick={() => setSelectedPid(p.pid)}
                  data-testid={`plan-card-${p.pid}`}
                  data-selected={selected}
                  className={`rounded-xl border p-5 text-left transition-colors ${
                    selected ? 'border-primary ring-2 ring-primary' : 'border-border hover:border-primary/50'
                  }`}
                >
                  <div className="flex items-center justify-between">
                    <span className="font-medium">
                      {isAnnual ? t('stripe.annualLabel') : t('stripe.monthlyLabel')}
                    </span>
                    {savePercent != null && savePercent > 0 && (
                      <span className="rounded-full bg-primary/10 px-2 py-0.5 text-xs font-semibold text-primary">
                        {t('stripe.savePercent', { percent: savePercent })}
                      </span>
                    )}
                  </div>
                  <div className="mt-3 text-3xl font-bold">
                    {isAnnual ? formatEur(p.price, 0) : t('stripe.monthlyPrice', { price: formatEur(p.price, 2) })}
                  </div>
                  <div className="mt-1 text-sm text-muted-foreground">
                    {isAnnual
                      ? t('stripe.perMonthApprox', { price: formatEur(p.price / 12, 2) })
                      : t('stripe.cancelAnytime')}
                  </div>
                </button>
              );
            })}
          </div>

          {error && (
            <div className="mb-4 rounded-lg border border-destructive/50 bg-destructive/10 px-4 py-3 text-sm text-destructive">
              {error}
            </div>
          )}

          <Button
            className="w-full"
            size="lg"
            disabled={submitting || !selectedPid}
            onClick={handleSubscribe}
            data-testid="subscribe-btn"
          >
            {submitting ? t('stripe.redirecting') : t('stripe.subscribe')}
          </Button>
          <p className="mt-3 text-center text-xs text-muted-foreground">{t('stripe.currencyNote')}</p>

          <div className="mt-10">
            <MembershipBenefits />
          </div>
        </>
      )}
    </div>
  );
}
