import { useEffect, useState } from 'react';
import {
  Alert,
  Box,
  Button,
  Checkbox,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  FormControlLabel,
  Stack,
  TextField,
  Typography,
} from '@mui/material';
import { alpha, keyframes, type Theme } from '@mui/material/styles';
import WarningAmberRoundedIcon from '@mui/icons-material/WarningAmberRounded';
import { useTranslation } from 'react-i18next';
import type { DataUser } from '../services/api-types';
import { formatDate } from '../utils/time';
import { brandConfig } from '../brands';

// 注销是这个 app 里唯一无法回滚的用户操作：登录标识与设备是硬删除，付费
// 时长随账号一起作废。所以这里故意做得「难」——三步、每一步都要一个新的
// 主动动作（点继续 → 逐条勾选 → 手输确认词），默认/醒目按钮永远是「保留
// 账号」，而不是一个点两下就没了的确认框。
type Step = 1 | 2 | 3;

const pulse = keyframes`
  0%, 100% { border-color: var(--danger); box-shadow: 0 0 0 0 var(--danger-glow); }
  50% { border-color: transparent; box-shadow: 0 0 22px 4px var(--danger-glow); }
`;

interface DeleteAccountDialogProps {
  open: boolean;
  user: DataUser | null | undefined;
  loading: boolean;
  error: string | null;
  onClose: () => void;
  onConfirm: () => void;
}

