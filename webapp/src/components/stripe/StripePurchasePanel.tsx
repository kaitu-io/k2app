/**
 * StripePurchasePanel — Stripe 订阅品牌（stripeCheckout gate）的购买/管理面。
 * 纯订阅模式：无一次性支付、无 campaign 码（优惠由 Stripe Dashboard Promotion
 * Code 在 Checkout 页承接）、无 private_node。
 * 入账在服务端 webhook 完成——本组件只负责跳外链 + 引导刷新用户状态。
 */
import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  Alert, Box, Button, Card, CardContent, CircularProgress, Radio, Stack, Typography,
} from '@mui/material';
import OpenInNewIcon from '@mui/icons-material/OpenInNew';
import { useStripeCheckout } from '../../hooks/useStripeCheckout';
import { useSubscriptionAffordance } from '../../hooks/useSubscriptionAffordance';
import { useUser } from '../../hooks/useUser';
import { useLoginDialogStore } from '../../stores/login-dialog.store';
import MembershipBenefits from '../MembershipBenefits';
import type { Plan } from '../../services/api-types';

interface StripePurchasePanelProps {
  plans: Plan[];
  plansLoading: boolean;
}

// Apple 订阅管理跳转（同 IosMembershipPanel.tsx 的 APPLE_SUBS_URL）：overleap 同时
// 开了 apple_iap 渠道，manage.kind === 'apple_settings' 时该商品是在 iOS 端购买
// 的，桌面/web 无法调用 Stripe portal（会打错商户或直接报错）——只能引导去 App
// Store 的订阅管理页。
const APPLE_SUBS_URL = 'itms-apps://apps.apple.com/account/subscriptions';
const APPLE_SUBS_URL_HTTPS = 'https://apps.apple.com/account/subscriptions';

// itms-apps:// only resolves on Apple platforms — it silently no-ops on
// Windows/Linux desktop, so those need the https fallback to actually open.
function appleSubsUrl(): string {
  const os = window._platform?.os;
  return os === 'ios' || os === 'macos' ? APPLE_SUBS_URL : APPLE_SUBS_URL_HTTPS;
}

