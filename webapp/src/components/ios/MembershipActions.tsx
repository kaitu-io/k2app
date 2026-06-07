/**
 * MembershipActions — iOS 会员中心的自助操作区（P1）。
 * 二级操作：查看付费记录 + 恢复购买。Restore 是 Apple 强制项，两种会员态常驻、幂等。
 * 主管理 CTA（管理订阅 / 开启续订）在 RenewalStatusCard，不在此重复。
 */

import { Stack, Button } from '@mui/material';
import {
  ReceiptLongOutlined as HistoryIcon,
  RestoreOutlined as RestoreIcon,
} from '@mui/icons-material';
import { useTranslation } from 'react-i18next';

interface MembershipActionsProps {
  onHistory: () => void;
  onRestore: () => void;
  restoring: boolean;
}

export default function MembershipActions({ onHistory, onRestore, restoring }: MembershipActionsProps) {
  const { t } = useTranslation();

  return (
    <Stack direction="row" spacing={1.5} justifyContent="center" sx={{ flexWrap: 'wrap' }}>
      <Button
        size="small"
        startIcon={<HistoryIcon />}
        onClick={onHistory}
        data-testid="ios-membership-history-btn"
        sx={{ textTransform: 'none' }}
      >
        {t('purchase:purchase.iap.viewHistory')}
      </Button>
      <Button
        size="small"
        startIcon={<RestoreIcon />}
        onClick={onRestore}
        disabled={restoring}
        data-testid="ios-membership-restore-btn"
        sx={{ textTransform: 'none' }}
      >
        {restoring
          ? t('purchase:purchase.iap.restoring')
          : t('purchase:purchase.iap.restorePurchases')}
      </Button>
    </Stack>
  );
}
