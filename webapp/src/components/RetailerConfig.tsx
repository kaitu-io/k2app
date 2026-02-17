import {
  Card,
  Box,
  Stack,
  Avatar,
  Typography,
  Button,
  Paper,
  CircularProgress,
  alpha,
} from '@mui/material';
import {
  EmojiEvents as TrophyIcon,
  MonetizationOn as MoneyIcon,
  ArrowForwardIos as ArrowIcon,
} from '@mui/icons-material';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import type { MyInviteCode, DataUser } from '../services/api-types';

interface RetailerConfigProps {
  invite: MyInviteCode | null;
  user: DataUser | null;
  loading: boolean;
}

export default function RetailerConfig({ invite, user, loading }: RetailerConfigProps) {
  const { t } = useTranslation();
  const navigate = useNavigate();

  const cashbackPercent = user?.retailerConfig?.cashbackPercent || 0;
  const purchaseReward = invite?.purchaseReward || 0;

  return (
    <Card
      elevation={0}
      sx={(theme) => ({
        borderRadius: 3,
        border: '1px solid',
        borderColor: 'divider',
        overflow: 'hidden',
        background: theme.palette.mode === 'dark'
          ? `linear-gradient(135deg, ${alpha(theme.palette.success.main, 0.08)} 0%, ${alpha(theme.palette.primary.main, 0.08)} 100%)`
          : `linear-gradient(135deg, ${alpha(theme.palette.success.light, 0.08)} 0%, ${alpha(theme.palette.primary.light, 0.08)} 100%)`,
      })}
    >
      <Box sx={{ p: 2 }}>
        <Stack direction="row" alignItems="center" spacing={1.5} sx={{ mb: 2 }}>
          <Avatar
            sx={{
              width: 28,
              height: 28,
              bgcolor: 'success.main',
            }}
          >
            <TrophyIcon sx={{ fontSize: 16 }} />
          </Avatar>
          <Typography variant="h6" fontWeight={700} sx={{ flex: 1, fontSize: '1rem' }}>
            {t('invite:invite.retailerRewards', '分销奖励')}
          </Typography>
          <Button
            size="small"
            variant="outlined"
            endIcon={<ArrowIcon sx={{ fontSize: 14 }} />}
            onClick={() => navigate('/retailer-rule')}
            sx={{
              textTransform: 'none',
              fontSize: '0.75rem',
              py: 0.5,
              px: 1.25,
            }}
          >
            {t('invite:invite.retailerDetailedRules', '详细规则')}
          </Button>
        </Stack>

        <Stack direction="row" spacing={1.5}>
          {/* 返现比例 */}
          <Paper
            elevation={0}
            sx={{
              flex: 1,
              p: 1.5,
              borderRadius: 2,
              bgcolor: 'background.paper',
              border: '1px solid',
              borderColor: 'divider',
            }}
          >
            <Stack direction="row" alignItems="center" spacing={1.5}>
              <Avatar
                sx={{
                  width: 36,
                  height: 36,
                  bgcolor: alpha('#4CAF50', 0.15),
                }}
              >
                <MoneyIcon sx={{ fontSize: 20, color: 'success.main' }} />
              </Avatar>
              <Box sx={{ flex: 1, minWidth: 0 }}>
                <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 0.25 }}>
                  {t('invite:invite.retailerCashbackPercent', '返现比例')}
                </Typography>
                {loading ? (
                  <CircularProgress size={14} />
                ) : (
                  <Typography variant="h6" color="success.main" fontWeight={700} sx={{ lineHeight: 1 }}>
                    {cashbackPercent}%
                  </Typography>
                )}
              </Box>
            </Stack>
          </Paper>

          {/* 已获得奖励 */}
          {purchaseReward > 0 && (
            <Paper
              elevation={0}
              sx={{
                flex: 1,
                p: 1.5,
                borderRadius: 2,
                bgcolor: 'background.paper',
                border: '1px solid',
                borderColor: 'divider',
              }}
            >
              <Stack direction="row" alignItems="center" spacing={1.5}>
                <Avatar
                  sx={{
                    width: 36,
                    height: 36,
                    bgcolor: alpha('#FF9800', 0.15),
                  }}
                >
                  <TrophyIcon sx={{ fontSize: 20, color: 'warning.main' }} />
                </Avatar>
                <Box sx={{ flex: 1, minWidth: 0 }}>
                  <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 0.25 }}>
                    {t('invite:invite.earned', '已获得')}
                  </Typography>
                  <Typography variant="h6" color="warning.main" fontWeight={700} sx={{ lineHeight: 1 }}>
                    {purchaseReward} {t('invite:invite.days', '天')}
                  </Typography>
                </Box>
              </Stack>
            </Paper>
          )}
        </Stack>
      </Box>
    </Card>
  );
}
