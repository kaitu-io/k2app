import { useState, useEffect } from 'react';
import {
  Box,
  Card,
  Typography,
  Grid,
  Stack,
  Avatar,
  Button,
  alpha,
  CircularProgress,
  LinearProgress,
  Chip,
  Divider,
} from '@mui/material';
import {
  MonetizationOn as MoneyIcon,
  AccountBalanceWallet as WalletIcon,
  TrendingUp as TrendingUpIcon,
  Pending as PendingIcon,
  Launch as LaunchIcon,
  ArrowForward as ArrowForwardIcon,
  ArrowForwardIos as ArrowIcon,
  EmojiEvents as TrophyIcon,
  People as PeopleIcon,
} from '@mui/icons-material';
import { useTranslation } from 'react-i18next';
import { useUser } from '../hooks/useUser';

import { useAppLinks } from '../hooks/useAppLinks';
import type { Wallet, RetailerStats } from '../services/api-types';
import { k2api } from '../services/k2api';

// 等级对应的颜色
const levelColors: Record<number, string> = {
  1: '#9E9E9E', // L1 灰色
  2: '#2196F3', // L2 蓝色
  3: '#9C27B0', // L3 紫色
  4: '#FF9800', // L4 金色
};

// 获取等级的本地化名称
const getLevelName = (level: number, t: (key: string) => string): string => {
  const levelKeys: Record<number, string> = {
    1: 'retailerStats.levelL1',
    2: 'retailerStats.levelL2',
    3: 'retailerStats.levelL3',
    4: 'retailerStats.levelL4',
  };
  return t(levelKeys[level] || levelKeys[1]);
};

