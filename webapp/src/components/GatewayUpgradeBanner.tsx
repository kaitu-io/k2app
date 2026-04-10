/**
 * Gateway OTA upgrade banner — shown on Dashboard when a newer
 * k2r version is available. Gateway-only (uses _platform.gatewayUpgrade*).
 */
import { useState, useEffect, useRef } from 'react';
import { Alert, AlertTitle, Button, CircularProgress, Typography } from '@mui/material';
import { SystemUpdateAlt as UpdateIcon } from '@mui/icons-material';
import { useTranslation } from 'react-i18next';

export default function GatewayUpgradeBanner() {
  const { t } = useTranslation();
  const [current, setCurrent] = useState('');
  const [latest, setLatest] = useState('');
  const [upgrading, setUpgrading] = useState(false);
  const [restarting, setRestarting] = useState(false);
  const [upgradeFailed, setUpgradeFailed] = useState(false);
  const [checked, setChecked] = useState(false);
  const timerRef = useRef<ReturnType<typeof setTimeout>>();

  useEffect(() => {
    const check = window._platform.gatewayUpgradeCheck;
    if (!check) { setChecked(true); return; }
    check().then(info => {
      if (info) { setCurrent(info.current); setLatest(info.latest); }
      setChecked(true);
    }).catch(() => setChecked(true));
  }, []);

  // Cleanup timer on unmount
  useEffect(() => () => { if (timerRef.current) clearTimeout(timerRef.current); }, []);

  const handleUpgrade = async () => {
    const apply = window._platform.gatewayUpgradeApply;
    if (!apply) return;
    setUpgrading(true);
    setUpgradeFailed(false);
    const ok = await apply();
    if (!ok) {
      setUpgradeFailed(true);
      setUpgrading(false);
      return;
    }
    // Service will restart — show "restarting" after 3s
    timerRef.current = setTimeout(() => setRestarting(true), 3000);
  };

  if (!checked || !latest || !current || latest === current) return null;

  return (
    <Alert
      severity={restarting ? 'warning' : upgradeFailed ? 'error' : 'info'}
      icon={<UpdateIcon />}
      sx={{ mb: 2 }}
      action={
        !restarting ? (
          <Button
            size="small"
            variant="contained"
            disabled={upgrading}
            onClick={handleUpgrade}
            startIcon={upgrading ? <CircularProgress size={16} /> : undefined}
          >
            {upgrading ? t('dashboard:upgrade.upgrading') : t('dashboard:upgrade.apply')}
          </Button>
        ) : undefined
      }
    >
      <AlertTitle>
        {restarting ? t('dashboard:upgrade.restarting') : t('dashboard:upgrade.available')}
      </AlertTitle>
      {restarting
        ? <Typography variant="body2">{t('dashboard:upgrade.restartingDesc')}</Typography>
        : t('dashboard:upgrade.description', { current, latest })}
    </Alert>
  );
}
