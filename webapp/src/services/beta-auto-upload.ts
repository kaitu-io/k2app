/**
 * Beta Auto-Upload Service
 *
 * When on beta channel, periodically uploads device logs to S3
 * and registers metadata in the database for troubleshooting.
 *
 * Replaces the Rust-side timer (log_upload::start_beta_auto_upload)
 * so that webapp can also call cloudApi for device-log registration.
 */

import { cloudApi } from './cloud-api';
import { getDeviceUdid } from './device-udid';

const INITIAL_DELAY_MS = 5 * 60 * 1000; // 5 minutes
const INTERVAL_MS = 24 * 60 * 60 * 1000; // 24 hours

let intervalId: ReturnType<typeof setInterval> | null = null;
let timeoutId: ReturnType<typeof setTimeout> | null = null;

async function doUploadAndRegister() {
  if (!window._platform?.uploadLogs) return;

  try {
    const udid = await getDeviceUdid();
    const result = await window._platform.uploadLogs({
      reason: 'beta-auto-upload',
      platform: window._platform.os,
      version: window._platform.version,
    });

    if (result.success && result.s3Keys?.length) {
      await cloudApi.post('/api/user/device-log', {
        udid,
        s3Keys: result.s3Keys,
        reason: 'beta-auto-upload',
        meta: {
          os: window._platform.os,
          appVersion: window._platform.version,
          channel: 'beta',
        },
      });
      console.debug('[BetaAutoUpload] Logs uploaded and registered');
    }
  } catch {
    // Silent — best-effort background upload
  }
}

/**
 * Start the beta auto-upload timer.
 * Only starts if platform is on beta channel and uploadLogs is available.
 * Idempotent — calling multiple times is safe.
 */
export function startBetaAutoUpload() {
  if (window._platform?.updater?.channel !== 'beta') return;
  if (!window._platform?.uploadLogs) return;
  if (timeoutId || intervalId) return; // already running

  console.info('[BetaAutoUpload] Starting (5min delay, 24h interval)');

  timeoutId = setTimeout(() => {
    doUploadAndRegister();
    intervalId = setInterval(doUploadAndRegister, INTERVAL_MS);
  }, INITIAL_DELAY_MS);
}

/**
 * Stop the beta auto-upload timer.
 */
export function stopBetaAutoUpload() {
  if (timeoutId) {
    clearTimeout(timeoutId);
    timeoutId = null;
  }
  if (intervalId) {
    clearInterval(intervalId);
    intervalId = null;
  }
}
