/**
 * BackgroundBlockedDialog — guides the user to macOS System Settings when the
 * desktop service is blocked by "Allow in the Background" (#3911).
 *
 * The switch is listed under the code-signing developer name (itemName), not
 * the app name, which is why users don't recognise it and why reinstalling —
 * the old "Resolve" path — never helped. Once the switch is on, launchd starts
 * the service by itself; ServiceAlert closes this dialog when it reconnects.
 */
import {
  Box,
  Button,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Typography,
} from '@mui/material';
import { useTranslation } from 'react-i18next';

const K = 'dashboard:dashboard.serviceFailure.backgroundBlocked';

interface Props {
  open: boolean;
  /** Switch label as macOS shows it. */
  itemName: string;
  /** The blocker is gone (switch on) but the service has not reconnected yet. */
  switchOn: boolean;
  /** Switch on for a while and still no service → offer the reinstall. */
  stalled: boolean;
  reinstalling: boolean;
  onReinstall: () => void;
  canOpenSettings: boolean;
  onOpenSettings: () => void;
  onClose: () => void;
}

export default function BackgroundBlockedDialog({
  open,
  itemName,
  switchOn,
  stalled,
  reinstalling,
  canOpenSettings,
  onOpenSettings,
  onReinstall,
  onClose,
}: Props) {
  const { t } = useTranslation();

  return (
    <Dialog
      open={open}
      onClose={onClose}
      maxWidth="sm"
      fullWidth
      // Render inside #root so it takes the viewport zoom like the rest of the
      // UI — the desktop window is narrower than the 430px design width, and a
      // body-level portal would draw at native scale (webapp/CLAUDE.md
      // "Viewport scaling"; same as FeedbackButton).
      container={() => document.getElementById('root') ?? document.body}
      sx={{ '& .MuiDialog-paper': { borderRadius: 2, m: 2, maxHeight: 'calc(100% - 32px)' } }}
    >
      <DialogTitle sx={{ pb: 1, fontSize: 18, fontWeight: 600, lineHeight: 1.35 }}>
        {t(`${K}.dialogTitle`)}
      </DialogTitle>
      <DialogContent>
        <Typography variant="body2" color="text.secondary" sx={{ mb: 2 }}>
          {t(`${K}.intro`)}
        </Typography>

        <Box component="ol" sx={{ pl: 2.5, m: 0, '& li': { mb: 1.5 } }}>
          <li>
            <Typography variant="body2">{t(`${K}.step1`)}</Typography>
          </li>
          <li>
            <Typography variant="body2">{t(`${K}.step2`)}</Typography>
            <Box
              data-testid="background-blocked-item-name"
              sx={{
                mt: 1,
                px: 1.5,
                py: 1,
                borderRadius: 1,
                bgcolor: 'action.hover',
                fontWeight: 600,
                fontSize: 14,
                wordBreak: 'break-word',
              }}
            >
              {itemName}
            </Box>
            <Typography variant="caption" color="text.secondary">
              {t(`${K}.itemNameNote`)}
            </Typography>
          </li>
          <li>
            <Typography variant="body2">{t(`${K}.step3`)}</Typography>
          </li>
        </Box>

        {stalled ? (
          <Typography role="status" variant="body2" color="warning.main" sx={{ mt: 1 }}>
            {t(`${K}.statusStalled`)}
          </Typography>
        ) : (
          <Box
            role="status"
            sx={{ display: 'flex', alignItems: 'center', gap: 1, mt: 1, color: switchOn ? 'success.main' : 'text.secondary' }}
          >
            <CircularProgress size={14} color={switchOn ? 'success' : 'inherit'} />
            <Typography variant="body2">{t(switchOn ? `${K}.statusRecovering` : `${K}.statusWaiting`)}</Typography>
          </Box>
        )}
        <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 1 }}>
          {t(`${K}.noReinstall`)}
        </Typography>
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2 }}>
        <Button onClick={onClose} sx={{ whiteSpace: 'nowrap' }}>
          {t(`${K}.later`)}
        </Button>
        {stalled ? (
          <Button
            variant="contained"
            onClick={onReinstall}
            disabled={reinstalling}
            sx={{ whiteSpace: 'nowrap' }}
          >
            {t(`${K}.reinstall`)}
          </Button>
        ) : (
          canOpenSettings && (
            <Button variant="contained" onClick={onOpenSettings} sx={{ whiteSpace: 'nowrap' }}>
              {t(`${K}.openSettings`)}
            </Button>
          )
        )}
      </DialogActions>
    </Dialog>
  );
}
