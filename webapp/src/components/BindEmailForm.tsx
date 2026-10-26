/**
 * BindEmailForm — 给当前已登录账号设置 / 绑定邮箱的两步表单：发验证码
 * （/api/user/email/send-bind-verification）→ 提交（/api/user/email/update-email）。
 *
 * 【设置邮箱】页（UpdateLoginEmail）和【其他设备安装】的零身份绑定区共用。成功后调
 * `onBound()`，跳转 / 刷新用户由调用方决定。
 */
import React, { useState, useRef, useEffect } from "react";
import { Box, Typography, TextField, Button, Stack, Alert } from "@mui/material";
import { useTranslation } from "react-i18next";

import EmailTextField from "./EmailTextField";
import { cloudApi } from '../services/cloud-api';
import { ERROR_CODES, handleResponseError } from "../utils/errorCode";
import { delayedFocus } from '../utils/ui';

export interface BindEmailFormProps {
  onBound(): void;
  /** 挂载时聚焦邮箱输入（独立的设置邮箱页要；嵌在别的页面里时不要，免得弹键盘）。 */
  autoFocus?: boolean;
  /** 渲染在提交按钮左侧的额外动作（如设置邮箱页的「取消」）；`busy` = 正在提交。 */
  renderSecondaryAction?: (busy: boolean) => React.ReactNode;
  /**
   * 邮箱已属别的账号（409001）时显示的文案。缺省用通用的「…或直接用该邮箱登录原账号」；
   * 零身份设备账号的绑定区必须传入「请换一个邮箱绑定；如需合并请联系客服」（spec §4.4）——
   * 去登录原账号会丢掉本机账号上的会员。
   */
  emailConflictMessage?: string;
}

export default function BindEmailForm({ onBound, autoFocus = false, renderSecondaryAction, emailConflictMessage }: BindEmailFormProps) {
  const { t } = useTranslation();
  const [email, setEmail] = useState("");
  const [code, setCode] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  const [sending, setSending] = useState(false);
  const [countdown, setCountdown] = useState(0);
  const [success, setSuccess] = useState(false);

  const emailInputRef = useRef<HTMLDivElement>(null);
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null);

  useEffect(() => {
    if (!autoFocus) return;
    return delayedFocus(
      () => emailInputRef.current?.querySelector('input') as HTMLInputElement | null,
      100
    );
  }, [autoFocus]);

  useEffect(() => () => {
    if (timerRef.current) clearInterval(timerRef.current);
  }, []);

  const checkResponse = (code: number, message: string | undefined, fallback: string) => {
    if (emailConflictMessage && code === ERROR_CODES.EMAIL_ALREADY_IN_USE) {
      throw new Error(emailConflictMessage);
    }
    handleResponseError(code, message, t, fallback);
  };

  // 发送验证码
  const handleSendCode = async () => {
    if (!email) {
      setError(t('auth:updateEmail.pleaseEnterEmail'));
      return;
    }
    setError("");
    setSending(true);
    try {
      // cloudApi.post does NOT throw on code !== 0; it returns the response
      // verbatim. Without handleResponseError here the user would see the
      // "code sent" UX even on a backend rejection.
      const response = await cloudApi.post('/api/user/email/send-bind-verification', { email });
      checkResponse(response.code, response.message, t('auth:updateEmail.sendCodeFailed'));
      setSuccess(true);
      setCountdown(60);
      if (timerRef.current) clearInterval(timerRef.current);
      timerRef.current = setInterval(() => {
        setCountdown(prev => {
          if (prev <= 1) {
            if (timerRef.current) clearInterval(timerRef.current);
            timerRef.current = null;
            return 0;
          }
          return prev - 1;
        });
      }, 1000);
    } catch (err) {
      setError(err instanceof Error ? err.message : t('auth:updateEmail.sendCodeFailed'));
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
      const response = await cloudApi.post('/api/user/email/update-email', { email, verificationCode: code });
      // Clear the input on expired code so the user can't keep submitting the
      // same stale value, and surface the precise i18n message.
      if (response.code === ERROR_CODES.VERIFICATION_CODE_EXPIRED) {
        setCode("");
      }
      checkResponse(response.code, response.message, t('auth:updateEmail.setEmailFailed'));
      setSuccess(true);
      onBound();
    } catch (err) {
      setError(err instanceof Error ? err.message : t('auth:updateEmail.setEmailFailed'));
    } finally {
      setLoading(false);
    }
  };

  return (
    <form onSubmit={handleSubmit} data-testid="bind-email-form">
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
          {renderSecondaryAction?.(loading)}
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
    </form>
  );
}
