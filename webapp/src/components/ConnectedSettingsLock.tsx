import { Box, Alert, Button } from '@mui/material';
import { useTranslation } from 'react-i18next';
import { useVPNMachineStore, vpnMachineDispatch } from '../stores';

export default function ConnectedSettingsLock({ children }: { children: React.ReactNode }) {
  const { t } = useTranslation();
  const vpnState = useVPNMachineStore(s => s.state);
  const locked = vpnState !== 'idle';

  if (!locked) return <>{children}</>;

  return (
    <Box>
      <Alert
        severity="info"
        sx={{ mb: 1.5 }}
        action={
          <Button size="small" onClick={() => vpnMachineDispatch('USER_DISCONNECT')}>
            {t('dashboard:disconnectVpn')}
          </Button>
        }
      >
        {t('dashboard:advancedSettingsLocked')}
      </Alert>
      <Box sx={{ pointerEvents: 'none', opacity: 0.45 }}>
        {children}
      </Box>
    </Box>
  );
}
