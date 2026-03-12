import { Box, Typography, Stack } from '@mui/material';
import {
  Devices as DevicesIcon,
  Public as GlobalIcon,
  RocketLaunch as ZeroMaintenanceIcon,
  AutorenewOutlined as OptimizationIcon,
  SupportAgent as SupportIcon,
} from '@mui/icons-material';
import { useTranslation } from 'react-i18next';

const DEVICE_COUNT = 5;

const benefits = [
  { key: 'multiDevice', icon: DevicesIcon, color: '#2196f3', count: DEVICE_COUNT },
  { key: 'globalNodes', icon: GlobalIcon, color: '#4caf50' },
  { key: 'zeroMaintenance', icon: ZeroMaintenanceIcon, color: '#ff9800' },
  { key: 'continuousOptimization', icon: OptimizationIcon, color: '#7c4dff' },
  { key: 'prioritySupport', icon: SupportIcon, color: '#9c27b0' },
];

export default function MembershipBenefits() {
  const { t } = useTranslation();

  return (
    <Box sx={{ mb: 2 }}>
      <Typography variant="subtitle1" sx={{ fontWeight: 600, mb: 1.5, fontSize: '1rem' }} component="span">
        {t('purchase:purchase.memberBenefits')}
      </Typography>
      <Stack spacing={1}>
        {benefits.map(({ key, icon: Icon, color, count }) => (
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
