import { useState } from 'react';
import {
  Button,
  Checkbox,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
  DialogTitle,
  FormControlLabel,
} from '@mui/material';
import { useTranslation } from 'react-i18next';
import {
  recordPushPromptShown,
  requestPushPermission,
  setMarketingOptIn,
  syncPushRegistration,
} from '../services/push-service';

/**
 * In-app explainer shown before the one-time iOS permission sheet (spec §5.3).
 * The marketing checkbox is unchecked by default — App Review 4.5.4 requires an
 * explicit opt-in for promotional pushes; account reminders need no opt-in.
 */
export default function PushPermissionPrompt({ open, onClose }: { open: boolean; onClose: () => void }) {
  const { t } = useTranslation('account');
  const [marketing, setMarketing] = useState(false);
  const [busy, setBusy] = useState(false);

  const handleLater = () => {
    recordPushPromptShown();
    onClose();
  };

  const handleEnable = async () => {
    recordPushPromptShown();
    setBusy(true);
    try {
      if (marketing) await setMarketingOptIn(true);
      const permission = await requestPushPermission();
      if (permission === 'granted') await syncPushRegistration({ force: true });
    } finally {
      setBusy(false);
      onClose();
    }
  };

  return (
    <Dialog open={open} onClose={busy ? undefined : handleLater} maxWidth="xs" fullWidth data-testid="push-permission-prompt">
      <DialogTitle>{t('push.promptTitle')}</DialogTitle>
      <DialogContent>
        <DialogContentText sx={{ mb: 1.5 }}>{t('push.promptBody')}</DialogContentText>
        <FormControlLabel
          control={
            <Checkbox
              checked={marketing}
              onChange={(e) => setMarketing(e.target.checked)}
              inputProps={{ 'aria-label': t('push.promptMarketing') }}
            />
          }
          label={t('push.promptMarketing')}
        />
      </DialogContent>
      <DialogActions>
        <Button onClick={handleLater} disabled={busy}>
          {t('push.later')}
        </Button>
        <Button variant="contained" onClick={() => void handleEnable()} disabled={busy}>
          {t('push.enable')}
        </Button>
      </DialogActions>
    </Dialog>
  );
}
