/**
 * Gateway OTA upgrade banner — shown on Dashboard when a newer
 * k2r version is available. Gateway-only (calls /api/upgrade).
 */
import { useState, useEffect } from 'react';
import { Alert, AlertTitle, Button, CircularProgress } from '@mui/material';
import { SystemUpdateAlt as UpdateIcon } from '@mui/icons-material';
import { useTranslation } from 'react-i18next';

export default function GatewayUpgradeBanner() {
  const { t } = useTranslation();
  const [current, setCurrent] = useState('');
  const [latest, setLatest] = useState('');
  const [upgrading, setUpgrading] = useState(false);
  const [checked, setChecked] = useState(false);

  useEffect(() => {
    fetch('/api/upgrade', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ action: 'check' }),
    })
      .then(r => r.json())
      .then(res => {
        if (res.code === 0 && res.data) {
          setCurrent(res.data.current);
          setLatest(res.data.latest);
        }
        setChecked(true);
      })
      .catch(() => setChecked(true));
  }, []);

  const handleUpgrade = async () => {
    setUpgrading(true);
    try {
      await fetch('/api/upgrade', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ action: 'apply' }),
      });
      // Service will restart — page will eventually reconnect
    } catch { /* ignore */ }
  };

  if (!checked || !latest || !current || latest === current) return null;

  return (
    <Alert
      severity="info"
      icon={<UpdateIcon />}
      sx={{ mb: 2 }}
      action={
        <Button
          size="small"
          variant="contained"
          disabled={upgrading}
          onClick={handleUpgrade}
          startIcon={upgrading ? <CircularProgress size={16} /> : undefined}
        >
          {upgrading ? t('dashboard:upgrade.upgrading') : t('dashboard:upgrade.apply')}
        </Button>
      }
    >
      <AlertTitle>{t('dashboard:upgrade.available')}</AlertTitle>
      {t('dashboard:upgrade.description', { current, latest })}
    </Alert>
  );
}
