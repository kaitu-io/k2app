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

// Shared SSE connection — single EventSource with fan-out to multiple subscribers.
// Avoids creating 4 parallel SSE connections (vpn-machine + vpn store × 2 callbacks each).
const statusCallbacks = new Set<(status: StatusResponseData) => void>();
const serviceCallbacks = new Set<(available: boolean) => void>();
let sharedES: EventSource | null = null;

function ensureSSE() {
  if (sharedES) return;

  function connect() {
    if (statusCallbacks.size === 0 && serviceCallbacks.size === 0) return;
    sharedES = new EventSource('/api/events');

    sharedES.onopen = () => {
      serviceCallbacks.forEach(cb => cb(true));
    };

    sharedES.addEventListener('status', (e: MessageEvent) => {
      try {
        const raw = JSON.parse(e.data);
        const status = transformStatus(raw);
        statusCallbacks.forEach(cb => cb(status));
      } catch { /* ignore parse errors */ }
    });

    sharedES.onerror = () => {
      serviceCallbacks.forEach(cb => cb(false));
      sharedES?.close();
      sharedES = null;
      setTimeout(connect, 3000);
    };
  }

  connect();
}

function teardownSSE() {
  if (statusCallbacks.size === 0 && serviceCallbacks.size === 0) {
    sharedES?.close();
    sharedES = null;
  }
}

const gatewayK2: IK2Vpn = {
  run: coreExec,

  onServiceStateChange: (callback: (available: boolean) => void): (() => void) => {
    serviceCallbacks.add(callback);
    ensureSSE();
    return () => { serviceCallbacks.delete(callback); teardownSSE(); };
  },

  onStatusChange: (callback: (status: StatusResponseData) => void): (() => void) => {
    statusCallbacks.add(callback);
    ensureSSE();
    return () => { statusCallbacks.delete(callback); teardownSSE(); };
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

  gatewayUpgradeCheck: async () => {
    try {
      const res = await fetch('/api/upgrade', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ action: 'check' }),
      });
      const json = await res.json();
      if (json.code === 0 && json.data) {
        return { current: json.data.current, latest: json.data.latest };
      }
      return null;
    } catch {
      return null;
    }
  },

  gatewayUpgradeApply: async () => {
    try {
      const res = await fetch('/api/upgrade', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ action: 'apply' }),
      });
      const json = await res.json().catch(() => null);
      return json?.code === 0;
    } catch {
      return false;
    }
  },

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
