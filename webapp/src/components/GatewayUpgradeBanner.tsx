/**
 * Gateway OTA upgrade banner — shown on Dashboard when a newer
 * k2r version is available. Gateway-only (calls /api/upgrade).
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
  const [checked, setChecked] = useState(false);
  const timerRef = useRef<ReturnType<typeof setTimeout>>();

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
      .catch((err) => {
        console.warn('[GatewayUpgrade] check failed:', err);
        setChecked(true);
      });
  }, []);

  // Cleanup timer on unmount
  useEffect(() => () => { if (timerRef.current) clearTimeout(timerRef.current); }, []);

  const handleUpgrade = async () => {
    setUpgrading(true);
    try {
      await fetch('/api/upgrade', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ action: 'apply' }),
      });
      // Service will restart — show "restarting" after 3s
      timerRef.current = setTimeout(() => setRestarting(true), 3000);
    } catch (err) {
      console.warn('[GatewayUpgrade] apply failed:', err);
      setUpgrading(false);
    }
  };

  if (!checked || !latest || !current || latest === current) return null;

  return (
    <Alert
      severity={restarting ? 'warning' : 'info'}
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
