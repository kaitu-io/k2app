import {
  Card,
  Box,
  Stack,
  Avatar,
  Typography,
  Button,
  Paper,
  Chip,
  CircularProgress,
  alpha,
} from '@mui/material';
import {
  EmojiEvents as TrophyIcon,
  ShoppingCart as ShoppingCartIcon,
  ArrowForwardIos as ArrowIcon,
  CheckCircle as CheckCircleIcon,
} from '@mui/icons-material';
import { useNavigate } from 'react-router-dom';
import { useTranslation } from 'react-i18next';
import { HighlightedText } from './HighlightedText';
import type { MyInviteCode } from '../services/api-types';

interface InviteRuleProps {
  invite: MyInviteCode | null;
  loading: boolean;
}

export default function InviteRule({ invite, loading }: InviteRuleProps) {
  const { t } = useTranslation();
  const navigate = useNavigate();

  return (
    <Card
      elevation={0}
      sx={(theme) => ({
        borderRadius: 3,
        border: '1px solid',
        borderColor: 'divider',
        overflow: 'hidden',
        background: theme.palette.mode === 'dark'
          ? `linear-gradient(135deg, ${alpha(theme.palette.warning.main, 0.08)} 0%, ${alpha(theme.palette.success.main, 0.08)} 100%)`
          : `linear-gradient(135deg, ${alpha(theme.palette.warning.light, 0.08)} 0%, ${alpha(theme.palette.success.light, 0.08)} 100%)`,
      })}
    >
      <Box sx={{ p: 2.5 }}>
        <Stack direction="row" alignItems="center" spacing={1.5} sx={{ mb: 2.5 }}>
          <Avatar
            sx={{
              width: 32,
              height: 32,
              bgcolor: 'primary.main',
            }}
          >
            <TrophyIcon sx={{ fontSize: 18 }} />
          </Avatar>
          <Typography variant="h6" fontWeight={700} sx={{ flex: 1 }}>
            {t('invite:invite.rewardRules')}
          </Typography>
          <Button
            size="small"
            variant="outlined"
            endIcon={<ArrowIcon sx={{ fontSize: 14 }} />}
            onClick={() => navigate('/pro-histories?type=invite_purchase_reward&from=/invite')}
            sx={{
              textTransform: 'none',
              fontSize: '0.8rem',
              py: 0.5,
              px: 1.5,
            }}
          >
            {t('invite:invite.viewHistory')}
          </Button>
        </Stack>

        {/* 规则说明 */}
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }} component="div">
          <HighlightedText text={t('invite:invite.ruleDescription')} />
        </Typography>

        <Stack spacing={2}>
          {/* Purchase Reward */}
          <Paper
            elevation={0}
            sx={{
              p: 2,
              borderRadius: 2,
              bgcolor: 'background.paper',
              border: '1px solid',
              borderColor: 'divider',
            }}
          >
            <Stack direction="row" alignItems="flex-start" spacing={2}>
              <Avatar
                sx={{
                  width: 40,
                  height: 40,
                  bgcolor: alpha('#FF9800', 0.15),
                }}
              >
                <ShoppingCartIcon sx={{ fontSize: 22, color: 'warning.main' }} />
              </Avatar>
              <Box sx={{ flex: 1 }}>
                <Typography variant="body2" fontWeight={600} sx={{ mb: 0.5 }}>
                  {t('invite:invite.purchaseReward')}
                </Typography>
                {loading ? (
                  <CircularProgress size={16} />
                ) : (
                  <Typography variant="h6" color="warning.main" fontWeight={700}>
                    +{invite?.config.purchaseRewardDays || 0} {t('invite:invite.days')}
                  </Typography>
                )}
                {invite && invite.purchaseReward > 0 && (
                  <Chip
                    icon={<CheckCircleIcon sx={{ fontSize: 14 }} />}
                    label={`${t('invite:invite.earned')} ${invite.purchaseReward} ${t('invite:invite.days')}`}
                    size="small"
                    color="warning"
                    sx={{ mt: 1, fontWeight: 600 }}
                  />
                )}
              </Box>
            </Stack>
          </Paper>
        </Stack>
      </Box>
    </Card>
  );
}
