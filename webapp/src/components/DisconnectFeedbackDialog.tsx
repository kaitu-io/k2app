/**
 * DisconnectFeedbackDialog — post-disconnect 5-star rating dialog.
 *
 * Shown once after each user-initiated disconnect when the connection
 * lasted at least MIN_FEEDBACK_DURATION_SEC (gated upstream in
 * connection.store). 4-5 stars submit a "good" rating instantly. 3 stars
 * submits "bad" instantly. 1-2 stars expand an inline detail step with
 * optional problem-tag chips, then submit "bad" + auto-ticket whose body
 * always uses zh-CN labels (admin reads in Chinese).
 */

import { useState, useEffect, useCallback, useRef } from 'react';
import {
  Dialog,
  DialogTitle,
  DialogContent,
  DialogActions,
  Button,
  Rating,
  Chip,
  Box,
  Typography,
} from '@mui/material';
import { useTranslation } from 'react-i18next';
import { useConnectionStore, type LastConnectionInfo } from '../stores/connection.store';
import { useAlertStore } from '../stores/alert.store';
import { cloudApi } from '../services/cloud-api';
import { getDeviceUdid } from '../services/device-udid';
import { refreshNetworkEnv } from '../services/network-env';
import { randomUUID } from '../utils/uuid';

type TagKey = 'slow' | 'cantConnect' | 'frequentDrops' | 'contentBlocked' | 'other';
const TAG_KEYS: readonly TagKey[] = ['slow', 'cantConnect', 'frequentDrops', 'contentBlocked', 'other'] as const;

// Locale-independent zh-CN labels written into auto-tickets. The admin team
// reads tickets in Chinese, so ticket content is locked to zh-CN regardless
// of the user's current locale.
const TAG_LABEL_ZH: Record<TagKey, string> = {
  slow: '速度慢',
  cantConnect: '连不上',
  frequentDrops: '经常断开',
  contentBlocked: '视频或网页打不开',
  other: '其他',
};

function formatConnectionInfo(info: LastConnectionInfo): string {
  return [
    `Server: ${info.name} (${info.domain})`,
    `Region: ${info.country || 'unknown'}`,
    `Type: ${info.source}`,
    `Duration: ${info.durationSec}s`,
    `Rule: ${info.ruleMode}`,
    `OS: ${info.os}`,
    `Version: ${info.appVersion}`,
    `Commit: ${info.commit || '-'}`,
  ].join('\n');
}

async function submitRating(
  rating: 'good' | 'bad',
  info: LastConnectionInfo,
  feedbackId: string,
): Promise<void> {
  const networkEnv = await refreshNetworkEnv();
  try {
    await cloudApi.post('/api/user/connection-rating', {
      rating,
      feedbackId,
      server: {
        domain: info.domain,
        name: info.name,
        country: info.country,
        source: info.source,
      },
      connection: {
        durationSec: info.durationSec,
        ruleMode: info.ruleMode,
        os: info.os,
        appVersion: info.appVersion,
      },
      network: networkEnv,
    });
  } catch (err) {
    console.warn('[DisconnectFeedback] rating submission failed:', err);
  }
}

