import { Box, Typography, Stack } from '@mui/material';
import {
  Devices as DevicesIcon,
  Public as GlobalIcon,
  RocketLaunch as ZeroMaintenanceIcon,
  AutorenewOutlined as OptimizationIcon,
  SupportAgent as SupportIcon,
  Router as RouterIcon,
} from '@mui/icons-material';
import { useTranslation } from 'react-i18next';

interface MembershipBenefitsProps {
  maxDevice?: number; // 0 or undefined = default 5
  maxRouterDevice?: number; // 0 = no router, >0 = supported
  maxLanClient?: number; // 0 = no LAN, -1 = unlimited, >0 = exact
}

export default function MembershipBenefits({ maxDevice, maxRouterDevice, maxLanClient }: MembershipBenefitsProps) {
  const { t } = useTranslation();
  const deviceCount = (maxDevice && maxDevice > 0) ? maxDevice : 5;
  const hasRouter = (maxRouterDevice ?? 0) > 0;
  const lanClientLabel = maxLanClient === -1
    ? t('purchase:purchase.features.unlimited')
    : maxLanClient && maxLanClient > 0
      ? String(maxLanClient)
      : null;

  const benefits: { key: string; icon: typeof DevicesIcon; color: string; count?: number; extra?: string | null }[] = [
    { key: 'multiDevice', icon: DevicesIcon, color: '#2196f3', count: deviceCount },
    { key: 'globalNodes', icon: GlobalIcon, color: '#4caf50' },
    ...(hasRouter ? [{
      key: 'routerAccess',
      icon: RouterIcon,
      color: '#ff5722',
      extra: lanClientLabel,
    }] : []),
    { key: 'zeroMaintenance', icon: ZeroMaintenanceIcon, color: '#ff9800' },
    { key: 'continuousOptimization', icon: OptimizationIcon, color: '#7c4dff' },
    { key: 'prioritySupport', icon: SupportIcon, color: '#9c27b0' },
  ];

  return (
    <Box sx={{ mb: 2 }}>
      <Typography variant="subtitle1" sx={{ fontWeight: 600, mb: 1.5, fontSize: '1rem' }} component="span">
        {t('purchase:purchase.memberBenefits')}
      </Typography>
      <Stack spacing={1}>
        {benefits.map(({ key, icon: Icon, color, count, extra }) => (
          <Box
            key={key}
            sx={{
              display: 'flex',
              alignItems: 'center',
              gap: 1.5,
              py: 0.8,
              px: 1.5,
              borderRadius: 1.5,
              bgcolor: 'action.hover',
            }}
          >
            <Icon sx={{ color, fontSize: 22 }} />
            <Box sx={{ flex: 1 }}>
              <Typography variant="body2" sx={{ fontWeight: 500, lineHeight: 1.4 }} component="span">
                {count ? (
                  <>
                    <Box
                      component="span"
                      sx={{
                        fontSize: '1.3rem',
                        fontWeight: 800,
                        color: 'primary.main',
                        mr: 0.3,
                      }}
                    >
                      {count}
                    </Box>
                    {t('purchase:purchase.features.multiDevice', { count: '' } as Record<string, unknown>).replace(/^\s+/, '')}
                  </>
                ) : extra ? (
                  <>
                    {t(`purchase:purchase.features.${key}`)}
                    <Box component="span" sx={{ ml: 0.5, fontWeight: 700, color: 'primary.main' }}>
                      ({extra})
                    </Box>
                  </>
                ) : (
                  t(`purchase:purchase.features.${key}`)
                )}
              </Typography>
              <Typography variant="caption" color="text.secondary" component="span" sx={{ display: 'block', mt: 0.2 }}>
                {t(`purchase:purchase.features.${key}Desc`)}
              </Typography>
            </Box>
          </Box>
        ))}
      </Stack>
    </Box>
  );
}
