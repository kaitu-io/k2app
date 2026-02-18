/**
 * LoginDialog - Login Dialog Component
 *
 * Email verification code login flow with MUI standard design patterns.
 */

import { useState, useEffect, useRef } from "react";
import { useNavigate } from "react-router-dom";
import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Box,
  Typography,
  TextField,
  Button,
  Alert,
  InputAdornment,
  Stack,
  CircularProgress,
  IconButton,
  Link,
  Divider,
} from "@mui/material";
import {
  Close as CloseIcon,
  Email as EmailIcon,
  VpnKey as VpnKeyIcon,
  CardGiftcard as InviteIcon,
} from "@mui/icons-material";
import { useTranslation } from "react-i18next";
import { useAuthStore } from "../stores";

import { useLoginDialogStore } from "../stores/login-dialog.store";
import { useAppLinks } from "../hooks/useAppLinks";
import { handleResponseError } from "../utils/errorCode";
import { cloudApi } from '../services/cloud-api';
import { cacheStore } from '../services/cache-store';
import type { AuthResult } from '../services/api-types';
import { delayedFocus } from '../utils/ui';

export default function LoginDialog() {
  const { t, i18n } = useTranslation();
  const navigate = useNavigate();
  const { links } = useAppLinks();

  // Dialog state from store
  const { isOpen, message, close } = useLoginDialogStore();
  const setIsAuthenticated = useAuthStore((s) => s.setIsAuthenticated);

  // Form state
  const [email, setEmail] = useState("");
  const [verificationCode, setVerificationCode] = useState("");
  const [inviteCode, setInviteCode] = useState("");

  // UI state
  const [step, setStep] = useState<"email" | "code">("email");
  const [countdown, setCountdown] = useState(0);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState("");

  // Refs for delayed focus (avoid autoFocus timing issues on old WebViews)
  const emailInputRef = useRef<HTMLInputElement>(null);
  const codeInputRef = useRef<HTMLInputElement>(null);

  // Email validation
  const emailRegex = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
  const isEmailValid = email.trim() !== "" && emailRegex.test(email);

  // User status (from backend)
  const [isActivated, setIsActivated] = useState(true);

  // Reset form when dialog closes
  useEffect(() => {
    if (!isOpen) {
      setEmail("");
      setVerificationCode("");
      setInviteCode("");
      setStep("email");
      setCountdown(0);
      setError("");
      setIsActivated(true);
    }
  }, [isOpen]);

  // Delayed focus management - avoids autoFocus timing issues on old WebViews
  useEffect(() => {
    if (!isOpen) return;

    // Delay focus to allow Dialog animation to complete
    const cancel = delayedFocus(
      () => (step === "email" ? emailInputRef.current : codeInputRef.current),
      150
    );
    return cancel;
  }, [isOpen, step]);

  // Countdown logic
  useEffect(() => {
    if (countdown > 0) {
      const timer = setTimeout(() => setCountdown(countdown - 1), 1000);
      return () => clearTimeout(timer);
    }
  }, [countdown]);

  // Step 1: Send verification code
  const handleSendCode = async () => {
    if (!isEmailValid) {
      setError(t("auth:auth.invalidEmailFormat"));
      return;
    }

    try {
      setIsSubmitting(true);
      setError("");

      const response = await cloudApi.post<{
        userExists: boolean;
        isActivated: boolean;
        isFirstOrderDone: boolean;
      }>('/api/auth/code', { email, language: i18n.language });

      handleResponseError(
        response.code,
        response.message,
        t,
        t("auth:auth.sendCodeFailed")
      );

      if (response.data) {
        setIsActivated(response.data.isActivated);
        setStep("code");
        setCountdown(60);
      }
    } catch (err) {
      console.error('[LoginDialog] Failed to send verification code:', err);
      setError(t("auth:auth.sendCodeFailedRetry"));
    } finally {
      setIsSubmitting(false);
    }
  };

  // Step 2: Verify code and login
  const handleVerifyCode = async () => {
    if (!verificationCode.trim()) {
      setError(t("auth:auth.pleaseEnterCode"));
      return;
    }

    try {
      setIsSubmitting(true);
      setError("");

      const udid = await window._platform!.getUdid();
      const response = await cloudApi.post<AuthResult>('/api/auth/login', {
        email,
        verificationCode: verificationCode,
        udid,
        remark: t("startup:startup.newDevice"),
        inviteCode: inviteCode.trim() || undefined,
        language: i18n.language,
      });

      handleResponseError(
        response.code,
        response.message,
        t,
        t("auth:auth.loginFailed")
      );

      // Tokens are automatically saved by cloudApi for auth paths
      // Clear all cache to ensure fresh data after login
      cacheStore.clear();
      setIsAuthenticated(true);
      close();
    } catch (err) {
      console.error('[LoginDialog] Failed to verify code:', err);
      setError(t("auth:auth.loginFailedRetry"));
    } finally {
      setIsSubmitting(false);
    }
  };

  // Go back to previous step
  const handleBack = () => {
    setError("");
    if (step === "code") {
      setStep("email");
      setVerificationCode("");
      setInviteCode("");
    }
  };

  return (
    <Dialog
      open={isOpen}
      onClose={close}
      maxWidth="xs"
      fullWidth
      PaperProps={{
        sx: {
          borderRadius: 3,
          m: 2,
        },
      }}
    >
      {/* Header with Title and Close Button */}
      <DialogTitle sx={{
        pb: 1,
        pr: 6, // Space for close button
        display: 'flex',
        alignItems: 'center',
        gap: 1.5,
      }}>
        <Box
          component="img"
          src="/icon-192x192.png"
          alt="Kaitu"
          sx={{
            width: 40,
            height: 40,
            borderRadius: 2,
          }}
        />
        <Box>
          <Typography variant="h6" component="span" fontWeight={600}>
            {t("auth:auth.login", "Login")}
          </Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mt: -0.25 }}>
            Kaitu.io
          </Typography>
        </Box>
      </DialogTitle>

      <IconButton
        onClick={close}
        sx={{
          position: "absolute",
          top: 12,
          right: 12,
          color: "text.secondary",
        }}
        size="small"
      >
        <CloseIcon fontSize="small" />
      </IconButton>

      <DialogContent sx={{ pt: 1 }}>
        {/* Optional message */}
        {message && (
          <Alert severity="info" sx={{ mb: 2 }}>
            {message}
          </Alert>
        )}

        {/* Error Alert */}
        {error && (
          <Alert
            severity="error"
            onClose={() => setError("")}
            sx={{ mb: 2 }}
          >
            {error}
          </Alert>
        )}

        {/* Step 1: Email Input */}
        {step === "email" && (
          <Stack spacing={2}>
            <TextField
              fullWidth
              label={t("auth:auth.email")}
              placeholder={t("auth:auth.emailPlaceholder")}
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              onBlur={(e) => setEmail(e.target.value.trim())}
              onKeyDown={(e) => {
                if (e.key === "Enter" && !isSubmitting && isEmailValid) {
                  handleSendCode();
                }
              }}
              disabled={isSubmitting}
              inputRef={emailInputRef}
              type="email"
              inputProps={{
                autoCapitalize: "none",
                autoCorrect: "off",
                autoComplete: "email",
                spellCheck: false,
              }}
              InputProps={{
                startAdornment: (
                  <InputAdornment position="start">
                    <EmailIcon color="action" />
                  </InputAdornment>
                ),
              }}
            />

            <Button
              fullWidth
              variant="contained"
              size="large"
              onClick={handleSendCode}
              disabled={!isEmailValid || isSubmitting}
              startIcon={
                isSubmitting ? (
                  <CircularProgress size={20} color="inherit" />
                ) : null
              }
            >
              {t("auth:auth.sendCode")}
            </Button>

            <Divider sx={{ my: 1 }}>
              <Typography variant="caption" color="text.secondary">
                {t("common:common.or", "OR")}
              </Typography>
            </Divider>

            <Button
              fullWidth
              variant="outlined"
              onClick={() => {
                close();
                navigate("/purchase");
              }}
            >
              {t("auth:auth.activateService")}
            </Button>
          </Stack>
        )}

        {/* Step 2: Verification Code Input */}
        {step === "code" && (
          <Stack spacing={2}>
            {!isActivated && (
              <Alert severity="info">
                {t("auth:auth.inviteCodeOptional")}
              </Alert>
            )}

            <Box sx={{
              bgcolor: 'action.hover',
              borderRadius: 2,
              p: 1.5,
              mb: 1,
            }}>
              <Typography variant="body2" color="text.secondary">
                {t("auth:auth.codeSentTo", { email })}
              </Typography>
              <Typography variant="caption" color="text.secondary">
                {t("auth:auth.checkSpamFolder")}
              </Typography>
            </Box>

            <Box sx={{ display: "flex", gap: 1 }}>
              <TextField
                fullWidth
                label={t("auth:auth.verificationCode")}
                placeholder={t("auth:auth.codePlaceholder")}
                value={verificationCode}
                onChange={(e) => setVerificationCode(e.target.value)}
                onBlur={(e) => setVerificationCode(e.target.value.trim())}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && !isSubmitting && verificationCode) {
                    handleVerifyCode();
                  }
                }}
                disabled={isSubmitting}
                inputRef={codeInputRef}
                inputProps={{
                  autoCapitalize: "none",
                  autoCorrect: "off",
                  autoComplete: "one-time-code",
                  spellCheck: false,
                  inputMode: "numeric",
                  pattern: "[0-9]*",
                }}
                InputProps={{
                  startAdornment: (
                    <InputAdornment position="start">
                      <VpnKeyIcon color="action" />
                    </InputAdornment>
                  ),
                }}
              />

              <Button
                variant="outlined"
                onClick={handleSendCode}
                disabled={countdown > 0 || isSubmitting}
                sx={{
                  minWidth: 80,
                  flexShrink: 0,
                }}
              >
                {countdown > 0 ? `${countdown}s` : t("auth:auth.resend")}
              </Button>
            </Box>

            {!isActivated && (
              <TextField
                fullWidth
                label={t("auth:auth.inviteCode")}
                placeholder={t("auth:auth.inviteCodePlaceholder")}
                value={inviteCode}
                onChange={(e) => setInviteCode(e.target.value)}
                onBlur={(e) => setInviteCode(e.target.value.trim().toUpperCase())}
                disabled={isSubmitting}
                inputProps={{
                  maxLength: 8,
                  style: { textTransform: "uppercase", letterSpacing: "0.1em" },
                  autoCapitalize: "characters",
                  autoCorrect: "off",
                  spellCheck: false,
                }}
                InputProps={{
                  startAdornment: (
                    <InputAdornment position="start">
                      <InviteIcon color="action" />
                    </InputAdornment>
                  ),
                }}
              />
            )}

            <Button
              fullWidth
              variant="contained"
              size="large"
              onClick={handleVerifyCode}
              disabled={!verificationCode || isSubmitting}
              startIcon={
                isSubmitting ? (
                  <CircularProgress size={20} color="inherit" />
                ) : null
              }
            >
              {t("auth:auth.verify")}
            </Button>
          </Stack>
        )}
      </DialogContent>

      <DialogActions sx={{
        px: 3,
        pb: 2,
        pt: 0,
        flexDirection: 'column',
        gap: 1,
      }}>
        {step === "email" ? (
          <Button
            fullWidth
            variant="text"
            onClick={close}
            color="inherit"
          >
            {t("common:common.later", "Later")}
          </Button>
        ) : (
          <Button
            fullWidth
            variant="text"
            onClick={handleBack}
            disabled={isSubmitting}
            color="inherit"
          >
            {t("common:common.back")}
          </Button>
        )}

        {/* Terms Notice */}
        <Typography
          variant="caption"
          color="text.secondary"
          align="center"
          sx={{ opacity: 0.8 }}
        >
          {t("auth:auth.loginAgreement", "By logging in, you agree to our")}{" "}
          <Link
            component="button"
            variant="caption"
            onClick={() => window._platform!.openExternal?.(links.termsOfServiceUrl)}
            sx={{ cursor: "pointer" }}
          >
            {t("auth:auth.termsOfService", "Terms of Service")}
          </Link>
        </Typography>
      </DialogActions>
    </Dialog>
  );
}
