/**
 * DisconnectFeedbackDialog — mandatory post-disconnect quality dialog
 *
 * Shown once after each user-initiated disconnect (authenticated only).
 * "不好" auto-submits a ticket + uploads logs for diagnostics.
 */

import { useState, useEffect, useCallback, useRef } from 'react';
import {
  Dialog,
  DialogTitle,
  DialogActions,
  Button,
} from '@mui/material';
import { useTranslation } from 'react-i18next';
import { useConnectionStore, type LastConnectionInfo } from '../stores/connection.store';
import { useAlertStore } from '../stores/alert.store';
import { cloudApi } from '../services/cloud-api';
import { getDeviceUdid } from '../services/device-udid';

function generateFeedbackId(): string {
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, (c) => {
    const r = (Math.random() * 16) | 0;
    const v = c === 'x' ? r : (r & 0x3) | 0x8;
    return v.toString(16);
  });
}

function formatConnectionInfo(info: LastConnectionInfo): string {
  return [
    `Server: ${info.name} (${info.domain})`,
    `Region: ${info.country || 'unknown'}`,
    `Type: ${info.source}`,
    `Duration: ${info.durationSec}s`,
    `Rule: ${info.ruleMode}`,
    `OS: ${info.os}`,
    `Version: ${info.appVersion}`,
  ].join('\n');
}

async function submitNegativeFeedback(info: LastConnectionInfo): Promise<void> {
  const feedbackId = generateFeedbackId();
  let s3Keys: Array<{ name: string; s3Key: string }> = [];

  // Step 1: Upload logs (best-effort)
  if (window._platform?.uploadLogs) {
    try {
      const result = await window._platform.uploadLogs({
        email: null,
        reason: 'disconnect_feedback_bad',
        platform: window._platform.os,
        version: window._platform.version,
        feedbackId,
      });
      if (result.success && result.s3Keys?.length) {
        s3Keys = result.s3Keys;
      }
    } catch (err) {
      console.warn('[DisconnectFeedback] uploadLogs failed:', err);
    }
  }

  // Step 2: Submit ticket (proceeds even if logs failed)
  try {
    await cloudApi.post('/api/user/ticket', {
      content: `[Auto] User reported bad connection experience after disconnect.\n\n${formatConnectionInfo(info)}`,
      feedbackId,
      os: info.os,
      app_version: info.appVersion,
    });
  } catch (err) {
    console.warn('[DisconnectFeedback] ticket submission failed:', err);
  }

  // Step 3: Register log metadata (only if logs were uploaded)
  if (s3Keys.length > 0) {
    try {
      const udid = await getDeviceUdid();
      await cloudApi.post('/api/user/device-log', {
        udid,
        feedbackId,
        s3Keys,
        reason: 'disconnect_feedback_bad',
        meta: {
          os: info.os,
          appVersion: info.appVersion,
          channel: window._platform?.updater?.channel ?? 'stable',
        },
      });
    } catch (err) {
      console.warn('[DisconnectFeedback] device-log registration failed:', err);
    }
  }

  // Step 4: Slack notification
  try {
    await cloudApi.post('/api/user/feedback-notify', {
      reason: 'disconnect_feedback_bad',
      platform: info.os,
      version: info.appVersion,
      feedbackId,
      s3Keys,
    });
  } catch (err) {
    console.warn('[DisconnectFeedback] feedback-notify failed:', err);
  }
}

export function DisconnectFeedbackDialog() {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const connectionInfoRef = useRef<LastConnectionInfo | null>(null);

  const pendingFeedback = useConnectionStore((s) => s.pendingFeedback);
  const lastConnectionInfo = useConnectionStore((s) => s.lastConnectionInfo);
  const clearPendingFeedback = useConnectionStore((s) => s.clearPendingFeedback);
  const showAlert = useAlertStore((s) => s.showAlert);

  // When pendingFeedback becomes true, consume it and open dialog
  useEffect(() => {
    if (pendingFeedback) {
      connectionInfoRef.current = lastConnectionInfo;
      clearPendingFeedback();
      setOpen(true);
    }
  }, [pendingFeedback, lastConnectionInfo, clearPendingFeedback]);

  const handleGood = useCallback(() => {
    setOpen(false);
    connectionInfoRef.current = null;
  }, []);

  const handleBad = useCallback(() => {
    setOpen(false);
    const info = connectionInfoRef.current;
    connectionInfoRef.current = null;

    if (info) {
      showAlert(t('feedback:feedback.disconnectFeedback.thankYou'), 'info');
      // Fire-and-forget
      submitNegativeFeedback(info).catch((err) => {
        console.error('[DisconnectFeedback] submission error:', err);
      });
    }
  }, [showAlert, t]);

  return (
    <Dialog
      open={open}
      disableEscapeKeyDown
      onClose={(_event, reason) => {
        // Block backdrop click — force user to choose
        if (reason === 'backdropClick') return;
      }}
      PaperProps={{
        sx: { minWidth: 280, textAlign: 'center' },
      }}
    >
      <DialogTitle sx={{ pb: 1 }}>
        {t('feedback:feedback.disconnectFeedback.title')}
      </DialogTitle>
      <DialogActions sx={{ justifyContent: 'center', pb: 2, gap: 2 }}>
        <Button
          variant="outlined"
          onClick={handleGood}
          sx={{ minWidth: 80 }}
        >
          {t('feedback:feedback.disconnectFeedback.good')}
        </Button>
        <Button
          variant="contained"
          color="error"
          onClick={handleBad}
          sx={{ minWidth: 80 }}
        >
          {t('feedback:feedback.disconnectFeedback.bad')}
        </Button>
      </DialogActions>
    </Dialog>
  );
}
