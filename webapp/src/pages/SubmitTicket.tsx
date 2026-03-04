import { useState, useEffect, useCallback, useRef } from "react";
import { useSearchParams, useNavigate } from "react-router-dom";
import {
  Box,
  Typography,
  Card,
  CardContent,
  TextField,
  Button,
  Alert,
  Stack,
  CircularProgress,
  Stepper,
  Step,
  StepLabel,
} from "@mui/material";
import {
  Send as SendIcon,
  Email as EmailIcon,
  CheckCircle as SuccessIcon,
  EditNote as EditNoteIcon,
  CloudUpload as CloudUploadIcon,
  MarkEmailRead as MarkEmailReadIcon,
} from "@mui/icons-material";
import { useTranslation } from "react-i18next";

import BackButton from "../components/BackButton";
import { cloudApi } from '../services/cloud-api';
import { useAuthStore } from '../stores/auth.store';
import { useUser } from '../hooks/useUser';
import i18n from '../i18n/i18n';
import type { StatusResponseData } from '../services/vpn-types';

// Log upload status (internal tracking only, no UI display)
type LogUploadStatus = 'idle' | 'uploading' | 'success' | 'error';

/**
 * Generate a unique feedback ID (UUID v4)
 */
function generateFeedbackId(): string {
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, (c) => {
    const r = Math.random() * 16 | 0;
    const v = c === 'x' ? r : (r & 0x3 | 0x8);
    return v.toString(16);
  });
}

/**
 * Gather system info for ticket submission (best-effort)
 */
async function gatherSystemInfo(): Promise<Record<string, string | undefined>> {
  const info: Record<string, string | undefined> = {
    os: window._platform?.os,
    app_version: window._platform?.version,
    channel: window._platform?.updater?.channel ?? 'stable',
    submit_time: new Date().toISOString(),
    language: i18n.language,
  };

  try {
    const resp = await window._k2.run<StatusResponseData>('status');
    if (resp.code === 0 && resp.data) {
      info.vpn_state = resp.data.state;
    }
  } catch {
    // best-effort, ignore if VPN status unavailable
  }

  return info;
}

