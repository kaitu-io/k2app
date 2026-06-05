import { Box, Stack, Typography, Button } from '@mui/material';
import { useTranslation } from 'react-i18next';
import type { ManageSurface } from '../services/api-types';

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
 */
export default function IosMembershipPanel({ mode, expiredAt, manageSurface }: IosMembershipPanelProps) {
  const { t } = useTranslation();

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
      </Stack>
    </Box>
  );
}
