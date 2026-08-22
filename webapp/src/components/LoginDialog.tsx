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
  Tabs,
  Tab,
  alpha,
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
import { ERROR_CODES, handleResponseError } from "../utils/errorCode";
import { suggestEmail } from "../utils/email-suggest";
import EmailSuggestion from "./EmailSuggestion";
import PasswordAuthFields from "./PasswordAuthFields";
import { cloudApi } from '../services/cloud-api';
import { getDeviceUdid } from '../services/device-udid';
import { cacheStore } from '../services/cache-store';
import type { AuthResult } from '../services/api-types';
import { delayedFocus } from '../utils/ui';
import { useSubscriptionAffordance } from '../hooks/useSubscriptionAffordance';
import { brandConfig } from '../brands';

/**
 * DialogContent is a flex column, so a form taller than the viewport squashes its
 * children instead of scrolling — at 320×568 the "Activate Service" button collapsed
 * into a 0-height outline. Pinning shrink makes the content overflow and scroll.
 */
const FORM_STACK_SX = { '& > *': { flexShrink: 0 } } as const;

/**
 * Narrow-screen input tuning: the leading icon plus its default 8px gutter ate ~46px
 * of an already cramped field. A smaller icon and tighter gutter give it back.
 */
const COMPACT_FIELD_SX = {
  '& .MuiInputAdornment-positionStart': { mr: 1 },
  '& .MuiOutlinedInput-input': { minWidth: 0 },
} as const;

