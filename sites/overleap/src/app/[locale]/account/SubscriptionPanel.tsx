'use client';

/**
 * Subscription status + "Manage" (Stripe portal / App Store) + post-checkout activation.
 *
 * The Stripe webhook books the subscription asynchronously, so a return to
 * /account?checkout=success may arrive before it exists: poll the profile
 * (3 s × 10) in one async chain, then fall back to "refresh shortly".
 */
import { useCallback, useEffect, useState } from 'react';
import { useLocale, useTranslations } from 'next-intl';
import { useSearchParams } from 'next/navigation';
import { Link } from '@/i18n/routing';
import { useAuth } from '@/contexts/AuthContext';
import { api } from '@/lib/api';
import { errorMessage } from '@/lib/api-errors';
import { Button } from '@/components/ui/button';

const POLL_INTERVAL_MS = 3000;
const POLL_MAX_ATTEMPTS = 10;
const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

function Panel({ children, testId }: { children: React.ReactNode; testId: string }) {
  return (
    <section data-testid={testId} className="rounded-xl border bg-card p-6">
      {children}
    </section>
  );
}

export default function SubscriptionPanel() {
  const t = useTranslations('account');
  const tErr = useTranslations('errors');
  const locale = useLocale();
  const fromCheckout = useSearchParams().get('checkout') === 'success';
  const { profile, refresh } = useAuth();
  const sub = profile?.subscriptions?.[0] ?? null;

  const [pollExhausted, setPollExhausted] = useState(false);
  const [managing, setManaging] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!fromCheckout || sub) return;
    let cancelled = false;
    (async () => {
      for (let i = 0; i < POLL_MAX_ATTEMPTS; i++) {
        await sleep(POLL_INTERVAL_MS);
        if (cancelled) return;
        const p = await refresh();
        if (p?.subscriptions?.length) return;
      }
      if (!cancelled) setPollExhausted(true);
    })();
    return () => {
      cancelled = true;
    };
    // Start once per landing; `sub` appearing ends the loop via refresh().
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [fromCheckout, refresh]);

  const manage = useCallback(async () => {
    if (!sub) return;
    const m = sub.manage;
    if (m.kind === 'url' && m.url) return void window.location.assign(m.url);
    if (m.kind === 'apple_settings') return void window.location.assign('https://apps.apple.com/account/subscriptions');
    setManaging(true);
    setError(null);
    try {
      const { url } = await api.createStripePortal();
      window.location.assign(url);
    } catch (err) {
      setError(errorMessage(err, tErr));
      setManaging(false);
    }
  }, [sub, tErr]);

  if (fromCheckout && !sub) {
    return (
      <Panel testId={pollExhausted ? 'activation-delayed' : 'activating'}>
        <div className="flex flex-col items-center gap-4 py-6 text-center">
          {!pollExhausted && <div className="h-8 w-8 animate-spin rounded-full border-2 border-primary border-t-transparent" />}
          <p>{pollExhausted ? t('activationDelayed') : t('activating')}</p>
        </div>
      </Panel>
    );
  }

  if (!sub) {
    return (
      <Panel testId="no-subscription">
        <h2 className="text-lg font-semibold">{t('noSubscriptionTitle')}</h2>
        <p className="mt-1 text-sm text-muted-foreground">{t('noSubscription')}</p>
        <Button asChild className="mt-5">
          <Link href="/pricing">{t('seePlans')}</Link>
        </Button>
      </Panel>
    );
  }

  const date = new Date(sub.currentPeriodEnd * 1000).toLocaleDateString(locale, { year: 'numeric', month: 'long', day: 'numeric' });

  return (
    <div className="space-y-6">
      <Panel testId="subscription-card">
        <h2 className="text-lg font-semibold">{t('subscriptionTitle')}</h2>
        <p className="mt-1 text-sm text-muted-foreground">
          {sub.autoRenew ? t('renewsOn', { date }) : t('endsOn', { date })}
        </p>
        {error && <p className="mt-3 text-sm text-destructive">{error}</p>}
        <Button className="mt-5" onClick={manage} disabled={managing} data-testid="manage-btn">
          {managing ? t('opening') : t('manage')}
        </Button>
      </Panel>
      {fromCheckout && (
        <Panel testId="download-guide">
          <h2 className="text-lg font-semibold">{t('downloadTitle')}</h2>
          <p className="mt-1 text-sm text-muted-foreground">{t('downloadBody')}</p>
          <Button asChild size="lg" className="mt-5">
            <Link href="/install">{t('downloadCta')}</Link>
          </Button>
        </Panel>
      )}
    </div>
  );
}
