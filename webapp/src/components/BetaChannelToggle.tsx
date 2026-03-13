import { useState, useCallback, useEffect } from 'react';
import {
  ListItem,
  ListItemIcon,
  ListItemText,
  Typography,
  Switch,
  Dialog,
  DialogTitle,
  DialogContent,
  DialogContentText,
  DialogActions,
  Button,
  CircularProgress,
  Divider,
} from '@mui/material';
import { Science as ScienceIcon } from '@mui/icons-material';
import { useTranslation } from 'react-i18next';
import { cloudApi } from '../services/cloud-api';
import { useUser } from '../hooks/useUser';

export default function BetaChannelToggle() {
  const { t } = useTranslation('account');
  const updater = window._platform?.updater;
  const platform = window._platform;
  const { user } = useUser();

  // Initial state: desktop/android use local updater.channel, iOS uses server-side betaOptedIn
  const getInitialBeta = () => {
    if (updater?.setChannel) return updater.channel === 'beta';
    return user?.betaOptedIn ?? false;
  };

  const [isBeta, setIsBeta] = useState(getInitialBeta);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [switching, setSwitching] = useState(false);

  // Sync from server when user data updates (SWR background revalidate)
  useEffect(() => {
    if (!updater?.setChannel && user) {
      setIsBeta(user.betaOptedIn ?? false);
    }
  }, [user?.betaOptedIn]);

  // Don't render if user is not logged in
  if (!user) return null;

  const isIos = platform?.os === 'ios';
  const description = isIos ? t('betaProgram.descriptionIos') : t('betaProgram.description');

  const handleToggleClick = () => {
    setDialogOpen(true);
  };

  const handleConfirm = useCallback(async () => {
    const newBeta = !isBeta;
    const newChannel = newBeta ? 'beta' : 'stable';
    setSwitching(true);
    setDialogOpen(false);

    try {
      // Local channel switch (desktop + android only)
      if (updater?.setChannel) {
        await updater.setChannel(newChannel);
      }
      setIsBeta(newBeta);

      // API sync (all platforms, fire-and-forget)
      cloudApi.request('PUT', '/api/user/beta-channel', { opted_in: newBeta }).catch((e: any) => {
        console.warn('[BetaToggle] Failed to sync beta status to API:', e);
      });
    } catch (e) {
      console.error('[BetaToggle] Failed to switch channel:', e);
    } finally {
      setSwitching(false);
    }
  }, [isBeta, updater]);

  return (
    <>
      <Divider />
      <ListItem sx={{ py: 1.5 }}>
        <ListItemIcon>
          <ScienceIcon />
        </ListItemIcon>
        <ListItemText
          primary={
            <Typography variant="body2" sx={{ fontWeight: 500, fontSize: '0.9rem' }}>
              {t('betaProgram.title')}
            </Typography>
          }
          secondary={
            <Typography variant="caption" color="text.secondary">
              {description}
            </Typography>
          }
        />
        {switching ? (
          <CircularProgress size={24} />
        ) : (
          <Switch
            checked={isBeta}
            onChange={handleToggleClick}
            color="warning"
          />
        )}
      </ListItem>

      <Dialog open={dialogOpen} onClose={() => setDialogOpen(false)}>
        <DialogTitle>
          {isBeta ? t('betaProgram.disableConfirm') : t('betaProgram.enableConfirm')}
        </DialogTitle>
        <DialogContent>
          <DialogContentText>
            {isBeta ? t('betaProgram.disableWarning') : t('betaProgram.enableWarning')}
          </DialogContentText>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setDialogOpen(false)}>
            {t('common:common.cancel', '取消')}
          </Button>
          <Button
            onClick={handleConfirm}
            color={isBeta ? 'primary' : 'warning'}
            variant="contained"
          >
            {isBeta ? t('betaProgram.disableConfirm') : t('betaProgram.enableConfirm')}
          </Button>
        </DialogActions>
      </Dialog>
    </>
  );
}