async function submitNegativeFeedback(
  info: LastConnectionInfo,
  stars: number,
  tags: TagKey[],
): Promise<void> {
  const feedbackId = randomUUID();
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

  const tagsLine = tags.length > 0
    ? tags.map((k) => TAG_LABEL_ZH[k]).join(', ')
    : '无';

  // Step 2: Submit ticket with auto_generated flag (hidden from user)
  try {
    await cloudApi.post('/api/user/ticket', {
      content: `[Auto] 用户报告体验问题 (${stars}★)\nTags: ${tagsLine}\n\n${formatConnectionInfo(info)}`,
      feedbackId,
      os: info.os,
      app_version: info.appVersion,
      commit: info.commit,
      auto_generated: true,
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

  // Step 5: Submit rating
  await submitRating('bad', info, feedbackId);
}

function fireSubmit(stars: number, tags: TagKey[], info: LastConnectionInfo): void {
  if (stars <= 2) {
    submitNegativeFeedback(info, stars, tags).catch((err) => {
      console.error('[DisconnectFeedback] negative feedback error:', err);
    });
    return;
  }
  const rating: 'good' | 'bad' = stars >= 4 ? 'good' : 'bad';
  const feedbackId = randomUUID();
  submitRating(rating, info, feedbackId).catch((err) => {
    console.error('[DisconnectFeedback] rating error:', err);
  });
}

export function DisconnectFeedbackDialog() {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const [stars, setStars] = useState(0);
  const [tags, setTags] = useState<TagKey[]>([]);
  const connectionInfoRef = useRef<LastConnectionInfo | null>(null);

  const pendingFeedback = useConnectionStore((s) => s.pendingFeedback);
  const lastConnectionInfo = useConnectionStore((s) => s.lastConnectionInfo);
  const clearPendingFeedback = useConnectionStore((s) => s.clearPendingFeedback);
  const showAlert = useAlertStore((s) => s.showAlert);

  useEffect(() => {
    if (pendingFeedback) {
      connectionInfoRef.current = lastConnectionInfo;
      clearPendingFeedback();
      setStars(0);
      setTags([]);
      setOpen(true);
    }
  }, [pendingFeedback, lastConnectionInfo, clearPendingFeedback]);

  const closeWithThanks = useCallback(() => {
    setOpen(false);
    showAlert(t('feedback:feedback.disconnectFeedback.thankYou'), 'info');
  }, [showAlert, t]);

  const handleStarsChange = useCallback((_e: unknown, newValue: number | null) => {
    const value = newValue ?? 0;
    if (value <= 0) return;
    setStars(value);
    if (value >= 3) {
      const info = connectionInfoRef.current;
      connectionInfoRef.current = null;
      if (info) fireSubmit(value, [], info);
      closeWithThanks();
    }
  }, [closeWithThanks]);

  const handleSubmitDetail = useCallback(() => {
    const info = connectionInfoRef.current;
    connectionInfoRef.current = null;
    if (info) fireSubmit(stars, tags, info);
    closeWithThanks();
  }, [stars, tags, closeWithThanks]);

  const toggleTag = useCallback((key: TagKey) => {
    setTags((prev) =>
      prev.includes(key) ? prev.filter((k) => k !== key) : [...prev, key],
    );
  }, []);

  const inDetail = stars > 0 && stars <= 2;

  return (
    <Dialog
      open={open}
      disableEscapeKeyDown
      onClose={(_event, reason) => {
        if (reason === 'backdropClick') return;
      }}
      PaperProps={{ sx: { minWidth: 320, textAlign: 'center' } }}
    >
      <DialogTitle sx={{ pb: 1 }}>
        {t('feedback:feedback.disconnectFeedback.title')}
      </DialogTitle>
      <DialogContent sx={{ pb: 1 }}>
        <Box sx={{ display: 'flex', justifyContent: 'center', py: 1 }}>
          <Rating
            value={stars}
            size="large"
            onChange={handleStarsChange}
            sx={{ color: 'warning.main' }}
          />
        </Box>
        {inDetail && (
          <Box sx={{ mt: 2 }}>
            <Typography variant="body2" sx={{ mb: 1.5, color: 'text.secondary' }}>
              {t('feedback:feedback.disconnectFeedback.detailTitle')}
            </Typography>
            <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 1, justifyContent: 'center' }}>
              {TAG_KEYS.map((key) => {
                const selected = tags.includes(key);
                return (
                  <Chip
                    key={key}
                    label={t(`feedback:feedback.disconnectFeedback.tags.${key}`)}
                    variant={selected ? 'filled' : 'outlined'}
                    color={selected ? 'primary' : 'default'}
                    onClick={() => toggleTag(key)}
                  />
                );
              })}
            </Box>
          </Box>
        )}
      </DialogContent>
      {inDetail && (
        <DialogActions sx={{ justifyContent: 'center', pb: 2 }}>
          <Button variant="contained" onClick={handleSubmitDetail} sx={{ minWidth: 100 }}>
            {t('feedback:feedback.disconnectFeedback.submit')}
          </Button>
        </DialogActions>
      )}
    </Dialog>
  );
}
