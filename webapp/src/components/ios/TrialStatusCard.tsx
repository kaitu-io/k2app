/**
 * TrialStatusCard — iOS 会员中心的试用卡（设计稿 ②，spec 2026-10-19 §4.2）。
 *
 * 订阅 `isTrial` 时取代 RenewalStatusCard：「免费试用中」+ 剩余天数 + 续订说明 +「管理订阅」。
 * 用户已取消自动续订（autoRenew === false）时，续订说明换成「试用将于 X 结束，不会扣费」。
 * 总天数 / 进度条 / 续订价格全部来自 StoreKit 商品（trialLengthOf + displayPrice）；
 * 拿不到商品（加载失败、旧壳无 introOffer）时只显示剩余天数和不带价格的续订说明。
 *
 * 纯展示组件：数据走 props，无副作用。
 */

import { Box, Button, Card, Stack, Typography } from '@mui/material';
import { useTranslation } from 'react-i18next';
import type { IapProduct } from '../../types/kaitu-core';
import { daysRemaining, formatExpiryDate } from './membership-format';
import { trialLengthOf } from './trial-offer';

interface TrialStatusCardProps {
  /** 试用结束（unix 秒）。 */
  trialEndsAtSec: number;
  /** 当前订阅的 StoreKit 商品（可能还没加载到）。 */
  product?: IapProduct;
  /** 订阅是否自动续订（Apple renewal info）。false = 用户已取消，试用到期即结束、不扣费。 */
  autoRenew?: boolean;
  /** 当前时间（unix 秒），可注入便于测试；缺省取 Date.now()。 */
  nowSec?: number;
  onManage: () => void;
}

export default function TrialStatusCard({ trialEndsAtSec, product, autoRenew, nowSec, onManage }: TrialStatusCardProps) {
  const { t } = useTranslation();
  const now = nowSec ?? Math.floor(Date.now() / 1000);
  const daysLeft = daysRemaining(trialEndsAtSec, now);
  // 试用中资格已用掉（eligible=false），总天数取商品配置的试用时长，不看资格。
  const offer = trialLengthOf(product);
  const total = offer?.days ?? 0;
  const price = product?.displayPrice;
  const date = formatExpiryDate(trialEndsAtSec);
  const progress = total > 0 ? Math.round((Math.max(0, total - daysLeft) / total) * 100) : 0;

  return (
    <Card
      variant="outlined"
      data-testid="trial-status-card"
      sx={{ p: 2.25, borderRadius: 4, display: 'flex', flexDirection: 'column', gap: 1.75 }}
    >
      <Stack direction="row" alignItems="center" justifyContent="space-between">
        <Typography sx={{ fontSize: 17, fontWeight: 700 }}>
          {t('purchase:purchase.iap.planName')}
        </Typography>
        <Box
          sx={{
            fontSize: 13,
            fontWeight: 600,
            color: 'primary.contrastText',
            bgcolor: 'primary.main',
            borderRadius: 999,
            px: 1.25,
            py: 0.375,
          }}
        >
          {t('purchase:purchase.iap.trialStatusTag')}
        </Box>
      </Stack>

      <Stack spacing={1}>
        <Stack direction="row" justifyContent="space-between">
          <Typography variant="body2" color="text.secondary" sx={{ fontSize: 13 }}>
            {t('purchase:purchase.iap.trialDaysLeft', { days: daysLeft })}
          </Typography>
          {offer && (
            <Typography variant="body2" color="text.secondary" sx={{ fontSize: 13 }}>
              {t('purchase:purchase.iap.trialTotal', { days: total })}
            </Typography>
          )}
        </Stack>
        {offer && (
          <Box
            data-testid="trial-progress"
            sx={{ height: 6, borderRadius: 3, bgcolor: 'action.hover', overflow: 'hidden' }}
          >
            <Box sx={{ height: 6, width: `${progress}%`, bgcolor: 'primary.main' }} />
          </Box>
        )}
      </Stack>

      <Typography variant="body2" sx={{ fontSize: 14, lineHeight: 1.5 }}>
        {autoRenew === false
          ? t('purchase:purchase.iap.trialEndsNoCharge', { date })
          : price
          ? t('purchase:purchase.iap.trialRenewNote', { date, price })
          : t('purchase:purchase.iap.trialRenewNoteNoPrice', { date })}
      </Typography>

      <Button
        variant="outlined"
        color="inherit"
        onClick={onManage}
        data-testid="trial-manage-btn"
        sx={{ minHeight: 44, borderRadius: 3, textTransform: 'none', fontSize: 15, fontWeight: 600 }}
      >
        {t('purchase:purchase.iap.manageSubscription')}
      </Button>
    </Card>
  );
}