export default function LoginDialog() {
  const { t, i18n } = useTranslation();
  const navigate = useNavigate();
  const { links } = useAppLinks();
  const affordance = useSubscriptionAffordance();

  // Dialog state from store
  const { isOpen, message, trigger, close } = useLoginDialogStore();
  const setIsAuthenticated = useAuthStore((s) => s.setIsAuthenticated);
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated);

  // Form state
  const [email, setEmail] = useState("");
  const [verificationCode, setVerificationCode] = useState("");
  const [inviteCode, setInviteCode] = useState("");

  // Email typo suggestion
  const [emailSuggestion, setEmailSuggestion] = useState<string | null>(null);

  // UI state
  const [step, setStep] = useState<"email" | "code">("email");
  const [countdown, setCountdown] = useState(0);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState("");
  const [loginMethod, setLoginMethod] = useState<"code" | "password">("code");
  const [password, setPassword] = useState("");

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
      setEmailSuggestion(null);
      setLoginMethod("code");
      setPassword("");
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

      const udid = await getDeviceUdid();
      const response = await cloudApi.post<{
        userExists: boolean;
        isActivated: boolean;
        isFirstOrderDone: boolean;
      }>('/api/auth/code', { email, language: i18n.language, udid });

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

      const udid = await getDeviceUdid();
      const response = await cloudApi.post<AuthResult>('/api/auth/login', {
        email,
        verificationCode: verificationCode,
        udid,
        remark: t("startup:startup.newDevice"),
        inviteCode: inviteCode.trim() || undefined,
        language: i18n.language,
      });

      // Surface VERIFICATION_CODE_EXPIRED (400013) before throwing so we can
      // clear the input + send the user back to step 1 where the resend
      // button lives.
      if (response.code === ERROR_CODES.VERIFICATION_CODE_EXPIRED) {
        setVerificationCode("");
        setStep("email");
        setCountdown(0);
      }

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
      // Surface the precise i18n message (400003 vs 400013) instead of a
      // generic fallback. The previous setError(loginFailedRetry) swallowed
      // the distinction users need to see.
      setError(err instanceof Error ? err.message : t("auth:auth.loginFailedRetry"));
    } finally {
      setIsSubmitting(false);
    }
  };

  // Password login handler — mirrors EmailLoginForm.handlePasswordLogin.
  // Shares the password-tab UX so both entry points stay in lock-step.
  const handlePasswordLogin = async () => {
    if (!isEmailValid || !password) {
      setError(t("auth:auth.pleaseEnterPassword"));
      return;
    }
    try {
      setIsSubmitting(true);
      setError("");
      const deviceRemark = t("startup:startup.newDevice");
      const udid = await getDeviceUdid();
      const response = await cloudApi.post<AuthResult>('/api/auth/login/password', {
        email,
        password,
        udid,
        remark: deviceRemark,
        deviceName: deviceRemark,
        platform: window._platform?.os || '',
        language: i18n.language,
      });
      handleResponseError(response.code, response.message, t, t("auth:auth.loginFailed"));
      cacheStore.clear();
      setIsAuthenticated(true);
      close();
    } catch (err) {
      console.error('[LoginDialog] Password login failed:', err);
      setError(err instanceof Error ? err.message : t("auth:auth.loginFailedRetry"));
    } finally {
      setIsSubmitting(false);
    }
  };

  // Close dialog — redirect to Dashboard if opened by route guard and user is not authenticated
  const handleClose = (_event?: unknown, reason?: string) => {
    console.warn('[LoginDialog] onClose triggered, reason:', reason);
    // Prevent accidental close via backdrop tap on mobile. CSS zoom on body causes
    // touch event target resolution to be misaligned — taps inside the dialog visually
    // land on the backdrop in CSS coordinates, triggering spurious backdropClick events.
    // Users can still close via the X button or the Later/Back button.
    if (reason === 'backdropClick') return;
    const shouldRedirect = trigger.startsWith('guard:') && !isAuthenticated;
    close();
    if (shouldRedirect) {
      navigate('/');
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
      onClose={handleClose}
      disableEscapeKeyDown
      maxWidth="xs"
      fullWidth
      // Lift the dialog off the page: darker + blurred scrim, brand-tinted hairline,
      // and a raised surface. The app renders dark-only, where background (#0a0a0f)
      // and paper (#111118) are nearly the same value — without this the panel edge
      // is invisible against the page behind it.
      // The dialog is portalled to <body>, so it never inherits Layout's
      // safe-area padding — same gap the theme already patches for Snackbar.
      // Inset the container instead of the paper so centering stays correct.
      sx={{
        '& .MuiDialog-container': {
          pt: 'env(safe-area-inset-top, 0px)',
          pb: 'env(safe-area-inset-bottom, 0px)',
        },
      }}
      slotProps={{
        backdrop: {
          sx: {
            backgroundColor: alpha('#000', 0.72),
            backdropFilter: 'blur(4px)',
            WebkitBackdropFilter: 'blur(4px)',
          },
        },
      }}
      PaperProps={{
        sx: (theme) => ({
          borderRadius: 3,
          // MUI's `fullWidth` hardcodes `width: calc(100% - 64px)` regardless of the
          // margin we set, so on a 375px phone the panel gave up 32px of content width
          // for nothing. Pin width to the actual margin. The dialog is portalled to
          // <body>, outside the #root CSS `zoom`, so these are real device pixels.
          m: { xs: 1.25, sm: 2 },
          width: { xs: 'calc(100% - 20px)', sm: 'calc(100% - 32px)' },
          maxWidth: { xs: 'calc(100% - 20px)', sm: 444 },
          maxHeight: { xs: 'calc(100% - 20px)', sm: 'calc(100% - 64px)' },
          border: `1px solid ${alpha(theme.palette.primary.main, 0.22)}`,
          // Raise the surface above background.paper (MuiPaper clears backgroundImage
          // globally; a flat overlay re-adds the elevation tint without a hardcoded hex).
          backgroundImage: `linear-gradient(${alpha('#fff', 0.05)}, ${alpha('#fff', 0.05)})`,
          boxShadow: `0 24px 64px -12px ${alpha('#000', 0.9)}, 0 0 40px -16px ${alpha(theme.palette.primary.main, 0.2)}`,
        }),
      }}
    >
      {/* Header — close button lives inside the flex row instead of absolute+pr:6,
          which reclaims 48px of title width on narrow screens. */}
      <DialogTitle sx={{
        px: { xs: 2, sm: 3 },
        pt: { xs: 2, sm: 2.5 },
        pb: { xs: 1.5, sm: 2 },
        display: 'flex',
        alignItems: 'center',
        gap: 1.5,
        borderBottom: (theme) => `1px solid ${theme.palette.divider}`,
      }}>
        <Box
          component="img"
          src="/icon-192x192.png"
          alt={brandConfig.productName}
          sx={{
            width: { xs: 36, sm: 40 },
            height: { xs: 36, sm: 40 },
            borderRadius: 2,
            flexShrink: 0,
          }}
        />
        <Box sx={{ minWidth: 0, flex: 1 }}>
          <Typography variant="h6" component="span" fontWeight={600} noWrap display="block">
            {t("auth:auth.login", "Login")}
          </Typography>
          <Typography variant="body2" color="text.secondary" sx={{ mt: -0.25 }} noWrap>
            {brandConfig.domainLabel}
          </Typography>
        </Box>

        <IconButton
          onClick={handleClose}
          sx={{ color: "text.secondary", flexShrink: 0, mr: -0.5 }}
          size="small"
          aria-label={t("common:common.close", "Close")}
        >
          <CloseIcon fontSize="small" />
        </IconButton>
      </DialogTitle>

      <DialogContent sx={{ px: { xs: 2, sm: 3 }, pt: 2, pb: 1 }}>
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

        {/* Login Method Tabs - only show on email step */}
        {step === "email" && (
          <Tabs
            value={loginMethod}
            onChange={(_, v) => { setLoginMethod(v); setError(""); }}
            centered
            sx={{ mb: 2 }}
          >
            <Tab value="code" label={t("auth:auth.codeLogin")} />
            <Tab value="password" label={t("auth:auth.passwordLogin")} />
          </Tabs>
        )}

        {/* Step 1: Email Input (Code Login) */}
        {step === "email" && loginMethod === "code" && (
          <Stack spacing={2} sx={FORM_STACK_SX}>
            <TextField
              fullWidth
              label={t("auth:auth.email")}
              placeholder={t("auth:auth.emailPlaceholder")}
              value={email}
              onChange={(e) => {
                setEmail(e.target.value);
                if (emailSuggestion) setEmailSuggestion(null);
              }}
              onBlur={(e) => {
                const cleaned = e.target.value.trim().toLowerCase().replace(/\s+/g, '');
                setEmail(cleaned);
                const suggested = suggestEmail(cleaned);
                setEmailSuggestion(suggested);
              }}
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
                    <EmailIcon color="action" fontSize="small" />
                  </InputAdornment>
                ),
              }}
              sx={COMPACT_FIELD_SX}
            />

            {emailSuggestion && (
              <EmailSuggestion
                suggestion={emailSuggestion}
                onAccept={() => { setEmail(emailSuggestion); setEmailSuggestion(null); }}
              />
            )}

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

            {(window._platform?.os !== 'ios' || affordance.mode === 'subscribe') && (
              <>
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
              </>
            )}
          </Stack>
        )}

        {/* Step 1: Password Login */}
        {step === "email" && loginMethod === "password" && (
          <Stack spacing={2} sx={FORM_STACK_SX}>
            <PasswordAuthFields
              email={email}
              password={password}
              onEmailChange={(v) => { setEmail(v); if (emailSuggestion) setEmailSuggestion(null); }}
              onPasswordChange={setPassword}
              onSubmit={handlePasswordLogin}
              onEmailBlur={() => {
                const cleaned = email.trim().toLowerCase().replace(/\s+/g, '');
                setEmail(cleaned);
                setEmailSuggestion(suggestEmail(cleaned));
              }}
              emailSuggestion={emailSuggestion}
              onAcceptSuggestion={() => { setEmail(emailSuggestion!); setEmailSuggestion(null); }}
              isSubmitting={isSubmitting}
            />
          </Stack>
        )}

        {/* Step 2: Verification Code Input */}
        {step === "code" && (
          <Stack spacing={2} sx={FORM_STACK_SX}>
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

            {/* Resend lives inside the field rather than beside it — a separate
                80px button plus its gap cost the code input a quarter of its
                width on a 320px screen. */}
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
                    <VpnKeyIcon color="action" fontSize="small" />
                  </InputAdornment>
                ),
                endAdornment: (
                  <InputAdornment position="end">
                    <Button
                      size="small"
                      onClick={handleSendCode}
                      disabled={countdown > 0 || isSubmitting}
                      sx={{
                        minWidth: 0,
                        px: 1,
                        mr: -0.5,
                        whiteSpace: 'nowrap',
                      }}
                    >
                      {countdown > 0 ? `${countdown}s` : t("auth:auth.resend")}
                    </Button>
                  </InputAdornment>
                ),
              }}
              sx={COMPACT_FIELD_SX}
            />

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
                  // 字距只为已输入的码服务；占位符继承它会被拉成「请 输 入 邀 请 码」
                  style: { textTransform: "uppercase", letterSpacing: inviteCode ? "0.1em" : "normal" },
                  autoCapitalize: "characters",
                  autoCorrect: "off",
                  spellCheck: false,
                }}
                InputProps={{
                  startAdornment: (
                    <InputAdornment position="start">
                      <InviteIcon color="action" fontSize="small" />
                    </InputAdornment>
                  ),
                }}
                sx={COMPACT_FIELD_SX}
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
        px: { xs: 2, sm: 3 },
        pb: 2,
        pt: 0,
        flexDirection: 'column',
        gap: 1,
      }}>
        {step === "email" ? (
          <Button
            fullWidth
            variant="text"
            onClick={handleClose}
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
