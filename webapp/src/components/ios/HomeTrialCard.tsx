/**
 * HomeTrialCard — 首页试用到期提醒（已关闭自动续订的 Apple 试用用户）。
 * 文案不含官网 / 折扣 / 价格 / 品牌名（App Store 审核）。点击进内购页，来源记 trial_card。
 */

import { Alert, Button } from '@mui/material';
import { useTranslation } from 'react-i18next';
import { useNavigate } from 'react-router-dom';
import { formatExpiryDate } from './membership-format';

interface HomeTrialCardProps {
  daysLeft: number;
  endsAtSec: number;
  urgent: boolean;
}

export default function HomeTrialCard({ daysLeft, endsAtSec, urgent }: HomeTrialCardProps) {
  const { t } = useTranslation();
  const navigate = useNavigate();

  return (
    <Alert
      data-testid="home-trial-card"
      severity={urgent ? 'warning' : 'info'}
      sx={{ alignItems: 'center' }}
      action={
        <Button
          color="inherit"
          size="small"
          data-testid="home-trial-continue"
          onClick={() => navigate('/purchase', { state: { from: 'trial_card' } })}
        >
          {t('dashboard:dashboard.trialContinue')}
        </Button>
      }
    >
      {t('dashboard:dashboard.trialRenewOff', { days: daysLeft, date: formatExpiryDate(endsAtSec) })}
    </Alert>
  );
}