export default function DeleteAccountDialog({
  open,
  user,
  loading,
  error,
  onClose,
  onConfirm,
}: DeleteAccountDialogProps) {
  const { t } = useTranslation();
  const [step, setStep] = useState<Step>(1);
  const [acks, setAcks] = useState<Record<string, boolean>>({});
  const [typed, setTyped] = useState('');

  // 每次打开都从第一步重新走，不继承上一次勾过的确认。
  useEffect(() => {
    if (open) {
      setStep(1);
      setAcks({});
      setTyped('');
    }
  }, [open]);

  const nowSec = Date.now() / 1000;
  const expiredAt = user?.expiredAt ?? 0;
  const hasMembership = expiredAt > nowSec;
  const daysLeft = hasMembership ? Math.ceil((expiredAt - nowSec) / 86400) : 0;
  const hasSubscription = (user?.subscriptions?.length ?? 0) > 0;
  const deviceCount = user?.deviceCount ?? 0;

  const ackKeys = [
    ...(hasMembership ? ['deleteAckMembership'] : []),
    ...(hasSubscription ? ['deleteAckSubscription'] : []),
    'deleteAckIrreversible',
  ];
  const allAcked = ackKeys.every((k) => acks[k]);

  const confirmWord = t('account:account.deleteConfirmWord');
  const typedOk = typed.trim().toLowerCase() === confirmWord.toLowerCase();

  const handleClose = () => {
    if (!loading) onClose();
  };

  return (
    <Dialog
      open={open}
      onClose={handleClose}
      fullWidth
      maxWidth="xs"
      PaperProps={{
        'data-testid': 'delete-account-dialog',
        sx: (theme: Theme) => ({
          '--danger': theme.palette.error.main,
          '--danger-glow': alpha(theme.palette.error.main, 0.55),
          border: '3px solid var(--danger)',
          animation: `${pulse} 1s ease-in-out infinite`,
          '@media (prefers-reduced-motion: reduce)': { animation: 'none' },
        }),
      } as object}
    >
      <DialogTitle sx={{ color: 'error.main', display: 'flex', alignItems: 'center', gap: 1, pb: 0.5 }}>
        <WarningAmberRoundedIcon />
        {t('account:account.deleteAccountTitle')}
      </DialogTitle>
      <DialogContent>
        <Typography variant="caption" color="text.secondary">
          {t('account:account.deleteStep', { step })}
        </Typography>

        {step === 1 && (
          <>
            <Typography sx={{ mt: 1, fontWeight: 600 }}>
              {t('account:account.deleteAccountWarning')}
            </Typography>
            <Stack
              spacing={1}
              sx={(theme) => ({
                mt: 1.5,
                p: 1.5,
                borderRadius: 1,
                border: `1px solid ${theme.palette.error.main}`,
                bgcolor: alpha(theme.palette.error.main, 0.12),
              })}
            >
              {hasMembership && (
                <Typography
                  data-testid="delete-lose-membership"
                  sx={{ color: 'error.main', fontWeight: 800, fontSize: '1.05rem' }}
                >
                  {t('account:account.deleteLoseMembership', {
                    date: formatDate(expiredAt),
                    days: daysLeft,
                  })}
                </Typography>
              )}
              {hasSubscription && (
                <Typography sx={{ color: 'error.main', fontWeight: 700 }}>
                  {t('account:account.deleteLoseSubscription')}
                </Typography>
              )}
              {deviceCount > 0 && (
                <Typography variant="body2">
                  {t('account:account.deleteLoseDevices', { n: deviceCount })}
                </Typography>
              )}
              {/* 钱包是开途专属（features.wallet）；没有钱包的品牌不提"会失去钱包余额"。 */}
              {brandConfig.features.wallet && (
                <Typography variant="body2">{t('account:account.deleteLoseWallet')}</Typography>
              )}
              <Typography variant="body2">{t('account:account.deleteLoseData')}</Typography>
            </Stack>
          </>
        )}

        {step === 2 && (
          <>
            <Typography sx={{ mt: 1, fontWeight: 600 }}>
              {t('account:account.deleteAckIntro')}
            </Typography>
            <Stack sx={{ mt: 1 }}>
              {ackKeys.map((k) => (
                <FormControlLabel
                  key={k}
                  sx={{ alignItems: 'flex-start', mt: 0.5 }}
                  control={
                    <Checkbox
                      color="error"
                      checked={!!acks[k]}
                      onChange={(e) => setAcks((prev) => ({ ...prev, [k]: e.target.checked }))}
                      sx={{ pt: 0.25 }}
                    />
                  }
                  label={
                    <Typography variant="body2">
                      {t(`account:account.${k}`, { date: formatDate(expiredAt), days: daysLeft })}
                    </Typography>
                  }
                />
              ))}
            </Stack>
          </>
        )}

        {step === 3 && (
          <>
            <Typography sx={{ mt: 1, fontWeight: 800, color: 'error.main' }}>
              {t('account:account.deleteFinalWarning')}
            </Typography>
            {hasMembership && (
              <Typography sx={{ mt: 1, color: 'error.main', fontWeight: 700 }}>
                {t('account:account.deleteLoseMembership', {
                  date: formatDate(expiredAt),
                  days: daysLeft,
                })}
              </Typography>
            )}
            <Typography variant="body2" sx={{ mt: 1.5 }}>
              {t('account:account.deleteTypePrompt', { word: confirmWord })}
            </Typography>
            <TextField
              fullWidth
              size="small"
              color="error"
              autoComplete="off"
              value={typed}
              onChange={(e) => setTyped(e.target.value)}
              placeholder={confirmWord}
              disabled={loading}
              inputProps={{ 'aria-label': confirmWord, autoCapitalize: 'off', autoCorrect: 'off' }}
              sx={{ mt: 1 }}
            />
          </>
        )}

        {error && (
          <Alert severity="error" sx={{ mt: 2 }}>
            {error}
          </Alert>
        )}
      </DialogContent>
      <DialogActions sx={{ px: 3, pb: 2, flexDirection: 'column', alignItems: 'stretch', gap: 1 }}>
        <Button variant="contained" color="primary" onClick={handleClose} disabled={loading}>
          {t('account:account.deleteKeep')}
        </Button>
        <Box sx={{ display: 'flex', justifyContent: 'center' }}>
          {step === 1 && (
            <Button color="error" size="small" onClick={() => setStep(2)} sx={{ ml: '0 !important' }}>
              {t('account:account.deleteContinue')}
            </Button>
          )}
          {step === 2 && (
            <Button color="error" size="small" disabled={!allAcked} onClick={() => setStep(3)}>
              {t('account:account.deleteContinue')}
            </Button>
          )}
          {step === 3 && (
            <Button color="error" size="small" disabled={!typedOk || loading} onClick={onConfirm}>
              {loading ? <CircularProgress size={20} /> : t('account:account.deleteFinalButton')}
            </Button>
          )}
        </Box>
      </DialogActions>
    </Dialog>
  );
}
