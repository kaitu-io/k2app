/**
 * SubscriptionManagePanel — provider 中立的"活跃订阅管理面"，从
 * StripePurchasePanel.tsx 的 manage 分支提取为共享组件（spec 2026-08-22）。
 * 消费方：
 *   - StripePurchasePanel（stripeCheckout 品牌，affordance.mode !== 'subscribe'）
 *   - Purchase.tsx WordGate 轨（affordance.mode === 'manage' 互斥拦截）
 * 按 activeSub.manage.kind 分派到对应 provider 的管理入口
 * （stripe_portal / apple_settings / url），kind 缺失/未知时 fail-safe——
 * 只展示订阅状态，不给一个必然报错或打错商户的死按钮。
 */
import { useTranslation } from 'react-i18next';
import { Alert, Box, Button, Stack, Typography } from '@mui/material';
import OpenInNewIcon from '@mui/icons-material/OpenInNew';
import { useStripeCheckout } from '../hooks/useStripeCheckout';
import { useUser } from '../hooks/useUser';
import MembershipBenefits from './MembershipBenefits';
import type { DataSubscription } from '../services/api-types';

interface SubscriptionManagePanelProps {
  activeSub?: DataSubscription;
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

export default function SubscriptionManagePanel({ activeSub }: SubscriptionManagePanelProps) {
  const { t } = useTranslation();
  const { user } = useUser();
  const { openPortal, loading, error } = useStripeCheckout();

  const manage = activeSub?.manage;

  return (
    <Box sx={{ width: '100%', py: 1 }} data-testid="stripe-manage-panel">
      <Stack spacing={2.5}>
        <Typography variant="h6" fontWeight={700}>
          {t('purchase:purchase.stripe.manageTitle')}
        </Typography>

        <Alert severity="info" data-testid="manage-blocked-hint">
          {t('purchase:purchase.manage.blockedHint')}
        </Alert>

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
            报错或打错商户的死按钮，只展示订阅状态（标题 + 权益 + 提醒）。 */}
      </Stack>
    </Box>
  );
}