export default function RetailerStatsOverview() {
  const { t } = useTranslation();
  const { user } = useUser();
  const { links } = useAppLinks();

  const [wallet, setWallet] = useState<Wallet | null>(null);
  const [stats, setStats] = useState<RetailerStats | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (user?.isRetailer) {
      loadRetailerData();
    }
  }, [user?.isRetailer]);

  const loadRetailerData = async () => {
    setLoading(true);
    setError(null);
    try {
      const [walletRes, statsRes] = await Promise.all([
        k2api().exec<Wallet>('api_request', {
          method: 'GET',
          path: '/api/wallet',
        }),
        k2api().exec<RetailerStats>('api_request', {
          method: 'GET',
          path: '/api/retailer/stats',
        }),
      ]);

      if (walletRes.code === 0 && walletRes.data) {
        setWallet(walletRes.data);
      }
      if (statsRes.code === 0 && statsRes.data) {
        setStats(statsRes.data);
      }
    } catch (err) {
      console.error('Failed to load retailer data:', err);
      setError(t('retailer:retailerStats.loadError'));
    } finally {
      setLoading(false);
    }
  };

  // 非分销商：显示"成为分销商"CTA
  if (!user?.isRetailer) {
    return (
      <Card
        elevation={0}
        sx={(theme) => ({
          mt: 2,
          borderRadius: 3,
          border: '2px solid',
          borderColor: 'primary.main',
          overflow: 'hidden',
          background:
            theme.palette.mode === 'dark'
              ? `linear-gradient(135deg, ${alpha(theme.palette.primary.main, 0.12)} 0%, ${alpha(theme.palette.secondary.main, 0.08)} 100%)`
              : `linear-gradient(135deg, ${alpha(theme.palette.primary.light, 0.12)} 0%, ${alpha(theme.palette.secondary.light, 0.08)} 100%)`,
        })}
      >
        <Box sx={{ p: 2, textAlign: 'center' }}>
          <Avatar
            sx={{
              width: 40,
              height: 40,
              bgcolor: 'primary.main',
              mx: 'auto',
              mb: 1.5,
            }}
          >
            <MoneyIcon sx={{ fontSize: 24 }} />
          </Avatar>
          <Typography variant="h6" fontWeight={700} gutterBottom sx={{ fontSize: '1rem' }}>
            {t('retailer:retailer.becomeRetailerTitle', '成为分销商，赚取推广收益')}
          </Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 2, fontSize: '0.85rem' }}>
            {t('retailer:retailer.becomeRetailerDesc', '邀请用户付费后可获得返现佣金，轻松实现被动收入')}
          </Typography>
          <Button
            variant="contained"
            size="medium"
            fullWidth
            endIcon={<LaunchIcon />}
            onClick={() => window._platform!.openExternal?.(links.retailerRulesUrl)}
            sx={{
              borderRadius: 2,
              textTransform: 'none',
              fontWeight: 600,
              py: 1,
              boxShadow: (theme) => `0 4px 12px ${alpha(theme.palette.primary.main, 0.3)}`,
            }}
          >
            {t('retailer:retailer.becomeRetailerButton', '了解分销商计划')}
          </Button>
        </Box>
      </Card>
    );
  }

  // 分销商：显示战果概览
  return (
    <Card
      elevation={0}
      sx={{
        mt: 2,
        borderRadius: 3,
        border: '1px solid',
        borderColor: 'divider',
      }}
    >
      <Box sx={{ p: 2.5 }}>
        {/* Header */}
        <Stack direction="row" alignItems="center" spacing={1.5} sx={{ mb: 2 }}>
          <Avatar
            sx={{
              width: 40,
              height: 40,
              bgcolor: alpha('#9C27B0', 0.15),
            }}
          >
            <TrendingUpIcon sx={{ fontSize: 24, color: '#9C27B0' }} />
          </Avatar>
          <Box sx={{ flex: 1 }}>
            <Typography variant="h6" fontWeight={700} sx={{ fontSize: '1.1rem' }}>
              {t('retailer:retailerStats.title', '分销战果')}
            </Typography>
            <Typography variant="body2" color="text.secondary" component="span" sx={{ fontSize: '0.85rem' }}>
              {t('retailer:retailerStats.subtitle', '查看您的推广收益')}
            </Typography>
          </Box>
          <Button
            variant="text"
            size="small"
            endIcon={<ArrowIcon sx={{ fontSize: 14 }} />}
            onClick={() => window._platform!.openExternal?.(links.retailerRulesUrl)}
            sx={{
              textTransform: 'none',
              fontWeight: 600,
              fontSize: '0.875rem',
              color: 'primary.main',
            }}
          >
            {t('invite:invite.retailerRules', '分销规则')}
          </Button>
        </Stack>

        {loading ? (
          <Box sx={{ display: 'flex', justifyContent: 'center', py: 3 }}>
            <CircularProgress size={32} />
          </Box>
        ) : error ? (
          <Typography variant="body2" color="error" align="center" sx={{ py: 3 }}>
            {error}
          </Typography>
        ) : (
          <>
            {/* Level Info Card */}
            {stats && (
              <Box
                sx={{
                  p: 2,
                  mb: 2,
                  borderRadius: 2,
                  bgcolor: alpha(levelColors[stats.level] || levelColors[1], 0.08),
                  border: '1px solid',
                  borderColor: alpha(levelColors[stats.level] || levelColors[1], 0.2),
                }}
              >
                {/* Level Header */}
                <Stack direction="row" alignItems="center" spacing={1.5} sx={{ mb: 1.5 }}>
                  <Avatar
                    sx={{
                      width: 36,
                      height: 36,
                      bgcolor: levelColors[stats.level] || levelColors[1],
                    }}
                  >
                    <TrophyIcon sx={{ fontSize: 20 }} />
                  </Avatar>
                  <Box sx={{ flex: 1 }}>
                    <Stack direction="row" alignItems="center" spacing={1}>
                      <Typography variant="subtitle1" fontWeight={700}>
                        {getLevelName(stats.level, t)}
                      </Typography>
                      <Chip
                        size="small"
                        label={`L${stats.level}`}
                        sx={{
                          height: 20,
                          fontSize: '0.7rem',
                          fontWeight: 700,
                          bgcolor: levelColors[stats.level] || levelColors[1],
                          color: 'white',
                        }}
                      />
                    </Stack>
                    <Stack direction="row" alignItems="center" spacing={0.5}>
                      <PeopleIcon sx={{ fontSize: 14, color: 'text.secondary' }} />
                      <Typography variant="caption" color="text.secondary">
                        {t('retailer:retailerStats.paidUsers', '累计付费用户')}: {stats.paidUserCount}
                      </Typography>
                    </Stack>
                  </Box>
                </Stack>

                {/* Commission Rates */}
                <Grid container spacing={1.5} sx={{ mb: 1.5 }}>
                  <Grid item xs={6}>
                    <Box sx={{ textAlign: 'center', p: 1, borderRadius: 1, bgcolor: 'background.paper' }}>
                      <Typography variant="caption" color="text.secondary" sx={{ fontSize: '0.7rem' }}>
                        {t('retailer:retailerStats.firstOrderCommission', '首单分成')}
                      </Typography>
                      <Typography variant="h6" fontWeight={700} color="success.main" sx={{ fontSize: '1.1rem' }}>
                        {stats.firstOrderPercent}%
                      </Typography>
                    </Box>
                  </Grid>
                  <Grid item xs={6}>
                    <Box sx={{ textAlign: 'center', p: 1, borderRadius: 1, bgcolor: 'background.paper' }}>
                      <Typography variant="caption" color="text.secondary" sx={{ fontSize: '0.7rem' }}>
                        {t('retailer:retailerStats.renewalCommission', '续费分成')}
                      </Typography>
                      <Typography variant="h6" fontWeight={700} color="primary.main" sx={{ fontSize: '1.1rem' }}>
                        {stats.renewalPercent > 0 ? `${stats.renewalPercent}%` : t('retailer:retailerStats.noRenewalCommission', '无')}
                      </Typography>
                    </Box>
                  </Grid>
                </Grid>

                {/* Upgrade Progress */}
                {stats.nextLevel ? (
                  <Box>
                    <Stack direction="row" justifyContent="space-between" alignItems="center" sx={{ mb: 0.5 }}>
                      <Typography variant="caption" color="text.secondary">
                        {t('retailer:retailerStats.nextLevel', '下一等级')}: {getLevelName(stats.nextLevel, t)}
                      </Typography>
                      <Typography variant="caption" fontWeight={600} color="text.primary">
                        {stats.progressPercent}%
                      </Typography>
                    </Stack>
                    <LinearProgress
                      variant="determinate"
                      value={stats.progressPercent}
                      sx={{
                        height: 6,
                        borderRadius: 3,
                        bgcolor: (theme) => alpha(theme.palette.primary.main, 0.1),
                        '& .MuiLinearProgress-bar': {
                          borderRadius: 3,
                          bgcolor: levelColors[stats.nextLevel] || 'primary.main',
                        },
                      }}
                    />
                    <Typography variant="caption" color="text.secondary" sx={{ mt: 0.5, display: 'block' }}>
                      {stats.nextLevelRequirement && stats.paidUserCount < stats.nextLevelRequirement
                        ? t('retailer:retailerStats.usersNeeded', { count: stats.nextLevelRequirement - stats.paidUserCount })
                        : stats.needContentProof
                          ? t('retailer:retailerStats.contentProofNeeded', '升级还需提交内容创作证明')
                          : ''}
                    </Typography>
                  </Box>
                ) : (
                  <Typography variant="caption" color="success.main" fontWeight={600} sx={{ display: 'block', textAlign: 'center' }}>
                    {t('retailer:retailerStats.maxLevelReached', '已达最高等级')}
                  </Typography>
                )}
              </Box>
            )}

            <Divider sx={{ mb: 2 }} />

            {/* Stats Grid */}
            <Grid container spacing={1.5} sx={{ mb: 2 }}>
              {/* 钱包余额 */}
              <Grid item xs={4}>
                <Box
                  onClick={() => window._platform!.openExternal?.(links.walletUrl)}
                  sx={{
                    p: 1.5,
                    borderRadius: 2,
                    bgcolor: (theme) => alpha(theme.palette.primary.main, 0.08),
                    cursor: 'pointer',
                    transition: 'all 0.2s',
                    '&:hover': {
                      bgcolor: (theme) => alpha(theme.palette.primary.main, 0.12),
                      transform: 'translateY(-2px)',
                    },
                  }}
                >
                  <Stack spacing={0.5} alignItems="center">
                    <WalletIcon sx={{ fontSize: 24, color: 'primary.main' }} />
                    <Typography variant="caption" color="text.secondary" sx={{ fontSize: '0.7rem' }}>
                      {t('retailer:retailerStats.walletBalance', '钱包余额')}
                    </Typography>
                    <Typography variant="h6" fontWeight={700} sx={{ fontSize: '1rem' }}>
                      ${((wallet?.balance || 0) / 100).toFixed(2)}
                    </Typography>
                  </Stack>
                </Box>
              </Grid>

              {/* 累计返现 */}
              <Grid item xs={4}>
                <Box
                  onClick={() => window._platform!.openExternal?.(links.walletUrl)}
                  sx={{
                    p: 1.5,
                    borderRadius: 2,
                    bgcolor: (theme) => alpha(theme.palette.success.main, 0.08),
                    cursor: 'pointer',
                    transition: 'all 0.2s',
                    '&:hover': {
                      bgcolor: (theme) => alpha(theme.palette.success.main, 0.12),
                      transform: 'translateY(-2px)',
                    },
                  }}
                >
                  <Stack spacing={0.5} alignItems="center">
                    <MoneyIcon sx={{ fontSize: 24, color: 'success.main' }} />
                    <Typography variant="caption" color="text.secondary" sx={{ fontSize: '0.7rem' }}>
                      {t('retailer:retailerStats.totalCashback', '累计返现')}
                    </Typography>
                    <Typography variant="h6" fontWeight={700} sx={{ fontSize: '1rem' }}>
                      ${((wallet?.totalIncome || 0) / 100).toFixed(2)}
                    </Typography>
                  </Stack>
                </Box>
              </Grid>

              {/* 待提现 */}
              <Grid item xs={4}>
                <Box
                  onClick={() => window._platform!.openExternal?.(links.walletUrl)}
                  sx={{
                    p: 1.5,
                    borderRadius: 2,
                    bgcolor: (theme) => alpha(theme.palette.warning.main, 0.08),
                    cursor: 'pointer',
                    transition: 'all 0.2s',
                    '&:hover': {
                      bgcolor: (theme) => alpha(theme.palette.warning.main, 0.12),
                      transform: 'translateY(-2px)',
                    },
                  }}
                >
                  <Stack spacing={0.5} alignItems="center">
                    <PendingIcon sx={{ fontSize: 24, color: 'warning.main' }} />
                    <Typography variant="caption" color="text.secondary" sx={{ fontSize: '0.7rem' }}>
                      {t('retailer:retailerStats.pendingWithdraw', '待提现')}
                    </Typography>
                    <Typography variant="h6" fontWeight={700} sx={{ fontSize: '1rem' }}>
                      ${((wallet?.availableBalance || 0) / 100).toFixed(2)}
                    </Typography>
                  </Stack>
                </Box>
              </Grid>
            </Grid>

            {/* Quick Action */}
            <Button
              variant="contained"
              size="medium"
              fullWidth
              endIcon={<ArrowForwardIcon />}
              onClick={() => window._platform!.openExternal?.(links.walletUrl)}
              sx={{
                borderRadius: 2,
                textTransform: 'none',
                fontWeight: 600,
                py: 1.25,
              }}
            >
              {t('retailer:retailerStats.viewAndManage', '查看与操作')}
            </Button>
          </>
        )}
      </Box>
    </Card>
  );
}
