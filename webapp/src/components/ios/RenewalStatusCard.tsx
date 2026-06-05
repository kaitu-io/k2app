/**
 * RenewalStatusCard — iOS 会员中心的核心卡片：续订状态 + 到期倒计时 + 紧迫度配色。
 *
 * 三态（provider 中立，由 auto_renew + mode 驱动；auto_renew 现已可信，见
 * api applyRenewalInfo 修复）：
 *  - 订阅 · 自动续订开：成功色，"下次续订 {date}"，可前往管理。
 *  - 订阅 · 自动续订关：警示色，"{date} 后将失去会员"，主 CTA「开启自动续订」（防流失核心）。
 *  - 一次性会员（无订阅）：中性，"有效期至 {date} · 剩 N 天"，无管理按钮。
 *
 * 纯展示组件：所有数据走 props，无 hook、无副作用。日期/天数算法走 membership-format。
 */

import { Card, Stack, Chip, Typography, Button, useTheme } from '@mui/material';
import {
  AutorenewOutlined as RenewIcon,
  WarningAmberOutlined as WarnIcon,
  CheckCircleOutline as CheckIcon,
} from '@mui/icons-material';
import { useTranslation } from 'react-i18next';
import { getThemeColors } from '../../theme/colors';
import { daysRemaining, expiryUrgency, formatExpiryDate, urgencyColor } from './membership-format';

interface RenewalStatusCardProps {
  mode: 'manage' | 'status';
  /** 订阅是否自动续订（manage 模式有意义；undefined 视为"开启/常规管理态"）。 */
  autoRenew?: boolean;
  /** 订阅当前周期到期（unix 秒，manage 模式优先）。 */
  periodEndSec?: number;
  /** 用户会员到期（unix 秒，status 模式或回退）。 */
  expiredAtSec: number;
  /** 当前时间（unix 秒），可注入便于测试；缺省取 Date.now()。 */
  nowSec?: number;
  /** 前往 provider 管理面（开启续订 / 管理订阅）。 */
  onManage: () => void;
}

export default function RenewalStatusCard({
  mode,
  autoRenew,
  periodEndSec,
  expiredAtSec,
  nowSec,
  onManage,
}: RenewalStatusCardProps) {
  const { t } = useTranslation();
  const theme = useTheme();
  const colors = getThemeColors(theme.palette.mode === 'dark');

  const now = nowSec ?? Math.floor(Date.now() / 1000);
  const effectiveEnd = mode === 'manage' && periodEndSec ? periodEndSec : expiredAtSec;
  const days = daysRemaining(effectiveEnd, now);
  const urgency = expiryUrgency(days);
  const dateStr = formatExpiryDate(effectiveEnd);

  // 续订关闭 = 最强流失信号：醒目警示 + 主 CTA。
  const renewalOff = mode === 'manage' && autoRenew === false;

  // 续订健康（自动续订开启的订阅）：周期末只是下次扣费日，不是断服日，故不施加
  // 紧迫度配色——否则会对"将自动续费"的用户误报红色告警（剩 2 天却仍正常续订）。
  // 仅续订关闭 / 一次性会员（真会断服）才用紧迫度红橙配色。
  const renewalHealthy = mode === 'manage' && autoRenew !== false;
  const daysColor = renewalHealthy ? 'text.secondary' : `${urgencyColor(urgency)}.main`;

  const accent = renewalOff
    ? 'warning.main'
    : mode === 'manage'
      ? 'primary.main'
      : 'success.main';

  return (
    <Card
      variant="outlined"
      data-testid="renewal-status-card"
      sx={{
        p: 2,
        borderRadius: 3,
        borderWidth: 2,
        borderColor: accent,
        background: colors.selectedGradient,
      }}
    >
      <Stack spacing={1.5}>
        {/* 状态行：图标 + 续订态 chip + 剩余天数 */}
        <Stack direction="row" alignItems="center" justifyContent="space-between">
          <Stack direction="row" alignItems="center" spacing={1}>
            {renewalOff ? (
              <WarnIcon sx={{ color: 'warning.main' }} />
            ) : mode === 'manage' ? (
              <RenewIcon sx={{ color: 'primary.main' }} />
            ) : (
              <CheckIcon sx={{ color: 'success.main' }} />
            )}
            {mode === 'manage' && (
              <Chip
                size="small"
                color={renewalOff ? 'warning' : 'primary'}
                variant={renewalOff ? 'filled' : 'outlined'}
                label={
                  renewalOff
                    ? t('purchase:purchase.iap.renewOffLabel')
                    : t('purchase:purchase.iap.renewOnLabel')
                }
                sx={{ fontWeight: 700 }}
              />
            )}
          </Stack>
          {days > 0 && (
            <Typography variant="subtitle2" sx={{ color: daysColor, fontWeight: 800 }} component="span">
              {t('purchase:purchase.iap.daysLeft', { days })}
            </Typography>
          )}
        </Stack>

        {/* 主文案：续订/到期日期 */}
        <Typography variant="body2" color="text.secondary" component="span">
          {renewalOff
            ? t('purchase:purchase.iap.renewOffWarn', { date: dateStr })
            : mode === 'manage'
              ? t('purchase:purchase.iap.renewOnNext', { date: dateStr })
              : t('purchase:purchase.iap.expiresOn', { date: dateStr })}
        </Typography>

        {/* 管理 CTA：仅 manage 模式（订阅可在 App Store 重开续订 / 管理）。 */}
        {mode === 'manage' && (
          <Button
            variant={renewalOff ? 'contained' : 'outlined'}
            color={renewalOff ? 'warning' : 'primary'}
            onClick={onManage}
            data-testid="ios-membership-manage-btn"
            sx={{ textTransform: 'none', fontWeight: 700, alignSelf: 'flex-start' }}
          >
            {renewalOff
              ? t('purchase:purchase.iap.renewOffCta')
              : t('purchase:purchase.iap.openManage')}
          </Button>
        )}
      </Stack>
    </Card>
  );
}
