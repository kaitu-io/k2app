'use client';

/**
 * Stripe subscription checkout: pick a plan → POST /api/user/stripe/checkout →
 * same-window redirect to Stripe Checkout. Stripe prices are resolved server-side
 * (plan → stripe_price_id); entitlement lands asynchronously via webhook and the
 * success return goes to /account.
 */
import { useCallback, useEffect, useMemo, useState } from 'react';
import { useLocale, useTranslations } from 'next-intl';
import { useSearchParams } from 'next/navigation';
import { Check } from 'lucide-react';
import { Link, useRouter } from '@/i18n/routing';
import { useAuth } from '@/contexts/AuthContext';
import { api, ApiError, ErrorCode, type Plan } from '@/lib/api';
import { Button } from '@/components/ui/button';
import { Card, CardContent } from '@/components/ui/card';
import { displayCurrency, formatMinor, pickAmount, type DisplayCurrency } from '@/lib/pricing';

/** A plan's price in the display currency: API `currencyPrices` (Stripe truth) first, else `price` (USD minor units). */
function planAmount(p: Plan, currency: DisplayCurrency): { amount: number; currency: string } {
  return pickAmount(p.currencyPrices, currency) ?? { amount: p.price, currency: 'usd' };
}

export default function PurchaseClient() {
  const t = useTranslations('purchase');
  const tl = useTranslations('landing');
  const locale = useLocale();
  const currency = displayCurrency(locale);
  const { profile, loading: authLoading } = useAuth();
  const router = useRouter();
  const searchParams = useSearchParams();

  const [plans, setPlans] = useState<Plan[]>([]);
  const [plansLoading, setPlansLoading] = useState(true);
  const [selectedPid, setSelectedPid] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const cancelled = searchParams.get('checkout') === 'cancelled';
  const preselect = searchParams.get('plan');

  useEffect(() => {
    let alive = true;
    api
      .getPlans()
      .then((res) => {
        if (!alive) return;
        const appPlans = (res.items ?? []).filter((p) => (p.product ?? 'app') === 'app');
        setPlans(appPlans);
        const fallback = appPlans.find((p) => p.highlight) ?? appPlans[0];
        setSelectedPid(preselect && appPlans.some((p) => p.pid === preselect) ? preselect : (fallback?.pid ?? null));
      })
      .catch(() => {
        /* falls through to the noPlans empty state */
      })
      .finally(() => {
        if (alive) setPlansLoading(false);
      });
    return () => {
      alive = false;
    };
  }, [preselect]);

  const sorted = useMemo(() => [...plans].sort((a, b) => b.month - a.month), [plans]);
  const monthly = useMemo(() => plans.find((p) => p.month === 1), [plans]);
  // One currency for every card (pickAmount already fell back to usd when the API lacks one), so the saving compares like with like.
  const shownCurrency = (sorted[0] ? planAmount(sorted[0], currency).currency : currency).toUpperCase();
  const activeSub = profile?.subscriptions?.[0] ?? null;

  const handleSubscribe = useCallback(async () => {
    if (!selectedPid) return;
    if (!profile) {
      router.push(`/login?next=${encodeURIComponent(`/purchase?plan=${selectedPid}`)}`);
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      const { url } = await api.createStripeCheckout(selectedPid);
      window.location.assign(url);
    } catch (err) {
      setError(err instanceof ApiError && err.code === ErrorCode.ChannelUnavailable ? t('channelUnavailable') : t('genericError'));
      setSubmitting(false);
    }
  }, [selectedPid, profile, router, t]);

  if (activeSub) {
    return (
      <div className="container mx-auto max-w-2xl px-4 py-12" data-testid="purchase">
        <Card data-testid="subscribed-card">
          <CardContent className="flex flex-col items-center gap-4 py-4 text-center">
            <p className="text-lg font-medium">{t('alreadySubscribed')}</p>
            <Button asChild>
              <Link href="/account">{t('manageInAccount')}</Link>
            </Button>
          </CardContent>
        </Card>
      </div>
    );
  }

  return (
    <div className="container mx-auto max-w-2xl px-4 py-12" data-testid="purchase">
      <h1 className="mb-6 text-2xl font-bold">{t('title')}</h1>

      {cancelled && (
        <div className="mb-6 rounded-lg border border-warning/50 bg-warning/10 px-4 py-3 text-sm text-warning" data-testid="cancelled-banner">
          {t('checkoutCancelled')}
        </div>
      )}

      {plansLoading ? (
        <div className="flex justify-center py-16">
          <div className="h-8 w-8 animate-spin rounded-full border-2 border-primary border-t-transparent" />
        </div>
      ) : sorted.length === 0 ? (
        <p className="py-16 text-center text-muted-foreground">{t('noPlans')}</p>
      ) : (
        <>
          <div className="mb-6 grid gap-4 sm:grid-cols-2">
            {sorted.map((p) => {
              const isAnnual = p.month === 12;
              const selected = p.pid === selectedPid;
              const shown = planAmount(p, currency);
              const savePercent =
                isAnnual && monthly
                  ? Math.round((1 - shown.amount / (planAmount(monthly, currency).amount * 12)) * 100)
                  : null;
              return (
                <button
                  key={p.pid}
                  type="button"
                  onClick={() => setSelectedPid(p.pid)}
                  data-testid={`plan-card-${p.pid}`}
                  data-selected={selected}
                  aria-pressed={selected}
                  className={`rounded-xl border p-5 text-start transition-colors ${
                    selected ? 'border-primary ring-2 ring-primary' : 'border-border hover:border-primary/50'
                  }`}
                >
                  <div className="flex items-center justify-between">
                    <span className="font-medium">{isAnnual ? t('annualLabel') : t('monthlyLabel')}</span>
                    {savePercent != null && savePercent > 0 && (
                      <span className="rounded-full bg-primary/10 px-2 py-0.5 text-xs font-semibold text-primary">
                        {t('savePercent', { percent: savePercent })}
                      </span>
                    )}
                  </div>
                  <div className="mt-3 text-3xl font-bold" data-testid={`plan-price-${p.pid}`}>
                    {isAnnual
                      ? formatMinor(shown.amount, shown.currency, locale, { digits: 0 })
                      : t('monthlyPrice', { price: formatMinor(shown.amount, shown.currency, locale, { digits: 2 }) })}
                  </div>
                  <div className="mt-1 text-sm text-muted-foreground">
                    {isAnnual
                      ? t('perMonthApprox', { price: formatMinor(shown.amount / 12, shown.currency, locale, { digits: 2 }) })
                      : t('cancelAnytime')}
                  </div>
                </button>
              );
            })}
          </div>

          {error && (
            <div role="alert" className="mb-4 rounded-lg border border-destructive/50 bg-destructive/10 px-4 py-3 text-sm text-destructive">
              {error}
            </div>
          )}

          <Button
            className="w-full"
            size="lg"
            disabled={submitting || authLoading || !selectedPid}
            onClick={handleSubscribe}
            data-testid="subscribe-btn"
          >
            {submitting ? t('redirecting') : t('subscribe')}
          </Button>
          <p className="mt-3 text-center text-xs text-muted-foreground">{t('currencyNote', { currency: shownCurrency })}</p>

          <Card className="mt-10" data-testid="plan-includes">
            <CardContent>
              <h2 className="mb-4 text-lg font-bold text-foreground sm:text-xl">{tl('pricing.title')}</h2>
              <ul className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                {(tl.raw('pricing.includes') as string[]).map((item) => (
                  <li key={item} className="flex items-center gap-2 text-sm text-foreground">
                    <Check className="h-4 w-4 shrink-0 text-secondary" />
                    {item}
                  </li>
                ))}
              </ul>
            </CardContent>
          </Card>
        </>
      )}
    </div>
  );
}
