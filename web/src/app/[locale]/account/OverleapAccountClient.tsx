'use client';

/**
 * 本品牌账户页：订阅状态 + Billing Portal 入口 + checkout success 激活轮询。
 *
 * 鉴权由 account/layout.tsx 的 useAuth 守卫承担（未登录被重定向到 /login），
 * 本组件假定已登录。webhook 入账是异步的：?checkout=success 回跳时订阅可能
 * 尚未落库，轮询用户档（3s × 10 次）直至出现，超时给"稍后刷新"兜底。
 *
 * 轮询和挂载首取放在同一个 effect 的单条异步链里（sleep + 循环），不拆成两个
 * 互相依赖 state 的 effect：后一种写法要等 React 提交一次新渲染才能看到
 * `loaded` 变化去建 interval，测试里用假定时器把多段推进都包进同一个 `act()`
 * 时中间渲染不会提交，interval 永远建不起来。单链路直接用上一次 fetch 的
 * 返回值做判断，不依赖渲染时机。
 */
import { useCallback, useEffect, useState } from 'react';
import { useTranslations, useLocale } from 'next-intl';
import { useSearchParams } from 'next/navigation';
import { Link } from '@/i18n/routing';
import { api, type DataSubscription, type User } from '@/lib/api';
import { Button } from '@/components/ui/button';
import { Card, CardContent } from '@/components/ui/card';

const POLL_INTERVAL_MS = 3000;
const POLL_MAX_ATTEMPTS = 10;

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

export default function OverleapAccountClient() {
  const t = useTranslations('account');
  const locale = useLocale();
  const searchParams = useSearchParams();
  const fromCheckoutSuccess = searchParams.get('checkout') === 'success';

  const [profile, setProfile] = useState<User | null>(null);
  const [loaded, setLoaded] = useState(false);
  const [pollExhausted, setPollExhausted] = useState(false);
  const [managing, setManaging] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const activeSub: DataSubscription | null = profile?.subscriptions?.[0] ?? null;

  const fetchProfile = useCallback(async (): Promise<User | null> => {
    try {
      const u = await api.getUserProfile({ autoRedirectToAuth: false });
      setProfile(u);
      return u;
    } catch {
      return null;
    } finally {
      setLoaded(true);
    }
  }, []);

  // 挂载即取一次档案；success 回跳且尚无订阅 → 在同一条异步链里继续轮询等 webhook 入账
  useEffect(() => {
    let cancelled = false;

    (async () => {
      const first = await fetchProfile();
      if (cancelled || !fromCheckoutSuccess || first?.subscriptions?.length) return;

      for (let attempt = 0; attempt < POLL_MAX_ATTEMPTS; attempt++) {
        await sleep(POLL_INTERVAL_MS);
        if (cancelled) return;
        const u = await fetchProfile();
        if (u?.subscriptions?.length) return;
      }
      if (!cancelled) setPollExhausted(true);
    })();

    return () => {
      cancelled = true;
    };
  }, [fromCheckoutSuccess, fetchProfile]);

  const handleManage = useCallback(async () => {
    if (!activeSub) return;
    const m = activeSub.manage;
    if (m.kind === 'url' && m.url) {
      window.location.assign(m.url);
      return;
    }
    if (m.kind === 'apple_settings') {
      window.location.assign('https://apps.apple.com/account/subscriptions');
      return;
    }
    // stripe_portal（缺省）
    setManaging(true);
    setError(null);
    try {
      const { url } = await api.createStripePortal({ autoRedirectToAuth: false });
      window.location.assign(url);
    } catch {
      setError(t('stripe.genericError'));
      setManaging(false);
    }
  }, [activeSub, t]);

  if (!loaded) {
    return (
      <div className="flex min-h-[300px] items-center justify-center">
        <div className="h-8 w-8 animate-spin rounded-full border-2 border-primary border-t-transparent" />
      </div>
    );
  }

  if (fromCheckoutSuccess && !activeSub) {
    return pollExhausted ? (
      <Card>
        <CardContent className="py-10 text-center" data-testid="activation-delayed">
          {t('stripe.activationDelayed')}
        </CardContent>
      </Card>
    ) : (
      <Card>
        <CardContent
          className="flex flex-col items-center gap-4 py-10 text-center"
          data-testid="activating"
        >
          <div className="h-8 w-8 animate-spin rounded-full border-2 border-primary border-t-transparent" />
          <p>{t('stripe.activating')}</p>
        </CardContent>
      </Card>
    );
  }

  if (!activeSub) {
    return (
      <Card>
        <CardContent
          className="flex flex-col items-center gap-4 py-10 text-center"
          data-testid="no-subscription"
        >
          <p className="text-muted-foreground">{t('stripe.noSubscription')}</p>
          <Button asChild>
            <Link href="/purchase">{t('stripe.choosePlan')}</Link>
          </Button>
        </CardContent>
      </Card>
    );
  }

  const periodEnd = new Date(activeSub.currentPeriodEnd * 1000).toLocaleDateString(locale, {
    year: 'numeric',
    month: 'long',
    day: 'numeric',
  });

  return (
    <div className="flex flex-col gap-6">
      <Card data-testid="subscription-card">
        <CardContent className="flex flex-col gap-4 py-6">
          <div>
            <h2 className="text-lg font-semibold">{t('stripe.subscriptionTitle')}</h2>
            <p className="mt-1 text-sm text-muted-foreground">
              {activeSub.autoRenew
                ? t('stripe.renewsOn', { date: periodEnd })
                : t('stripe.expiresOn', { date: periodEnd })}
            </p>
          </div>
          {error && <p className="text-sm text-destructive">{error}</p>}
          <div>
            <Button onClick={handleManage} disabled={managing} data-testid="manage-btn">
              {managing ? t('stripe.opening') : t('stripe.manage')}
            </Button>
          </div>
        </CardContent>
      </Card>

      {fromCheckoutSuccess && (
        <Card data-testid="download-guide">
          <CardContent className="flex flex-col items-center gap-4 py-8 text-center">
            <h3 className="text-lg font-semibold">{t('stripe.downloadTitle')}</h3>
            <Button asChild size="lg">
              <Link href="/install">{t('stripe.downloadCta')}</Link>
            </Button>
          </CardContent>
        </Card>
      )}
    </div>
  );
}
