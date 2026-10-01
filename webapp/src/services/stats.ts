/**
 * Usage Analytics — Event queue with persistent storage and batch upload.
 *
 * Events queued in _platform.storage under key 'stats_queue'.
 * On each trigger (app_open, connect, disconnect), queue is flushed
 * to POST /api/stats/events. On failure, events stay in queue.
 */

import { cloudApi } from './cloud-api';
import { getDeviceUdid } from './device-udid';
import { randomUUID } from '../utils/uuid';
import type { AppFunnelEvent, FunnelProps } from './funnel-events';

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

/** Behavior-only funnel event. `eid` is the server-side idempotency key and is
 * generated at enqueue time so a retried flush resends the same eid. */
interface FunnelQueued {
  eid: string;
  device_hash: string;
  os: string;
  app_version: string;
  event: AppFunnelEvent;
  plan?: string;
  source?: string;
  channel?: string;
  created_at: string;
}

interface StatsQueue {
  app_opens: AppOpenEvent[];
  connections: ConnectionEvent[];
  funnel: FunnelQueued[];
}

const STORAGE_KEY = 'stats_queue';

// ========================= Queue Management =========================

async function getQueue(): Promise<StatsQueue> {
  try {
    const stored = await window._platform?.storage?.get<StatsQueue>(STORAGE_KEY);
    if (stored) {
      // Older persisted queues predate `funnel` (and could lack other arrays).
      return {
        app_opens: stored.app_opens ?? [],
        connections: stored.connections ?? [],
        funnel: stored.funnel ?? [],
      };
    }
  } catch {
    // Corrupted data, start fresh
  }
  return { app_opens: [], connections: [], funnel: [] };
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
    _deviceHash = await getDeviceUdid();
    return _deviceHash;
  } catch {
    return 'unknown';
  }
}

// ========================= Flush =========================

let _flushing = false;

async function flush(): Promise<void> {
  if (_flushing) return;
  _flushing = true;

  try {
    // Snapshot what we send. Events appended while the request is in flight
    // are not in the snapshot and must survive a successful flush.
    const queue = await getQueue();
    const nOpens = queue.app_opens.length;
    const nConns = queue.connections.length;
    const nFunnel = queue.funnel.length;
    const total = nOpens + nConns + nFunnel;
    if (total === 0) return;

    const resp = await cloudApi.request('POST', '/api/stats/events', {
      app_opens: queue.app_opens.slice(0, nOpens),
      connections: queue.connections.slice(0, nConns),
      funnel: queue.funnel.slice(0, nFunnel),
    });

    if (resp.code === 0) {
      // Re-read: drop only the sent prefix of each array.
      const latest = await getQueue();
      const rest: StatsQueue = {
        app_opens: latest.app_opens.slice(nOpens),
        connections: latest.connections.slice(nConns),
        funnel: latest.funnel.slice(nFunnel),
      };
      if (rest.app_opens.length + rest.connections.length + rest.funnel.length > 0) {
        await saveQueue(rest);
      } else {
        await clearQueue();
      }
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

  /** Record a behavior funnel event and flush queue. Never throws. */
  async trackFunnel(event: AppFunnelEvent, props?: FunnelProps): Promise<void> {
    try {
      await enqueueFunnel(event, props);
    } catch (err) {
      console.warn('[Stats] trackFunnel failed:', err);
    }
  },

  /** Like trackFunnel, but at most once per install (flag set after enqueue succeeds). */
  async trackFunnelOnce(event: AppFunnelEvent, props?: FunnelProps): Promise<void> {
    try {
      const key = `funnel_once:${event}`;
      if (await window._platform?.storage?.get(key)) return;
      await enqueueFunnel(event, props);
      await window._platform?.storage?.set(key, true);
    } catch (err) {
      console.warn('[Stats] trackFunnelOnce failed:', err);
    }
  },
};

async function enqueueFunnel(event: AppFunnelEvent, props?: FunnelProps): Promise<void> {
  const deviceHash = await getDeviceHash();
  const { os, app_version } = getPlatformInfo();
  const item: FunnelQueued = {
    eid: randomUUID(),
    device_hash: deviceHash,
    os,
    app_version,
    event,
    created_at: new Date().toISOString(),
  };
  if (props?.plan) item.plan = props.plan;
  if (props?.source) item.source = props.source;
  if (props?.channel) item.channel = props.channel;

  const queue = await getQueue();
  queue.funnel.push(item);
  await saveQueue(queue);
  flush(); // fire-and-forget
}
