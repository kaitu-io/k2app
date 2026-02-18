/**
 * EmailLoginForm - 嵌入式邮箱登录/注册表单
 *
 * 用于 Purchase 页面，未登录时显示此表单进行注册/登录
 * 支持：
 * - 邮箱验证码登录/注册
 * - 未激活用户显示邀请码输入（可选）
 * - 邀请奖励提示（来自 cookie）
 */

import { useState, useEffect, useRef } from "react";
import {
  Box,
  TextField,
  Button,
  Typography,
  InputAdornment,
  Alert,
  CircularProgress,
  Stack,
  Tabs,
  Tab,
} from "@mui/material";
import {
  AlternateEmail as AlternateEmailIcon,
  Lock as LockIcon,
  CardGiftcard as GiftIcon,
} from "@mui/icons-material";
import { useTranslation } from "react-i18next";
import { useAuthStore } from "../stores";
import { handleResponseError } from "../utils/errorCode";
import type { SendCodeResponse, AuthResult } from "../services/api-types";
import { cloudApi } from '../services/cloud-api';
import { cacheStore } from '../services/cache-store';
import { delayedFocus } from '../utils/ui';

// Cookie helper function
function getCookie(name: string): string | null {
  if (typeof document === 'undefined') return null;
  const nameEQ = name + "=";
  const ca = document.cookie.split(';');
  for (let i = 0; i < ca.length; i++) {
    let c = ca[i];
    while (c.charAt(0) === ' ') c = c.substring(1, c.length);
    if (c.indexOf(nameEQ) === 0) return c.substring(nameEQ.length, c.length);
  }
  return null;
}

export interface EmailLoginFormProps {
  onLoginSuccess?: () => void;
}

