/**
 * Gateway K2 Bridge
 *
 * Platform bridge for k2r gateway mode. Detected via window.__K2_GATEWAY__
 * injected by the Go gateway's HTML serving.
 *
 * VPN control: HTTP POST to /api/core (same protocol as daemon)
 * Events: SSE from /api/events (status + stats)
 * Storage: Server-side encrypted via /api/storage
 */

import type { IK2Vpn, IPlatform, SResponse } from '../types/kaitu-core';
import type { StatusResponseData } from './vpn-types';
import { transformStatus } from './status-transform';
import { gatewayStorage } from './gateway-storage';
import { webPlatform } from './web-platform';

const CORE_ENDPOINT = '/api/core';

async function coreExec<T = any>(action: string, params?: any): Promise<SResponse<T>> {
  try {
    const response = await fetch(CORE_ENDPOINT, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ action, params: params ?? {} }),
    });
    if (!response.ok) {
      return { code: -1, message: 'Service error' };
    }
    return await response.json();
  } catch {
    return { code: -1, message: 'Service unavailable' };
  }
}

function connectSSE(
  onStatus: ((status: StatusResponseData) => void) | null,
  onServiceState: ((available: boolean) => void) | null,
): () => void {
  let es: EventSource | null = null;
  let closed = false;

  function connect() {
    if (closed) return;
    es = new EventSource('/api/events');

    es.onopen = () => {
      onServiceState?.(true);
    };

    es.addEventListener('status', (e: MessageEvent) => {
      try {
        const raw = JSON.parse(e.data);
        onStatus?.(transformStatus(raw));
      } catch { /* ignore parse errors */ }
    });

    es.onerror = () => {
      onServiceState?.(false);
      es?.close();
      if (!closed) {
        setTimeout(connect, 3000);
      }
    };
  }

  connect();

  return () => {
    closed = true;
    es?.close();
  };
}

const gatewayK2: IK2Vpn = {
  run: coreExec,

  onServiceStateChange: (callback: (available: boolean) => void): (() => void) => {
    return connectSSE(null, callback);
  },

  onStatusChange: (callback: (status: StatusResponseData) => void): (() => void) => {
    return connectSSE(callback, null);
  },
};

const gwInfo = () => window.__K2_GATEWAY__ ?? { version: 'unknown', commit: '', arch: '' };

const gatewayPlatform: IPlatform = {
  ...webPlatform,
  os: 'linux',
  platformType: 'gateway',
  version: gwInfo().version,
  arch: gwInfo().arch,
  commit: gwInfo().commit,
  storage: gatewayStorage,

  setLogLevel: (level: string): void => {
    fetch('/api/log-level', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ level }),
    }).catch(() => {});
  },

  setDevEnabled: () => {},
};

export async function injectGatewayGlobals(): Promise<void> {
  (window as any)._k2 = gatewayK2;
  (window as any)._platform = gatewayPlatform;
  console.info(`[K2:Gateway] Injected - version=${gatewayPlatform.version}, arch=${gatewayPlatform.arch}`);
}
