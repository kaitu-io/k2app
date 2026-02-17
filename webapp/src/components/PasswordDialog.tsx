import { useState } from "react";
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

interface PasswordDialogProps {
  open: boolean;
  onClose: () => void;
  onSuccess: () => void;
}

export default function PasswordDialog({ open, onClose, onSuccess }: PasswordDialogProps) {
  const { t } = useTranslation();
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [error, setError] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);

  const handleClose = () => {
    setPassword("");
    setConfirmPassword("");
    setError("");
    onClose();
  };

  const handleSubmit = async () => {
    setError("");

    if (password.length < 8) {
      setError(t("account:password.tooShort"));
      return;
    }
    if (!/[a-zA-Z]/.test(password)) {
      setError(t("account:password.needsLetter"));
      return;
    }
    if (!/[0-9]/.test(password)) {
      setError(t("account:password.needsNumber"));
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

  return (
    <Dialog open={open} onClose={handleClose} maxWidth="xs" fullWidth>
      <DialogTitle>{t("account:password.setPassword")}</DialogTitle>
      <DialogContent>
        {error && <Alert severity="error" sx={{ mb: 2 }}>{error}</Alert>}
        <TextField
          fullWidth
          type="password"
          label={t("account:password.newPassword")}
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          margin="normal"
          helperText={t("account:password.requirements")}
        />
        <TextField
          fullWidth
          type="password"
          label={t("account:password.confirmPassword")}
          value={confirmPassword}
          onChange={(e) => setConfirmPassword(e.target.value)}
          margin="normal"
        />
      </DialogContent>
      <DialogActions>
        <Button onClick={handleClose}>{t("common:common.cancel")}</Button>
        <Button
          variant="contained"
          onClick={handleSubmit}
          disabled={isSubmitting || !password || !confirmPassword}
          startIcon={isSubmitting ? <CircularProgress size={16} /> : null}
        >
          {t("common:common.confirm")}
        </Button>
      </DialogActions>
    </Dialog>
  );
}