export default function StripePurchasePanel({ plans, plansLoading }: StripePurchasePanelProps) {
  const { t } = useTranslation();
  const { user, fetchUser } = useUser();
  const affordance = useSubscriptionAffordance();
  const openLoginDialog = useLoginDialogStore((s) => s.open);
  const { checkout, openPortal, loading, error, clearError } = useStripeCheckout();
  const [opened, setOpened] = useState(false);
  const [refreshing, setRefreshing] = useState(false);

  // 纯订阅：只卖 app 产品线的订阅套餐。
  const subPlans = useMemo(() => plans.filter((p) => p.product === 'app'), [plans]);
  const [selected, setSelected] = useState('');
  const selectedPid = useMemo(() => {
    if (selected && subPlans.some((p) => p.pid === selected)) return selected;
    return subPlans.find((p) => p.highlight)?.pid ?? subPlans[0]?.pid ?? '';
  }, [selected, subPlans]);
  const selectedPlan = subPlans.find((p) => p.pid === selectedPid);

  const handleSubscribe = async () => {
    if (!user) {
      openLoginDialog({ trigger: 'purchase' });
      return;
    }
    clearError();
    if (await checkout(selectedPid)) setOpened(true);
  };

  const handleRefresh = async () => {
    setRefreshing(true);
    try {
      await fetchUser(true);
    } finally {
      setRefreshing(false);
    }
  };

  // 管理面：有活跃订阅时不再兜售，按 manage.kind 分派到对应 provider 的管理入口。
  // overleap 同时开了 apple_iap（本分支 api Tasks 1-3）——manage 面不能默认假设
  // Stripe：iOS 端购的订阅在这里看到的是 apple_settings，Stripe portal 按钮对它
  // 必然报错，必须先按 kind 分派（同 IosMembershipPanel.tsx openManage 的模式）。
  if (affordance.mode !== 'subscribe') {
    const manage = affordance.activeSub?.manage;
    return (
      <Box sx={{ width: '100%', py: 1 }} data-testid="stripe-manage-panel">
        <Stack spacing={2.5}>
          <Typography variant="h6" fontWeight={700}>
            {t('purchase:purchase.stripe.manageTitle')}
          </Typography>
          <MembershipBenefits
            maxDevice={user?.maxDevice}
            maxRouterDevice={user?.maxRouterDevice}
            maxLanClient={user?.maxLanClient}
          />

          {manage?.kind === 'stripe_portal' && (
            <>
              <Typography variant="body2" color="text.secondary">
                {t('purchase:purchase.stripe.portalHint')}
              </Typography>
              {error && <Alert severity="error">{error}</Alert>}
              <Button
                data-testid="stripe-portal-btn"
                variant="contained"
                endIcon={<OpenInNewIcon />}
                disabled={loading}
                onClick={() => void openPortal()}
              >
                {t('purchase:purchase.stripe.manageButton')}
              </Button>
            </>
          )}

          {manage?.kind === 'apple_settings' && (
            <Button
              data-testid="stripe-manage-apple-btn"
              variant="contained"
              endIcon={<OpenInNewIcon />}
              onClick={() => void window._platform?.openExternal?.(appleSubsUrl())}
            >
              {t('purchase:purchase.iap.openManage')}
            </Button>
          )}

          {manage?.kind === 'url' && manage.url && (
            <Button
              data-testid="stripe-manage-url-btn"
              variant="contained"
              endIcon={<OpenInNewIcon />}
              onClick={() => void window._platform?.openExternal?.(manage.url!)}
            >
              {t('purchase:purchase.iap.openManage')}
            </Button>
          )}

          {/* kind 缺失/未知（或 'url' 却没带 url）：fail-safe——不给一个必然
              报错或打错商户的死按钮，只展示订阅状态（标题 + 权益）。 */}
        </Stack>
      </Box>
    );
  }

  if (plansLoading) {
    return (
      <Box sx={{ display: 'flex', justifyContent: 'center', py: 6 }}>
        <CircularProgress />
      </Box>
    );
  }
  if (subPlans.length === 0) {
    return (
      <Box sx={{ p: 4, textAlign: 'center' }}>
        <Typography variant="body1">{t('purchase:purchase.stripe.noPlans')}</Typography>
      </Box>
    );
  }

  return (
    <Box sx={{ width: '100%', py: 1 }} data-testid="stripe-subscribe-panel">
      <Stack spacing={2.5}>
        <Typography variant="h6" fontWeight={700}>
          {t('purchase:purchase.stripe.subscribeTitle')}
        </Typography>

        <Stack spacing={1.5}>
          {subPlans.map((p) => (
            <Card
              key={p.pid}
              variant="outlined"
              onClick={() => setSelected(p.pid)}
              sx={{
                cursor: 'pointer',
                borderColor: p.pid === selectedPid ? 'primary.main' : 'divider',
                borderWidth: p.pid === selectedPid ? 2 : 1,
              }}
            >
              <CardContent sx={{ display: 'flex', alignItems: 'center', gap: 1, py: 1.5, '&:last-child': { pb: 1.5 } }}>
                <Radio checked={p.pid === selectedPid} size="small" />
                <Box sx={{ flex: 1 }}>
                  <Typography fontWeight={600}>{p.label}</Typography>
                  <Typography variant="caption" color="text.secondary">
                    ${(p.price / p.month / 100).toFixed(2)}
                    {t('purchase:purchase.stripe.perMonth')}
                  </Typography>
                </Box>
                <Typography variant="h6" fontWeight={700}>
                  ${(p.price / 100).toFixed(2)}
                </Typography>
              </CardContent>
            </Card>
          ))}
        </Stack>

        <MembershipBenefits
          maxDevice={selectedPlan?.maxDevice}
          maxRouterDevice={selectedPlan?.maxRouterDevice}
          maxLanClient={selectedPlan?.maxLanClient}
        />

        {error && <Alert severity="error">{error}</Alert>}

        {opened ? (
          <Stack spacing={1.5} data-testid="stripe-opened-hint">
            <Alert severity="info">{t('purchase:purchase.stripe.openedHint')}</Alert>
            <Button variant="outlined" disabled={refreshing} onClick={() => void handleRefresh()}>
              {t('purchase:purchase.stripe.refreshStatus')}
            </Button>
          </Stack>
        ) : (
          <Button
            data-testid="stripe-subscribe-btn"
            variant="contained"
            size="large"
            endIcon={<OpenInNewIcon />}
            disabled={loading || !selectedPid}
            onClick={() => void handleSubscribe()}
          >
            {t('purchase:purchase.stripe.subscribeButton')}
          </Button>
        )}
      </Stack>
    </Box>
  );
}
