/**
 * BillingIssueCard — 订阅扣款失败（grace / billing retry）的提示卡。
 * 三处共用：iOS 会员页、订阅管理面板、首页（compact）。
 * 按钮分派与 SubscriptionManagePanel 的 manage.kind 一致：
 *   apple_settings → 仅 iOS 显示，打开 Apple 付款方式页；其他平台只给文字指引
 *   stripe_portal  → Billing Portal
 *   url            → openExternal(url)
 */
import { Alert, AlertTitle, Button, Typography } from '@mui/material';
import { useTranslation } from 'react-i18next';
import type { DataSubscription } from '../services/api-types';
import { useStripeCheckout } from '../hooks/useStripeCheckout';
import { statsService } from '../services/stats';
import { APPLE_BILLING_URL } from '../utils/billing-issue';

interface BillingIssueCardProps {
  sub: DataSubscription;
  /** 首页用：只显示一行正文 + 按钮。 */
  compact?: boolean;
}

export default function BillingIssueCard({ sub, compact }: BillingIssueCardProps) {
  const { t } = useTranslation();
  const { openPortal } = useStripeCheckout();
  const manage = sub.manage;

  let action: (() => void) | undefined;
  let elsewhere = false;
  if (manage?.kind === 'apple_settings') {
    if (window._platform?.os === 'ios') {
      action = () => void window._platform?.openExternal?.(APPLE_BILLING_URL);
    } else {
      elsewhere = true;
    }
  } else if (manage?.kind === 'stripe_portal') {
    action = () => void openPortal();
  } else if (manage?.kind === 'url' && manage.url) {
    const url = manage.url;
    action = () => void window._platform?.openExternal?.(url);
  }

  const onClick = () => {
    void statsService.trackFunnel('manage_click', { source: 'billing_issue' });
    action?.();
  };

  // body / trialBody 文案点名了 Apple，只对 Apple 订阅使用；其他 provider 走中性文案。
  const body =
    sub.provider !== 'apple'
      ? t('purchase:purchase.billingIssue.bodyGeneric')
      : sub.isTrial
        ? t('purchase:purchase.billingIssue.trialBody')
        : t('purchase:purchase.billingIssue.body');

  return (
    <Alert
      severity="warning"
      data-testid="billing-issue-card"
      action={
        compact && action ? (
          <Button color="inherit" size="small" data-testid="billing-issue-update" onClick={onClick}>
            {t('purchase:purchase.billingIssue.update')}
          </Button>
        ) : undefined
      }
    >
      {!compact && <AlertTitle>{t('purchase:purchase.billingIssue.title')}</AlertTitle>}
      <Typography variant="body2">{body}</Typography>
      {elsewhere && !compact && (
        <Typography variant="body2" sx={{ mt: 0.5 }}>
          {t('purchase:purchase.billingIssue.appleElsewhere')}
        </Typography>
      )}
      {!compact && action && (
        <Button
          variant="contained"
          size="small"
          sx={{ mt: 1.5 }}
          data-testid="billing-issue-update"
          onClick={onClick}
        >
          {t('purchase:purchase.billingIssue.update')}
        </Button>
      )}
    </Alert>
  );
}