export default function SubmitTicket() {
  const { t } = useTranslation();
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated);
  const { user } = useUser();

  // Feedback mode - when navigating from FeedbackButton
  const isFeedbackMode = searchParams.get('feedback') === 'true';
  const feedbackIdRef = useRef<string | null>(null);
  const uploadAttemptedRef = useRef(false);

  // Form state
  const [content, setContent] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [submitResult, setSubmitResult] = useState<{ success: boolean; message: string } | null>(null);

  // Log upload state (silent, no UI display)
  const [logUploadStatus, setLogUploadStatus] = useState<LogUploadStatus>('idle');

  // Check platform capabilities
  const canUploadLogs = !!window._platform?.uploadLogs;
  const isBetaUser = window._platform?.updater?.channel === 'beta' ||
    window._platform?.version?.includes('-beta');

  // Upload logs silently (no UI feedback)
  const uploadLogs = useCallback(async () => {
    if (!canUploadLogs) {
      setLogUploadStatus('error');
      return;
    }

    // Generate feedbackId if not already generated
    if (!feedbackIdRef.current) {
      feedbackIdRef.current = generateFeedbackId();
    }

    setLogUploadStatus('uploading');

    try {
      const result = await window._platform.uploadLogs!({
        email: isAuthenticated ? user?.loginIdentifies?.[0]?.value : null,
        reason: 'user_feedback_report',
        platform: window._platform.os,
        version: window._platform.version,
        feedbackId: feedbackIdRef.current,
      });

      if (result.success) {
        setLogUploadStatus('success');
        console.debug('[SubmitTicket] Log upload successful, feedbackId:', feedbackIdRef.current);
      } else {
        setLogUploadStatus('error');
        console.debug('[SubmitTicket] Log upload failed:', result.error);
      }
    } catch (error) {
      console.error('[SubmitTicket] Failed to upload logs:', error);
      setLogUploadStatus('error');
    }
  }, [canUploadLogs, isAuthenticated, user]);

  // Auto-upload logs when entering the page (always, for better support)
  useEffect(() => {
    if (!uploadAttemptedRef.current && canUploadLogs) {
      uploadAttemptedRef.current = true;
      uploadLogs();
    }
  }, [uploadLogs, canUploadLogs]);

  const handleSubmit = async () => {
    if (!content.trim()) {
      setSubmitResult({ success: false, message: t('ticket:ticket.validation.contentRequired') });
      return;
    }
    if (content.length > 5000) {
      setSubmitResult({ success: false, message: t('ticket:ticket.validation.contentTooLong') });
      return;
    }

    setIsSubmitting(true);
    setSubmitResult(null);

    try {
      const systemInfo = await gatherSystemInfo();
      const response = await cloudApi.post('/api/user/ticket', {
        content: content.trim(),
        // Include feedbackId if logs were uploaded successfully
        ...(feedbackIdRef.current && logUploadStatus === 'success'
          ? { feedbackId: feedbackIdRef.current }
          : {}),
        ...systemInfo,
      });

      if (response.code === 0) {
        setSubmitResult({ success: true, message: t('ticket:ticket.submitSuccess') });
        setContent("");
      } else {
        console.error('[SubmitTicket] Submit failed:', response.code, response.message);
        setSubmitResult({ success: false, message: t('ticket:ticket.submitFailed') });
      }
    } catch (error) {
      console.error('[SubmitTicket] Failed to submit ticket:', error);
      setSubmitResult({ success: false, message: t('ticket:ticket.submitFailed') });
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <Box sx={{
      width: "100%",
      height: "100%",
      position: "relative"
    }} data-tour="submit-ticket-page">
      <BackButton />

      <Box sx={{
        width: "100%",
        height: "100%",
        display: "flex",
        justifyContent: "center",
        alignItems: "flex-start",
        pt: 9
      }}>
        <Box sx={{
          width: 500,
          display: "flex",
          flexDirection: "column",
          gap: 2,
          overflow: "auto",
          height: "100%",
          pr: 0.5,
          pb: 4,
          '&::-webkit-scrollbar': {
            width: '8px',
          },
          '&::-webkit-scrollbar-track': {
            background: 'transparent',
          },
          '&::-webkit-scrollbar-thumb': {
            background: (theme) => theme.palette.mode === 'dark' ? 'rgba(255,255,255,0.2)' : 'rgba(0,0,0,0.2)',
            borderRadius: '4px',
          },
          '&::-webkit-scrollbar-thumb:hover': {
            background: (theme) => theme.palette.mode === 'dark' ? 'rgba(255,255,255,0.3)' : 'rgba(0,0,0,0.3)',
          },
        }}>
          {submitResult?.success ? (
            /* 成功提示 - 独立显示，隐藏表单 */
            <Card sx={{ bgcolor: 'success.main', color: 'success.contrastText' }}>
              <CardContent>
                <Stack spacing={3} alignItems="center" py={4}>
                  <SuccessIcon sx={{ fontSize: 64 }} />
                  <Typography variant="h5" textAlign="center" fontWeight="bold">
                    {t('ticket:ticket.submitSuccessTitle')}
                  </Typography>
                  <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                    <EmailIcon sx={{ fontSize: 28 }} />
                    <Typography variant="h6" textAlign="center">
                      {t('ticket:ticket.nextStepHint')}
                    </Typography>
                  </Box>
                  <Typography variant="body1" textAlign="center" sx={{ opacity: 0.95, maxWidth: 400 }}>
                    {t('ticket:ticket.emailReplyHint')}
                  </Typography>
                </Stack>
              </CardContent>
            </Card>
          ) : (
            /* 工单表单 - 只在未成功时显示 */
            <>
              {isBetaUser && (
                <Alert
                  severity="warning"
                  action={
                    <Button
                      color="warning"
                      size="small"
                      onClick={() => navigate('/account')}
                    >
                      {t('ticket:ticket.switchToStable')}
                    </Button>
                  }
                >
                  {t('ticket:ticket.betaWarning')}
                </Alert>
              )}

              <Card>
                <CardContent>
                  <Stack spacing={3}>
                    <Typography variant="h6">
                      {isFeedbackMode ? t('ticket:ticket.feedbackTitle') : t('ticket:ticket.title')}
                    </Typography>

                    <Typography variant="body2" color="text.secondary">
                      {t('ticket:ticket.description')}
                    </Typography>

                    <TextField
                      label={t('ticket:ticket.contentLabel')}
                      placeholder={t('ticket:ticket.contentPlaceholder')}
                      value={content}
                      onChange={(e) => setContent(e.target.value)}
                      disabled={isSubmitting}
                      fullWidth
                      multiline
                      rows={6}
                      inputProps={{ maxLength: 5000 }}
                      helperText={`${content.length}/5000`}
                    />

                    <Button
                      variant="contained"
                      color="primary"
                      onClick={handleSubmit}
                      disabled={isSubmitting || !content.trim()}
                      startIcon={isSubmitting ? <CircularProgress size={20} color="inherit" /> : <SendIcon />}
                      fullWidth
                      size="large"
                    >
                      {isSubmitting ? t('ticket:ticket.submitting') : t('ticket:ticket.submitButton')}
                    </Button>

                    {submitResult && !submitResult.success && (
                      <Alert severity="error">
                        {submitResult.message}
                      </Alert>
                    )}
                  </Stack>
                </CardContent>
              </Card>

              {/* 工单处理流程 - Stepper */}
              <Card variant="outlined">
                <CardContent>
                  <Stack spacing={2}>
                    <Typography variant="subtitle2" color="text.secondary">
                      {t('ticket:ticket.howItWorks')}
                    </Typography>
                    <Stepper alternativeLabel>
                      <Step active>
                        <StepLabel
                          StepIconComponent={() => <EditNoteIcon color="primary" fontSize="small" />}
                        >
                          {t('ticket:ticket.stepper.step1')}
                        </StepLabel>
                      </Step>
                      <Step active>
                        <StepLabel
                          StepIconComponent={() => <CloudUploadIcon color="primary" fontSize="small" />}
                        >
                          {t('ticket:ticket.stepper.step2')}
                        </StepLabel>
                      </Step>
                      <Step active>
                        <StepLabel
                          StepIconComponent={() => <MarkEmailReadIcon color="primary" fontSize="small" />}
                        >
                          {t('ticket:ticket.stepper.step3')}
                        </StepLabel>
                      </Step>
                    </Stepper>
                    <Typography variant="caption" color="text.secondary" sx={{ textAlign: 'center' }}>
                      {t('ticket:ticket.dataCollectionHint')}
                    </Typography>
                  </Stack>
                </CardContent>
              </Card>
            </>
          )}
        </Box>
      </Box>
    </Box>
  );
}
