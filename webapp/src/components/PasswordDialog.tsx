import { useState, useEffect } from "react";
import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  TextField,
  Button,
  Alert,
  CircularProgress,
} from "@mui/material";
import { useTranslation } from "react-i18next";
import { cloudApi } from '../services/cloud-api';
import { handleResponseError } from "../utils/errorCode";
import { checkPasswordStrength, PASSWORD_MIN_LENGTH } from "../utils/password-strength";
import PasswordStrengthMeter from "./PasswordStrengthMeter";

interface PasswordDialogProps {
  open: boolean;
  onClose: () => void;
  onSuccess: () => void;
  /** Whether the user already has a password — controls copy. Default false. */
  hasPassword?: boolean;
  /** The user's email — fed to zxcvbn so the strength gate penalizes containing-email passwords. */
  userEmail?: string;
}

export default function PasswordDialog({
  open,
  onClose,
  onSuccess,
  hasPassword = false,
  userEmail = "",
}: PasswordDialogProps) {
  const { t } = useTranslation();
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [error, setError] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [strength, setStrength] = useState<{ score: 0 | 1 | 2 | 3 | 4; tooShort: boolean; isValid: boolean }>({
    score: 0,
    tooShort: true,
    isValid: false,
  });

  // Re-compute strength whenever password / userEmail changes. zxcvbn lazy-loads,
  // so the first keystroke is async; subsequent are near-instant. Aborts via the
  // cancelled flag.
  useEffect(() => {
    let cancelled = false;
    if (!password) {
      setStrength({ score: 0, tooShort: true, isValid: false });
      return;
    }
    const userInputs = userEmail ? [userEmail] : [];
    checkPasswordStrength(password, userInputs).then((r) => {
      if (!cancelled) setStrength(r);
    });
    return () => {
      cancelled = true;
    };
  }, [password, userEmail]);

  const handleClose = () => {
    setPassword("");
    setConfirmPassword("");
    setError("");
    setStrength({ score: 0, tooShort: true, isValid: false });
    onClose();
  };

  const canSubmit =
    !!password &&
    !!confirmPassword &&
    strength.isValid &&
    password === confirmPassword &&
    !isSubmitting;

  const handleSubmit = async () => {
    setError("");
    if (strength.tooShort) {
      setError(t("account:password.tooShort", { length: PASSWORD_MIN_LENGTH }));
      return;
    }
    if (!strength.isValid) {
      setError(t("account:password.tooWeak"));
      return;
    }
    if (password !== confirmPassword) {
      setError(t("account:password.mismatch"));
      return;
    }
    try {
      setIsSubmitting(true);
      const response = await cloudApi.post("/api/user/password", { password, confirmPassword });
      handleResponseError(response.code, response.message, t, t("account:password.setFailed"));
      onSuccess();
      handleClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : t("account:password.setFailed"));
    } finally {
      setIsSubmitting(false);
    }
  };

  const title = hasPassword
    ? t("account:password.changePassword")
    : t("account:password.setPassword");

  return (
    <Dialog open={open} onClose={handleClose} maxWidth="xs" fullWidth>
      <DialogTitle>{title}</DialogTitle>
      <DialogContent>
        {error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}
        <TextField
          fullWidth
          type="password"
          label={t("account:password.newPassword")}
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          margin="normal"
          helperText={t("account:password.requirements", { length: PASSWORD_MIN_LENGTH })}
          inputProps={{ autoComplete: 'new-password' }}
        />
        {password && (
          <PasswordStrengthMeter score={strength.score} tooShort={strength.tooShort} />
        )}
        <TextField
          fullWidth
          type="password"
          label={t("account:password.confirmPassword")}
          value={confirmPassword}
          onChange={(e) => setConfirmPassword(e.target.value)}
          margin="normal"
          inputProps={{ autoComplete: 'new-password' }}
        />
      </DialogContent>
      <DialogActions>
        <Button onClick={handleClose}>{t("common:common.cancel")}</Button>
        <Button
          variant="contained"
          onClick={handleSubmit}
          disabled={!canSubmit}
          startIcon={isSubmitting ? <CircularProgress size={16} /> : null}
        >
          {t("common:common.confirm")}
        </Button>
      </DialogActions>
    </Dialog>
  );
}