export default function EmailLoginForm({ onLoginSuccess }: EmailLoginFormProps) {
  const { t, i18n } = useTranslation();
  const setIsAuthenticated = useAuthStore((s) => s.setIsAuthenticated);

  // Form state
  const [email, setEmail] = useState("");
  const [verificationCode, setVerificationCode] = useState("");
  const [inviteCode, setInviteCode] = useState("");
  const [loginMethod, setLoginMethod] = useState<"code" | "password">("code");
  const [password, setPassword] = useState("");

  // UI state
  const [step, setStep] = useState<"email" | "code">("email");
  const [countdown, setCountdown] = useState(0);
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState("");

  // Refs for delayed focus (avoid autoFocus timing issues on old WebViews)
  const emailInputRef = useRef<HTMLInputElement>(null);
  const codeInputRef = useRef<HTMLInputElement>(null);
  const passwordInputRef = useRef<HTMLInputElement>(null);

  // User status (from backend)
  const [isActivated, setIsActivated] = useState(true);

  // Invite code from cookie
  const [inviteCodeFromCookie, setInviteCodeFromCookie] = useState<string | null>(null);

  // Load invite code from cookie
  useEffect(() => {
    const code = getCookie('kaitu_invite_code');
    if (code) {
      setInviteCode(code);
      setInviteCodeFromCookie(code);
    }
  }, []);

  // Email validation
  const emailRegex = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
  const isEmailValid = email.trim() !== "" && emailRegex.test(email);

  // Delayed focus management - avoids autoFocus timing issues on old WebViews
  useEffect(() => {
    const cancel = delayedFocus(
      () => {
        if (step === "code") {
          return codeInputRef.current;
        }
        // step === "email"
        return emailInputRef.current;
      },
      100
    );
    return cancel;
  }, [step, loginMethod]);

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

      const response = await cloudApi.post<SendCodeResponse>('/api/auth/code', {
        email,
        language: i18n.language,
      });

      handleResponseError(
        response.code,
        response.message,
        t,
        t("auth:auth.sendCodeFailed")
      );

      // Success
      if (response.data) {
        setIsActivated(response.data.isActivated);
        setStep("code");
        setCountdown(60);
        console.info(
          `Verification code sent to ${email}, isActivated: ${response.data.isActivated}`
        );
      }
    } catch (err) {
      console.error('[EmailLoginForm] Failed to send verification code:', err);
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

      // Tokens are automatically saved by cloudApi for auth paths
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

      // Tokens are automatically saved by k2api on successful login
      // Clear all cache to ensure fresh data after login
      cacheStore.clear();
      console.info(`Login successful for user: ${email}`);
      setIsAuthenticated(true);
      onLoginSuccess?.();
    } catch (err) {
      console.error('[EmailLoginForm] Failed to verify code:', err);
      setError(t("auth:auth.loginFailedRetry"));
    } finally {
      setIsSubmitting(false);
    }
  };

  // Password login handler
  const handlePasswordLogin = async () => {
    if (!isEmailValid || !password) {
      setError(t("auth:auth.pleaseEnterPassword"));
      return;
    }

    try {
      setIsSubmitting(true);
      setError("");

      const deviceRemark = t("startup:startup.newDevice");
      const udid = await window._platform!.getUdid();
      const response = await cloudApi.post('/api/auth/login/password', {
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
      onLoginSuccess?.();
    } catch (err) {
      console.error('[EmailLoginForm] Password login failed:', err);
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
    }
  };

  return (
    <Box>
      {/* Invite Reward Prompt */}
      {inviteCodeFromCookie && step === "code" && !isActivated && (
        <Alert
          severity="success"
          icon={<GiftIcon />}
          sx={{
            mb: 2,
            borderRadius: 2,
            background: (theme) =>
              theme.palette.mode === 'dark'
                ? 'rgba(76, 175, 80, 0.15)'
                : 'rgba(76, 175, 80, 0.08)',
            border: '1px solid rgba(76, 175, 80, 0.3)',
          }}
        >
          <Typography variant="body2" fontWeight="bold">
            {t('purchase:purchase.inviteRewardTitle')}
          </Typography>
          <Typography variant="caption">
            {t('purchase:purchase.inviteRewardDesc', { days: 3 })}
          </Typography>
        </Alert>
      )}

      {/* Error Alert */}
      {error && (
        <Alert
          severity="error"
          onClose={() => setError("")}
          sx={{ mb: 2, borderRadius: 2 }}
        >
          {error}
        </Alert>
      )}

      {/* Login Method Tabs - only show on email step */}
      {step === "email" && (
        <Tabs
          value={loginMethod}
          onChange={(_, v) => {
            setLoginMethod(v);
            setError("");
          }}
          centered
          sx={{ mb: 2 }}
        >
          <Tab value="code" label={t("auth:auth.codeLogin")} />
          <Tab value="password" label={t("auth:auth.passwordLogin")} />
        </Tabs>
      )}

      <Stack spacing={2}>
        {/* Code Login - Step 1: Email Input */}
        {step === "email" && loginMethod === "code" && (
          <>
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
              inputProps={{
                autoCapitalize: "none",
                autoCorrect: "off",
                autoComplete: "email",
                spellCheck: false,
              }}
              InputProps={{
                startAdornment: (
                  <InputAdornment position="start">
                    <AlternateEmailIcon color="primary" />
                  </InputAdornment>
                ),
              }}
              sx={{
                "& .MuiOutlinedInput-root": {
                  borderRadius: 2,
                },
              }}
            />

            <Button
              fullWidth
              size="large"
              variant="contained"
              onClick={handleSendCode}
              disabled={!isEmailValid || isSubmitting}
              startIcon={
                isSubmitting ? (
                  <CircularProgress size={20} color="inherit" />
                ) : null
              }
              sx={{
                py: 1.5,
                borderRadius: 2,
                textTransform: "none",
                fontSize: "1rem",
                fontWeight: 600,
              }}
            >
              {t("auth:auth.sendCode")}
            </Button>
          </>
        )}

        {/* Password Login - Email + Password fields */}
        {step === "email" && loginMethod === "password" && (
          <>
            <TextField
              fullWidth
              label={t("auth:auth.email")}
              placeholder={t("auth:auth.emailPlaceholder")}
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              onBlur={(e) => setEmail(e.target.value.trim())}
              onKeyDown={(e) => {
                if (e.key === "Enter" && !isSubmitting && isEmailValid && password) {
                  handlePasswordLogin();
                }
              }}
              disabled={isSubmitting}
              inputRef={emailInputRef}
              inputProps={{
                autoCapitalize: "none",
                autoCorrect: "off",
                autoComplete: "email",
                spellCheck: false,
              }}
              InputProps={{
                startAdornment: (
                  <InputAdornment position="start">
                    <AlternateEmailIcon color="primary" />
                  </InputAdornment>
                ),
              }}
              sx={{
                "& .MuiOutlinedInput-root": {
                  borderRadius: 2,
                },
              }}
            />

            <TextField
              fullWidth
              type="password"
              label={t("auth:auth.password")}
              placeholder={t("auth:auth.passwordPlaceholder")}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && !isSubmitting && isEmailValid && password) {
                  handlePasswordLogin();
                }
              }}
              disabled={isSubmitting}
              inputRef={passwordInputRef}
              inputProps={{
                autoCapitalize: "none",
                autoCorrect: "off",
                autoComplete: "current-password",
                spellCheck: false,
              }}
              InputProps={{
                startAdornment: (
                  <InputAdornment position="start">
                    <LockIcon color="primary" />
                  </InputAdornment>
                ),
              }}
              sx={{
                "& .MuiOutlinedInput-root": {
                  borderRadius: 2,
                },
              }}
            />

            <Button
              fullWidth
              size="large"
              variant="contained"
              onClick={handlePasswordLogin}
              disabled={!isEmailValid || !password || isSubmitting}
              startIcon={
                isSubmitting ? (
                  <CircularProgress size={20} color="inherit" />
                ) : null
              }
              sx={{
                py: 1.5,
                borderRadius: 2,
                textTransform: "none",
                fontSize: "1rem",
                fontWeight: 600,
              }}
            >
              {t("auth:auth.login")}
            </Button>
          </>
        )}

        {/* Step 2: Verification Code Input */}
        {step === "code" && (
          <>
            {!isActivated && (
              <Alert
                severity="info"
                sx={{
                  borderRadius: 2,
                }}
              >
                {t("auth:auth.inviteCodeOptional")}
              </Alert>
            )}

            <Box>
              <Typography variant="body2" color="text.secondary" gutterBottom>
                {t("auth:auth.codeSentTo", { email })}
              </Typography>
              <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 0.5 }}>
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
                      <LockIcon color="primary" />
                    </InputAdornment>
                  ),
                }}
                sx={{
                  flex: 1,
                  "& .MuiOutlinedInput-root": {
                    borderRadius: 2,
                  },
                }}
              />

              <Button
                variant="outlined"
                onClick={handleSendCode}
                disabled={countdown > 0 || isSubmitting}
                sx={{
                  minWidth: 80,
                  borderRadius: 2,
                  px: 1.5,
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
                  autoComplete: "off",
                  spellCheck: false,
                }}
                sx={{
                  "& .MuiOutlinedInput-root": {
                    borderRadius: 2,
                  },
                }}
              />
            )}

            <Button
              fullWidth
              size="large"
              variant="contained"
              onClick={handleVerifyCode}
              disabled={!verificationCode || isSubmitting}
              startIcon={
                isSubmitting ? (
                  <CircularProgress size={20} color="inherit" />
                ) : null
              }
              sx={{
                py: 1.5,
                borderRadius: 2,
                textTransform: "none",
                fontSize: "1rem",
                fontWeight: 600,
              }}
            >
              {t("auth:auth.verify")}
            </Button>

            <Button
              fullWidth
              variant="text"
              onClick={handleBack}
              disabled={isSubmitting}
              sx={{
                py: 1,
                textTransform: "none",
              }}
            >
              {t("common:common.back")}
            </Button>
          </>
        )}
      </Stack>
    </Box>
  );
}
