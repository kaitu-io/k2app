import { useState, useEffect, useCallback, useRef } from "react";
import { useSearchParams } from "react-router-dom";
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
} from "@mui/material";
import {
  Send as SendIcon,
  Email as EmailIcon,
  CheckCircle as SuccessIcon,
} from "@mui/icons-material";
import { useTranslation } from "react-i18next";

import BackButton from "../components/BackButton";
import { cloudApi } from '../services/cloud-api';
import { useAuthStore } from '../stores/auth.store';
import { useUser } from '../hooks/useUser';

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

export default function SubmitTicket() {
  const { t } = useTranslation();
  const [searchParams] = useSearchParams();
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated);
  const { user } = useUser();

  // Feedback mode - when navigating from FeedbackButton
  const isFeedbackMode = searchParams.get('feedback') === 'true';
  const feedbackIdRef = useRef<string | null>(null);
  const uploadAttemptedRef = useRef(false);

  // Form state
  const [subject, setSubject] = useState("");
  const [content, setContent] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [submitResult, setSubmitResult] = useState<{ success: boolean; message: string } | null>(null);

  // Log upload state (silent, no UI display)
  const [logUploadStatus, setLogUploadStatus] = useState<LogUploadStatus>('idle');

  // Check platform capabilities
  const canUploadLogs = !!window._platform?.uploadLogs;

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
    // 验证输入
    if (!subject.trim()) {
      setSubmitResult({ success: false, message: t('ticket:ticket.validation.subjectRequired') });
      return;
    }
    if (!content.trim()) {
      setSubmitResult({ success: false, message: t('ticket:ticket.validation.contentRequired') });
      return;
    }
    if (subject.length > 200) {
      setSubmitResult({ success: false, message: t('ticket:ticket.validation.subjectTooLong') });
      return;
    }
    if (content.length > 5000) {
      setSubmitResult({ success: false, message: t('ticket:ticket.validation.contentTooLong') });
      return;
    }

    setIsSubmitting(true);
    setSubmitResult(null);

    try {
      const response = await cloudApi.post('/api/user/ticket', {
        subject: subject.trim(),
        content: content.trim(),
        // Include feedbackId if logs were uploaded successfully
        ...(feedbackIdRef.current && logUploadStatus === 'success'
          ? { feedbackId: feedbackIdRef.current }
          : {}),
      });

      if (response.code === 0) {
        setSubmitResult({ success: true, message: t('ticket:ticket.submitSuccess') });
        // 清空表单
        setSubject("");
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
    }}>
      <BackButton to="/faq" />

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
              {/* Log upload happens silently in background - no UI display */}

              <Card>
                <CardContent>
                  <Stack spacing={3}>
                    <Typography variant="h6">
                      {isFeedbackMode ? t('ticket:ticket.feedbackTitle') : t('ticket:ticket.title')}
                    </Typography>

                    <Typography variant="body2" color="text.secondary">
                      {isFeedbackMode ? t('ticket:ticket.feedbackDescription') : t('ticket:ticket.description')}
                    </Typography>

                    <TextField
                      label={t('ticket:ticket.subjectLabel')}
                      placeholder={t('ticket:ticket.subjectPlaceholder')}
                      value={subject}
                      onChange={(e) => setSubject(e.target.value)}
                      disabled={isSubmitting}
                      fullWidth
                      inputProps={{ maxLength: 200 }}
                      helperText={`${subject.length}/200`}
                    />

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
                      disabled={isSubmitting || !subject.trim() || !content.trim()}
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

              {/* 提示信息 */}
              <Card variant="outlined">
                <CardContent>
                  <Stack spacing={1}>
                    <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                      <EmailIcon color="action" fontSize="small" />
                      <Typography variant="subtitle2" color="text.secondary">
                        {t('ticket:ticket.howItWorks')}
                      </Typography>
                    </Box>
                    <Typography variant="body2" color="text.secondary">
                      {t('ticket:ticket.howItWorksDescription')}
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
