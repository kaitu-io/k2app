import { Box, Stack, Switch, Typography } from '@mui/material';
import { useTranslation } from 'react-i18next';

import { useConfigStore } from '../stores/config.store';
import { useVPNMachine } from '../stores/vpn-machine.store';

export function AlwaysOnToggle() {
  const { t } = useTranslation('dashboard');
  const alwaysOn = useConfigStore((s) => s.alwaysOn);
  const setAlwaysOn = useConfigStore((s) => s.setAlwaysOn);
  const { isInteractive } = useVPNMachine();

  return (
    <Box sx={{ py: 1.5 }}>
      <Stack direction="row" justifyContent="space-between" alignItems="flex-start" spacing={2}>
        <Box sx={{ flex: 1, minWidth: 0 }}>
          <Typography variant="body2" fontWeight={600}>
            {t('dashboard.alwaysOn.title')}
          </Typography>
          <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 0.5 }}>
            {t('dashboard.alwaysOn.description')}
          </Typography>
          <Typography variant="caption" color="warning.main" sx={{ display: 'block', mt: 0.5 }}>
            {t('dashboard.alwaysOn.warning')}
          </Typography>
        </Box>
        <Switch
          checked={alwaysOn}
          onChange={(e) => { setAlwaysOn(e.target.checked); }}
          disabled={isInteractive}
          inputProps={{ 'aria-label': t('dashboard.alwaysOn.title') }}
        />
      </Stack>
    </Box>
  );
}
