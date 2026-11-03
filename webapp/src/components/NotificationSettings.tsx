import { useCallback, useEffect, useState } from 'react';
import {
  Button,
  Divider,
  ListItem,
  ListItemIcon,
  ListItemText,
  Switch,
  Typography,
} from '@mui/material';
import {
  Campaign as CampaignIcon,
  NotificationsActive as NotificationsActiveIcon,
} from '@mui/icons-material';
import { useTranslation } from 'react-i18next';
import { useAuthStore } from '../stores/auth.store';
import {
  getMarketingOptIn,
  getPushPermission,
  openPushSettings,
  pushSupported,
  requestPushPermission,
  setMarketingOptIn,
  syncPushRegistration,
} from '../services/push-service';
import type { PushPermission } from '../types/kaitu-core';

/**
 * Account → notification settings (spec §5.3). Hidden where push is unsupported.
 * Row 1: system permission (enable / go to Settings). Row 2: marketing opt-in,
 * stored per user on the server; account reminders don't depend on it.
 */
export default function NotificationSettings() {
  const { t } = useTranslation('account');
  const supported = pushSupported();
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated);
  const [permission, setPermission] = useState<PushPermission | null>(null);
  const [marketing, setMarketing] = useState<boolean | null>(null);
  const [busy, setBusy] = useState(false);

  const refreshPermission = useCallback(async () => {
    setPermission(await getPushPermission());
  }, []);

  useEffect(() => {
    if (!supported) return undefined;
    void refreshPermission();
    const onVisible = () => {
      if (document.visibilityState === 'visible') void refreshPermission();
    };
    document.addEventListener('visibilitychange', onVisible);
    return () => document.removeEventListener('visibilitychange', onVisible);
  }, [supported, refreshPermission]);

  useEffect(() => {
    if (!supported || !isAuthenticated) return;
    let cancelled = false;
    void getMarketingOptIn().then((v) => {
      if (!cancelled) setMarketing(v);
    });
    return () => {
      cancelled = true;
    };
  }, [supported, isAuthenticated]);

  if (!supported) return null;

  const handleEnable = async () => {
    setBusy(true);
    try {
      const p = await requestPushPermission();
      setPermission(p);
      if (p === 'granted') await syncPushRegistration({ force: true });
    } finally {
      setBusy(false);
    }
  };

  const handleMarketingToggle = async () => {
    if (marketing === null) return;
    const next = !marketing;
    setMarketing(next);
    if (!(await setMarketingOptIn(next))) setMarketing(!next);
  };

  let action: JSX.Element | null = null;
  if (permission === 'granted') {
    action = <Typography variant="caption" color="text.secondary">{t('push.statusOn')}</Typography>;
  } else if (permission === 'denied') {
    action = (
      <Button size="small" variant="outlined" onClick={() => void openPushSettings()} sx={{ textTransform: 'none' }}>
        {t('push.openSettings')}
      </Button>
    );
  } else if (permission === 'prompt') {
    action = (
      <Button size="small" variant="outlined" disabled={busy} onClick={() => void handleEnable()} sx={{ textTransform: 'none' }}>
        {t('push.enable')}
      </Button>
    );
  }

  return (
    <>
      <ListItem sx={{ py: 1.5 }} data-testid="account-push-permission" secondaryAction={action}>
        <ListItemIcon>
          <NotificationsActiveIcon />
        </ListItemIcon>
        <ListItemText
          primary={
            <Typography variant="body2" sx={{ fontWeight: 500, fontSize: '0.9rem' }}>
              {t('push.settingTitle')}
            </Typography>
          }
          secondary={
            permission === 'denied' ? (
              <Typography variant="caption" color="text.secondary">{t('push.statusOff')}</Typography>
            ) : undefined
          }
        />
      </ListItem>
      <Divider />
      {isAuthenticated && (
        <>
          <ListItem sx={{ py: 1.5 }} data-testid="account-push-marketing">
            <ListItemIcon>
              <CampaignIcon />
            </ListItemIcon>
            <ListItemText
              primary={
                <Typography variant="body2" sx={{ fontWeight: 500, fontSize: '0.9rem' }}>
                  {t('push.marketingTitle')}
                </Typography>
              }
              secondary={
                <Typography variant="caption" color="text.secondary">{t('push.marketingDesc')}</Typography>
              }
            />
            <Switch
              edge="end"
              checked={marketing === true}
              disabled={marketing === null}
              onChange={() => void handleMarketingToggle()}
              inputProps={{ 'aria-label': t('push.marketingTitle') }}
            />
          </ListItem>
          <Divider />
        </>
      )}
    </>
  );
}
