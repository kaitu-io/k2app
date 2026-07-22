/**
 * Shared status transformation for all platform bridges.
 *
 * Raw status from engine (via daemon HTTP, gateway SSE, or K2Plugin)
 * provides: state, error, startAt, uptimeSeconds.
 *
 * This function:
 * - Normalises daemon "stopped" to "disconnected"
 * - Accepts startAt as a Unix seconds integer (new engine format, Task 0A)
 * - Falls back to legacy connected_at / connectedAt RFC3339 strings
 * - Synthesises state='error' from (disconnected|connected) + error
 * - Computes derived fields: running, retrying, networkAvailable
 */

import type { StatusResponseData, ControlError, ServiceState } from './vpn-types';

export function transformStatus(raw: any): StatusResponseData {
  // Daemon outputs "stopped"; new engine outputs "disconnected".
  let state: ServiceState = raw.state === 'stopped' ? 'disconnected' : (raw.state ?? 'disconnected');
  const running = state === 'connecting' || state === 'connected';

  let error: ControlError | undefined;
  let retrying = false;

  if (raw.error) {
    if (typeof raw.error === 'object' && raw.error !== null && 'code' in raw.error) {
      error = { code: raw.error.code, message: raw.error.message || '' };
    } else {
      // Backward compat: old daemon sends string
      error = { code: 570, message: String(raw.error) };
    }
    // Error synthesis: disconnected + error → 'error' state
    if (state === 'disconnected' || state === 'connected') {
      // connected + error: TUN up but wire broken — engine retries on next traffic
      // disconnected + error: engine gave up
      const isClientError = [400, 401, 402, 403].includes(error.code);
      retrying = state === 'connected' && !isClientError;
      state = 'error';
    }
  }

  // startAt: prefer new integer field, fall back to legacy RFC3339 strings.
  let startAt: number | undefined;
  if (typeof raw.startAt === 'number') {
    startAt = raw.startAt;
  } else if (raw.connected_at) {
    // Tauri daemon legacy (snake_case RFC3339)
    startAt = Math.floor(new Date(raw.connected_at).getTime() / 1000);
  } else if (raw.connectedAt) {
    // Capacitor K2Plugin legacy (camelCase RFC3339)
    startAt = Math.floor(new Date(raw.connectedAt).getTime() / 1000);
  }

  return {
    state,
    running,
    networkAvailable: true,
    startAt,
    error,
    retrying,
    slots: raw.slots,
  };
}
