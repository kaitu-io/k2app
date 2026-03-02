import { useState, useCallback } from 'react';
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

export default function BetaChannelToggle() {
  const { t } = useTranslation('account');
  const updater = window._platform?.updater;

  const [isBeta, setIsBeta] = useState(() => updater?.channel === 'beta');
  const [dialogOpen, setDialogOpen] = useState(false);
  const [switching, setSwitching] = useState(false);

  // Don't render if platform doesn't support channel switching
  if (!updater?.setChannel) return null;

  const handleToggleClick = () => {
    setDialogOpen(true);
  };

  const handleConfirm = useCallback(async () => {
    const newChannel = isBeta ? 'stable' : 'beta';
    setSwitching(true);
    setDialogOpen(false);

    try {
      await updater!.setChannel!(newChannel);
      setIsBeta(newChannel === 'beta');
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
              {t('betaProgram.description')}
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
