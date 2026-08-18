'use client';

/**
 * 本品牌账户首页 —— 购买闭环的最后一环。
 *
 * 定位刻意收窄：只回答"我买了什么、到什么时候、买过几次"。设备管理、专属节点、
 * 邀请、反馈这些**只在 app 内**（webapp 的 Account.tsx 已经是完整账号中心），
 * 这里不复刻 —— 2026-04-22 fc5aa0d7 删掉「成员管理」后本页只剩一个跳转到
 * /purchase 的空壳，用户买完在网页上无处确认订单，本页补的就是这个洞。
 *
 * 鉴权由 account/layout.tsx 的 useAuth 守卫承担，本组件假定已登录。
 */
import { useEffect, useState } from 'react';
import { useLocale, useTranslations } from 'next-intl';
import { api, type ProHistory, type User } from '@/lib/api';
import { Card, CardContent } from '@/components/ui/card';
import SubscriptionStatusCard from '@/components/SubscriptionStatusCard';

const HISTORY_PAGE_SIZE = 10;

function HistoryRow({ item, locale }: { item: ProHistory; locale: string }) {
  const t = useTranslations('account');

  const label =
    item.type === 'recharge'
      ? t('subscription.historyRecharge')
      : item.type === 'reward'
        ? t('subscription.historyReward')
        : item.type;

  return (
    <li data-testid="history-item" className="flex flex-col gap-1 border-b py-3 last:border-b-0">
      <div className="flex items-center justify-between gap-3">
        <span className="text-sm font-medium">{label}</span>
        <span className="text-sm font-medium text-primary">
          {t('subscription.historyDays', { days: item.days })}
        </span>
      </div>
      <div className="flex items-center justify-between gap-3 text-xs text-muted-foreground">
        {/* 与状态卡的到期日同格式 —— 裸 toLocaleDateString 在 zh 下给出
            "2026/8/15"，和上方的"2027年2月12日"并排看着像两套系统 */}
        <span>
          {new Date(item.createdAt * 1000).toLocaleDateString(locale, {
            year: 'numeric',
            month: 'long',
            day: 'numeric',
          })}
        </span>
        {item.order && <span>¥{(item.order.payAmount / 100).toFixed(2)}</span>}
      </div>
      {item.reason && <p className="text-xs text-muted-foreground">{item.reason}</p>}
    </li>
  );
}

export default function KaituAccountClient() {
  const t = useTranslations('account');
  const locale = useLocale();

  const [profile, setProfile] = useState<User | null>(null);
  const [histories, setHistories] = useState<ProHistory[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [profileFailed, setProfileFailed] = useState(false);

  useEffect(() => {
    let cancelled = false;

    (async () => {
      // 历史失败不该拖垮订阅状态 —— 后者才是本页存在的理由，所以各自兜底。
      const [profileResult, historyResult] = await Promise.allSettled([
        api.getUserProfile({ autoRedirectToAuth: false }),
        api.getProHistories({ page: 1, pageSize: HISTORY_PAGE_SIZE }),
      ]);

      if (cancelled) return;

      if (profileResult.status === 'fulfilled') {
        setProfile(profileResult.value);
      } else {
        setProfileFailed(true);
      }
      if (historyResult.status === 'fulfilled') {
        setHistories(historyResult.value.items ?? []);
      }
      setLoaded(true);
    })();

    return () => {
      cancelled = true;
    };
  }, []);

  if (!loaded) {
    return (
      <div className="flex min-h-[300px] items-center justify-center">
        <div className="h-8 w-8 animate-spin rounded-full border-2 border-primary border-t-transparent" />
      </div>
    );
  }

  if (profileFailed || !profile) {
    return (
      <Card>
        <CardContent className="py-10 text-center text-muted-foreground" data-testid="profile-error">
          {t('subscription.loadFailed')}
        </CardContent>
      </Card>
    );
  }

  const email = profile.loginIdentifies?.find((i) => i.type === 'email')?.value ?? '';

  return (
    <div className="flex flex-col gap-6">
      <SubscriptionStatusCard profile={profile} />

      <Card>
        <CardContent className="flex flex-col gap-3 py-6">
          <h2 className="text-lg font-semibold">{t('subscription.accountTitle')}</h2>
          <div className="flex items-center justify-between gap-3 text-sm">
            <span className="text-muted-foreground">{t('subscription.emailLabel')}</span>
            <span data-testid="account-email" className="font-medium">
              {email}
            </span>
          </div>
          <div className="flex items-center justify-between gap-3 text-sm">
            <span className="text-muted-foreground">{t('subscription.devicesLabel')}</span>
            <span data-testid="account-devices" className="font-medium">
              {profile.deviceCount}
            </span>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardContent className="flex flex-col gap-2 py-6">
          <h2 className="text-lg font-semibold">{t('subscription.historyTitle')}</h2>
          {histories.length > 0 ? (
            <ul data-testid="history-list" className="flex flex-col">
              {histories.map((item) => (
                <HistoryRow
                  key={`${item.createdAt}-${item.type}-${item.order?.uuid ?? ''}`}
                  item={item}
                  locale={locale}
                />
              ))}
            </ul>
          ) : (
            <p data-testid="history-empty" className="py-4 text-sm text-muted-foreground">
              {t('subscription.historyEmpty')}
            </p>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
