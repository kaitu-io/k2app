/**
 * Usage Analytics — Event queue with persistent storage and batch upload.
 *
 * Events queued in _platform.storage under key 'stats_queue'.
 * On each trigger (app_open, connect, disconnect), queue is flushed
 * to POST /api/stats/events. On failure, events stay in queue.
 */

import { cloudApi } from './cloud-api';

// ========================= Types =========================

interface AppOpenEvent {
  device_hash: string;
  os: string;
  app_version: string;
  locale: string;
  created_at: string;
}

interface ConnectionEvent {
  device_hash: string;
  os: string;
  app_version: string;
  event: 'connect' | 'disconnect';
  node_type: 'cloud' | 'self-hosted';
  node_ipv4: string;
  node_region: string;
  rule_mode: string;
  duration_sec: number;
  disconnect_reason: string;
  created_at: string;
}

interface StatsQueue {
  app_opens: AppOpenEvent[];
  connections: ConnectionEvent[];
}

const STORAGE_KEY = 'stats_queue';

// ========================= Queue Management =========================

async function getQueue(): Promise<StatsQueue> {
  try {
    const stored = await window._platform?.storage?.get<StatsQueue>(STORAGE_KEY);
    if (stored) return stored;
  } catch {
    // Corrupted data, start fresh
  }
  return { app_opens: [], connections: [] };
}

async function saveQueue(queue: StatsQueue): Promise<void> {
  try {
    await window._platform?.storage?.set(STORAGE_KEY, queue);
  } catch (err) {
    console.warn('[Stats] Failed to save queue:', err);
  }
}

async function clearQueue(): Promise<void> {
  try {
    await window._platform?.storage?.remove(STORAGE_KEY);
  } catch {
    // ignore
  }
}

// ========================= Device Hash =========================

let _deviceHash: string | null = null;

async function getDeviceHash(): Promise<string> {
  if (_deviceHash) return _deviceHash;
  try {
    const udid = await window._platform?.getUdid();
    if (udid) {
      _deviceHash = await sha256(udid);
      return _deviceHash;
    }
  } catch {
    // getUdid failed (e.g. Windows daemon not responding)
  }

  // Fallback: generate a persistent random ID so each device is still unique
  const FALLBACK_KEY = 'stats_device_id';
  try {
    let fallbackId = await window._platform?.storage?.get<string>(FALLBACK_KEY);
    if (!fallbackId) {
      fallbackId = crypto.randomUUID();
      await window._platform?.storage?.set(FALLBACK_KEY, fallbackId);
    }
    _deviceHash = await sha256(fallbackId);
    return _deviceHash;
  } catch {
    // storage also failed
  }
  return 'unknown';
}

async function sha256(input: string): Promise<string> {
  const data = new TextEncoder().encode(input);
  const hashBuffer = await crypto.subtle.digest('SHA-256', data);
  const hashArray = Array.from(new Uint8Array(hashBuffer));
  return hashArray.map(b => b.toString(16).padStart(2, '0')).join('');
}

// ========================= Flush =========================

let _flushing = false;

async function flush(): Promise<void> {
  if (_flushing) return;
  _flushing = true;

  try {
    const queue = await getQueue();
    const total = queue.app_opens.length + queue.connections.length;
    if (total === 0) return;

    const resp = await cloudApi.request('POST', '/api/stats/events', {
      app_opens: queue.app_opens,
      connections: queue.connections,
    });

    if (resp.code === 0) {
      await clearQueue();
      console.debug(`[Stats] Flushed ${total} events`);
    } else {
      console.warn('[Stats] Flush failed, will retry:', resp.code);
    }
  } catch (err) {
    console.warn('[Stats] Flush error, will retry:', err);
  } finally {
    _flushing = false;
  }
}

// ========================= Public API =========================

function getPlatformInfo() {
  const p = window._platform;
  return {
    os: p?.os || 'unknown',
    app_version: p?.version || '0.0.0',
  };
}

export const statsService = {
  /** Record app open and flush queue */
  async trackAppOpen(): Promise<void> {
    try {
      const deviceHash = await getDeviceHash();
      const { os, app_version } = getPlatformInfo();
      const locale = document.documentElement.lang || 'unknown';

      const queue = await getQueue();
      queue.app_opens.push({
        device_hash: deviceHash,
        os,
        app_version,
        locale,
        created_at: new Date().toISOString(),
      });
      await saveQueue(queue);
      flush(); // fire-and-forget
    } catch (err) {
      console.warn('[Stats] trackAppOpen failed:', err);
    }
  },

  /** Record VPN connect and flush queue */
  async trackConnect(params: {
    nodeType: 'cloud' | 'self-hosted';
    nodeIpv4: string;
    nodeRegion: string;
    ruleMode: string;
  }): Promise<void> {
    try {
      const deviceHash = await getDeviceHash();
      const { os, app_version } = getPlatformInfo();

      const queue = await getQueue();
      queue.connections.push({
        device_hash: deviceHash,
        os,
        app_version,
        event: 'connect',
        node_type: params.nodeType,
        node_ipv4: params.nodeType === 'cloud' ? params.nodeIpv4 : '',
        node_region: params.nodeType === 'cloud' ? params.nodeRegion : '',
        rule_mode: params.ruleMode,
        duration_sec: 0,
        disconnect_reason: '',
        created_at: new Date().toISOString(),
      });
      await saveQueue(queue);
      flush(); // fire-and-forget
    } catch (err) {
      console.warn('[Stats] trackConnect failed:', err);
    }
  },

  /** Record VPN disconnect and flush queue */
  async trackDisconnect(params: {
    nodeType: 'cloud' | 'self-hosted';
    nodeIpv4: string;
    nodeRegion: string;
    ruleMode: string;
    durationSec: number;
    reason: 'user' | 'error' | 'network';
  }): Promise<void> {
    try {
      const deviceHash = await getDeviceHash();
      const { os, app_version } = getPlatformInfo();

      const queue = await getQueue();
      queue.connections.push({
        device_hash: deviceHash,
        os,
        app_version,
        event: 'disconnect',
        node_type: params.nodeType,
        node_ipv4: params.nodeType === 'cloud' ? params.nodeIpv4 : '',
        node_region: params.nodeType === 'cloud' ? params.nodeRegion : '',
        rule_mode: params.ruleMode,
        duration_sec: params.durationSec,
        disconnect_reason: params.reason,
        created_at: new Date().toISOString(),
      });
      await saveQueue(queue);
      flush(); // fire-and-forget
    } catch (err) {
      console.warn('[Stats] trackDisconnect failed:', err);
    }
  },
};
