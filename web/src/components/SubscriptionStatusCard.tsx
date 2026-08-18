'use client';

/**
 * 订阅状态卡 —— 回答"我买了什么、什么时候到期"。
 *
 * 两个消费方，形态不同：
 *   - /account 首页：完整形态，带续费 CTA
 *   - /purchase 顶部：compact，无 CTA（整个页面本身就是 CTA，再放一个按钮是噪音）
 *
 * 本品牌的订阅模型是 `User.expiredAt`（授权到期时间戳）+ `tier`（档位），
 * 不是 Stripe/Apple 的 `subscriptions[]` —— 那是另一品牌的形态，见
 * OverleapAccountClient。两者不共用组件是刻意的：数据模型不同，
 * 强行合并会得到一个两边都别扭的抽象。
 */
import { useLocale, useTranslations } from 'next-intl';
import { Link } from '@/i18n/routing';
import { Button } from '@/components/ui/button';
import { Card, CardContent } from '@/components/ui/card';
import type { User } from '@/lib/api';

const SECONDS_PER_DAY = 86400;

type SubscriptionState = 'active' | 'expired' | 'none';

export function resolveSubscriptionState(expiredAt: number | undefined, nowSeconds: number): SubscriptionState {
  if (!expiredAt || expiredAt <= 0) return 'none';
  return expiredAt < nowSeconds ? 'expired' : 'active';
}

/**
 * 只依赖真正读到的两个字段，而不是整个 `User`：购买页的 userProfile 是个
 * 更窄的结构化字面量类型，收窄 props 让两个消费方都能直接传。
 */
type SubscriptionProfile = Pick<User, 'expiredAt'> & Partial<Pick<User, 'tier'>>;

interface Props {
  profile: SubscriptionProfile;
  /** 购买页用：省略续费 CTA，避免与页面主 CTA 重复 */
  compact?: boolean;
}

export default function SubscriptionStatusCard({ profile, compact = false }: Props) {
  const t = useTranslations('account');
  const locale = useLocale();

  const nowSeconds = Date.now() / 1000;
  const state = resolveSubscriptionState(profile.expiredAt, nowSeconds);

  const expiryDate =
    profile.expiredAt > 0
      ? new Date(profile.expiredAt * 1000).toLocaleDateString(locale, {
          year: 'numeric',
          month: 'long',
          day: 'numeric',
        })
      : '';

  // 向上取整：还剩半天也算「剩余 1 天」，不显示 0 天误导用户以为已断
  const daysLeft = Math.ceil((profile.expiredAt - nowSeconds) / SECONDS_PER_DAY);

  return (
    <Card data-testid="subscription-status-card">
      <CardContent className="flex flex-col gap-4 py-6">
        <div className="flex flex-col gap-1">
          <h2 className="text-lg font-semibold">{t('subscription.title')}</h2>

          {state === 'active' && (
            <div data-testid="status-active" className="flex flex-col gap-1">
              <p className="text-sm text-muted-foreground">
                {t('subscription.activeUntil', { date: expiryDate })}
              </p>
              <p className="text-sm font-medium text-primary">
                {t('subscription.daysLeft', { days: daysLeft })}
              </p>
            </div>
          )}

          {state === 'expired' && (
            <p data-testid="status-expired" className="text-sm text-destructive">
              {t('subscription.expiredOn', { date: expiryDate })}
            </p>
          )}

          {state === 'none' && (
            <p data-testid="status-none" className="text-sm text-muted-foreground">
              {t('subscription.never')}
            </p>
          )}
        </div>

        {profile.tier && (
          <div data-testid="tier-row" className="flex items-center gap-2 text-sm">
            <span className="text-muted-foreground">{t('subscription.tierLabel')}</span>
            <span className="font-medium">{profile.tier}</span>
          </div>
        )}

        {!compact && (
          <div>
            <Button asChild>
              <Link href="/purchase" data-testid="renew-cta">
                {state === 'active' ? t('subscription.renew') : t('subscription.choosePlan')}
              </Link>
            </Button>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
