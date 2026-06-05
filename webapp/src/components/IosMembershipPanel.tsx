import { useEffect } from 'react';
import { Box, Stack, Typography, Button } from '@mui/material';
import { useTranslation } from 'react-i18next';
import type { ManageSurface } from '../services/api-types';
import { useIapPurchase } from '../hooks/useIapPurchase';
import { useUser } from '../hooks/useUser';
import { useAlert } from '../stores/alert.store';

interface IosMembershipPanelProps {
  /** affordance 模式（仅 manage/status 会渲染此面板；subscribe 走正常购买 UI）。 */
  mode: 'manage' | 'status';
  /** 用户会员到期（unix 秒），status 模式显示。 */
  expiredAt: number;
  /** 活跃订阅的管理面（manage 模式用）。 */
  manageSurface?: ManageSurface;
}

const APPLE_SUBS_URL = 'itms-apps://apps.apple.com/account/subscriptions';

/**
 * iOS 会员面板：活跃订阅 → "管理订阅"（跳 provider 管理面）；活跃一次性会员 → 显示到期。
 * 绝不出现购买/外链支付（满足 Apple 3.1.1）。
 *
 * Restore Purchases 在两种模式下都常驻：Apple 要求恢复购买入口始终可达，且与
 * affordance 无关（re-verify 幂等、绝不双扣）。本面板早返替代了 IapPurchaseSheet，
 * 故 restore 必须在此自带，否则 manage/status 模式下用户无从恢复购买（再次拒审风险）。
 */
export default function IosMembershipPanel({ mode, expiredAt, manageSurface }: IosMembershipPanelProps) {
  const { t } = useTranslation();
  const { restore, restoring, purchaseError, lastGrantedUser } = useIapPurchase();
  const { fetchUser } = useUser();
  const { showAlert } = useAlert();

  // 恢复成功后：提示 + 刷新用户，使 affordance 重算（未绑定的 Apple 订阅被绑定后
  // status → manage 自动翻转）。
  useEffect(() => {
    if (lastGrantedUser) {
      showAlert(t('purchase:purchase.iap.successAlert'), 'success');
      void fetchUser();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [lastGrantedUser]);

  const openManage = () => {
    const url =
      manageSurface?.kind === 'url' && manageSurface.url ? manageSurface.url : APPLE_SUBS_URL;
    void window._platform?.openExternal?.(url);
  };

  const dateStr = expiredAt > 0 ? new Date(expiredAt * 1000).toLocaleDateString() : '';

  return (
    <Box sx={{ width: '100%', py: 3 }} data-testid={`ios-membership-${mode}`}>
      <Stack spacing={2}>
        <Typography variant="h6" fontWeight={700}>
          {mode === 'manage'
            ? t('purchase:purchase.iap.manageTitle')
            : t('purchase:purchase.iap.statusTitle')}
        </Typography>
        <Typography variant="body2" color="text.secondary">
          {mode === 'manage'
            ? t('purchase:purchase.iap.manageBody')
            : t('purchase:purchase.iap.statusBody', { date: dateStr })}
        </Typography>
        {mode === 'manage' && (
          <Button
            variant="contained"
            onClick={openManage}
            data-testid="ios-membership-manage-btn"
            sx={{ textTransform: 'none', fontWeight: 700 }}
          >
            {t('purchase:purchase.iap.openManage')}
          </Button>
        )}

        {/* Restore — 两模式常驻，满足 Apple 恢复购买要求；幂等，绝不双扣。 */}
        <Button
          variant="text"
          size="small"
          onClick={() => void restore()}
          disabled={restoring}
          data-testid="ios-membership-restore-btn"
          sx={{ textTransform: 'none', alignSelf: mode === 'manage' ? 'flex-start' : 'center' }}
        >
          {restoring
            ? t('purchase:purchase.iap.restoring')
            : t('purchase:purchase.iap.restorePurchases')}
        </Button>

        {purchaseError && (
          <Typography
            variant="body2"
            color="error"
            data-testid="ios-membership-restore-error"
            sx={{ fontWeight: 600 }}
          >
            {purchaseError}
          </Typography>
        )}
      </Stack>
    </Box>
  );
}
