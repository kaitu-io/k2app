/**
 * InviteRewardCard — iOS 会员中心的增长闭环卡片（Apple 合规：免费得天数，无任何外部购买）。
 * active 会员是最佳传播者；引导其邀请好友，双方各得 N 天。纯展示，数据走 props。
 */

import { Box, Stack, Typography, Button, useTheme } from '@mui/material';
import { EmojiEvents as EmojiEventsIcon } from '@mui/icons-material';
import { useTranslation } from 'react-i18next';
import { getThemeColors } from '../../theme/colors';

interface InviteRewardCardProps {
  /** 触发奖励的最低套餐月数。 */
  months: number;
  onInvite: () => void;
}

export default function InviteRewardCard({ months, onInvite }: InviteRewardCardProps) {
  const { t } = useTranslation();
  const theme = useTheme();
  const colors = getThemeColors(theme.palette.mode === 'dark');

  return (
    <Box
      data-testid="invite-reward-card"
      sx={{
        borderRadius: 2,
        boxShadow: 2,
        background: colors.warningGradient,
        p: 1.5,
        display: 'flex',
        alignItems: 'center',
        gap: 1.5,
      }}
    >
      <EmojiEventsIcon sx={{ color: colors.warningDark, fontSize: 28, flexShrink: 0 }} />
      <Stack sx={{ flex: 1, minWidth: 0 }}>
        <Typography variant="subtitle2" fontWeight="bold" color="text.primary" component="span">
          {t('purchase:purchase.iap.inviteRewardCard', { months })}
        </Typography>
      </Stack>
      <Button
        size="small"
        variant="contained"
        color="warning"
        onClick={onInvite}
        data-testid="invite-reward-btn"
        sx={{ textTransform: 'none', fontWeight: 700, flexShrink: 0 }}
      >
        {t('purchase:purchase.iap.inviteRewardCta')}
      </Button>
    </Box>
  );
}
