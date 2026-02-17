import React, { useState, useRef, useEffect } from "react";
import {
  Box,
  Typography,
  TextField,
  Button,
  Stack,
  Alert,
} from "@mui/material";
import { useNavigate } from "react-router-dom";
import { useTranslation } from "react-i18next";

import EmailTextField from "../components/EmailTextField";
import BackButton from "../components/BackButton";
import { cloudApi } from '../services/cloud-api';
import { delayedFocus } from '../utils/ui';

export default function UpdateLoginEmail() {
  const navigate = useNavigate();
  const { t } = useTranslation();
  const [email, setEmail] = useState("");
  const [code, setCode] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [sending, setSending] = useState(false);
  const [countdown, setCountdown] = useState(0);
  const [success, setSuccess] = useState(false);

  // Ref for delayed focus
  const emailInputRef = useRef<HTMLDivElement>(null);

  // Delayed focus on mount
  useEffect(() => {
    const cancel = delayedFocus(
      () => emailInputRef.current?.querySelector('input') as HTMLInputElement | null,
      100
    );
    return cancel;
  }, []);

  // 发送验证码
  const handleSendCode = async () => {
    if (!email) {
      setError(t('auth:updateEmail.pleaseEnterEmail'));
      return;
    }
    setError("");
    setSending(true);
    try {
      await cloudApi.post('/api/user/email/send-bind-verification', { email });
      setSuccess(true);
      setCountdown(60);
      const timer = setInterval(() => {
        setCountdown(prev => {
          if (prev <= 1) {
            clearInterval(timer);
            return 0;
          }
          return prev - 1;
        });
      }, 1000);
    } catch (err) {
      setError(t('auth:updateEmail.sendCodeFailed'));
    } finally {
      setSending(false);
    }
  };

  // 提交邮箱+验证码
  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    setLoading(true);
    try {
      await cloudApi.post('/api/user/email/update-email', { email, verificationCode: code });
      setSuccess(true);
      navigate("/account", { replace: true });
    } catch (err) {
      setError(t('auth:updateEmail.setEmailFailed'));
    } finally {
      setLoading(false);
    }
  };

  return (
    <Box sx={{
      width: "100%",
      py: 0.5,
      backgroundColor: "transparent",
      position: "relative"
    }}>
      <BackButton to="/account" />
      <Box sx={{ display: 'flex', alignItems: 'center', mb: 1.5, px: 1, pt: 7 }}>
        <Typography variant="h6" sx={{ flex: 1, fontWeight: 600 }} component="span">
          {t('auth:updateEmail.title')}
        </Typography>
      </Box>
      <Box sx={{ width: "100%", bgcolor: 'background.paper', p: 2, borderRadius: 2 }}>
        <form onSubmit={handleSubmit}>
          <Box sx={{ p: 0 }}>
            <Stack spacing={2}>
              {error && (
                <Alert severity="error" onClose={() => setError("")}>{error}</Alert>
              )}
              {success && !loading && (
                <Alert severity="success">
                  {t('auth:updateEmail.codeSentSuccess')}
                  <Typography variant="caption" display="block" sx={{ mt: 0.5 }}>
                    {t('auth:auth.checkSpamFolder')}
                  </Typography>
                </Alert>
              )}
              <EmailTextField
                ref={emailInputRef}
                label={t('auth:updateEmail.emailLabel')}
                value={email}
                onChange={setEmail}
                required
                fullWidth
                placeholder={t('auth:updateEmail.emailPlaceholder')}
                disabled={loading}
                size="small"
              />
              <Box sx={{ display: 'flex', gap: 2 }}>
                <TextField
                  label={t('auth:updateEmail.verificationCodeLabel')}
                  value={code}
                  onChange={e => setCode(e.target.value)}
                  onKeyDown={e => {
                    if (e.key === 'Enter' && !loading && email && code) {
                      handleSubmit(e as any);
                    }
                  }}
                  required
                  fullWidth
                  placeholder={t('auth:updateEmail.verificationCodePlaceholder')}
                  disabled={loading}
                  size="small"
                  inputProps={{
                    autoCapitalize: "none",
                    autoCorrect: "off",
                    autoComplete: "one-time-code",
                    spellCheck: false,
                    inputMode: "numeric",
                    pattern: "[0-9]*",
                  }}
                />
                <Button
                  variant="outlined"
                  onClick={handleSendCode}
                  disabled={sending || countdown > 0 || !email}
                  sx={{ minWidth: 120 }}
                  size="small"
                >
                  {countdown > 0 ? t('auth:updateEmail.retryAfter', { seconds: countdown }) : t('auth:updateEmail.sendCode')}
                </Button>
              </Box>
              <Box sx={{ display: "flex", gap: 2, justifyContent: "flex-end" }}>
                <Button
                  variant="outlined"
                  onClick={() => navigate("/account")}
                  disabled={loading}
                  size="small"
                >
                  {t('common:common.cancel')}
                </Button>
                <Button
                  type="submit"
                  variant="contained"
                  disabled={loading || !email || !code}
                  size="small"
                >
                  {loading ? t('auth:updateEmail.submitting') : t('auth:updateEmail.confirmSetting')}
                </Button>
              </Box>
            </Stack>
          </Box>
        </form>
      </Box>
    </Box>
  );
}
