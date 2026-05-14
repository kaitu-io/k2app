import type { LastConnectionInfo } from '../stores/connection.store';
import { cloudApi } from './cloud-api';
import { getDeviceUdid } from './device-udid';
import { refreshNetworkEnv } from './network-env';
import { randomUUID } from '../utils/uuid';

export type TagKey = 'slow' | 'cantConnect' | 'frequentDrops' | 'contentBlocked' | 'other';

export const TAG_KEYS: readonly TagKey[] = [
  'slow',
  'cantConnect',
  'frequentDrops',
  'contentBlocked',
  'other',
] as const;

// Locale-independent zh-CN labels written into auto-tickets. The admin team
// reads tickets in Chinese, so ticket content is locked to zh-CN regardless
// of the user's current locale.
export const TAG_LABEL_ZH: Record<TagKey, string> = {
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

export async function submitRating(
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

export async function submitNegativeFeedback(
  info: LastConnectionInfo,
  stars: number,
  tags: TagKey[],
): Promise<void> {
  const feedbackId = randomUUID();
  let s3Keys: Array<{ name: string; s3Key: string }> = [];

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

  await submitRating('bad', info, feedbackId);
}

/**
 * Submit a non-negative rating (good or 3★-bad-no-tags). Used for explicit
 * star taps and for the silent-default path (no tap within the countdown).
 * Both paths produce the same payload — the rating value alone is the signal.
 * Negative ratings (1-2★ with tag chips) go through submitNegativeFeedback.
 */
export function submitSimpleRating(
  rating: 'good' | 'bad',
  info: LastConnectionInfo,
): void {
  const feedbackId = randomUUID();
  submitRating(rating, info, feedbackId).catch((err) => {
    console.error('[DisconnectFeedback] rating error:', err);
  });
}

export function submitNegativeFire(
  info: LastConnectionInfo,
  stars: number,
  tags: TagKey[],
): void {
  submitNegativeFeedback(info, stars, tags).catch((err) => {
    console.error('[DisconnectFeedback] negative feedback error:', err);
  });
}
