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
import SubscriptionManagePanel from '../SubscriptionManagePanel';
import type { Plan } from '../../services/api-types';
import { formatMinor, planAmount } from '../../utils/pricing';

interface StripePurchasePanelProps {
  plans: Plan[];
  plansLoading: boolean;
}

export default function StripePurchasePanel({ plans, plansLoading }: StripePurchasePanelProps) {
  const { t, i18n } = useTranslation();
  const { user, fetchUser } = useUser();
  const affordance = useSubscriptionAffordance();
  const openLoginDialog = useLoginDialogStore((s) => s.open);
  const { checkout, loading, error, clearError } = useStripeCheckout();
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

  // 管理面：有活跃订阅时不再兜售，提取为共享组件 SubscriptionManagePanel（也被
  // Purchase.tsx 的 WordGate 互斥拦截复用，spec 2026-08-22）。overleap 同时开了
  // apple_iap——manage 面不能默认假设 Stripe，组件内部按 manage.kind 分派。
  if (affordance.mode !== 'subscribe') {
    return <SubscriptionManagePanel activeSub={affordance.activeSub} />;
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
                    {formatMinor(planAmount(p, i18n.language).amount / p.month, planAmount(p, i18n.language).currency, i18n.language, 2)}
                    {t('purchase:purchase.stripe.perMonth')}
                  </Typography>
                </Box>
                <Typography variant="h6" fontWeight={700} data-testid={`stripe-plan-price-${p.pid}`}>
                  {formatMinor(planAmount(p, i18n.language).amount, planAmount(p, i18n.language).currency, i18n.language, 2)}
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
