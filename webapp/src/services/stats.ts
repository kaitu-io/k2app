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

/** Returns true only if the queue was actually persisted. */
async function saveQueue(queue: StatsQueue): Promise<boolean> {
  const storage = window._platform?.storage;
  if (!storage) return false;
  try {
    await storage.set(STORAGE_KEY, queue);
    return true;
  } catch (err) {
    console.warn('[Stats] Failed to save queue:', err);
    return false;
  }
}

// Storage calls are async, so every read-modify-write of the stored queue
// (enqueue from any track* method, and the post-flush trim) goes through this
// promise-chain mutex. Never held across the network call.
let _queueLock: Promise<unknown> = Promise.resolve();

function withQueueLock<T>(fn: () => Promise<T>): Promise<T> {
  const run = _queueLock.then(fn);
  _queueLock = run.catch(() => undefined);
  return run;
}

/** Server-side limit: one request may carry at most this many items in total. */
const MAX_BATCH = 100;
/** Stored-queue cap (all three arrays together). Oldest items are dropped beyond it. */
const MAX_QUEUE = 500;

type QueueKey = keyof StatsQueue;
/** Fixed batching order; within an array, oldest first. */
const QUEUE_KEYS: readonly QueueKey[] = ['app_opens', 'connections', 'funnel'];

function queueSize(queue: StatsQueue): number {
  return queue.app_opens.length + queue.connections.length + queue.funnel.length;
}

/**
 * Per-array count of items of the request currently in flight that are still at
 * the head of the stored queue. The post-flush trim drops exactly these. The
 * cap below evicts from the head too, so it decrements the count — otherwise
 * the trim would also drop items that were never sent.
 */
let _inFlight: Record<QueueKey, number> | null = null;

/** Drops the globally oldest items until the queue fits MAX_QUEUE. */
function enforceCap(queue: StatsQueue): void {
  let over = queueSize(queue) - MAX_QUEUE;
  while (over > 0) {
    let oldest: QueueKey | null = null;
    for (const key of QUEUE_KEYS) {
      const head = queue[key][0];
      if (!head) continue;
      // ISO-8601 UTC timestamps order lexicographically.
      if (oldest === null || head.created_at < queue[oldest][0].created_at) oldest = key;
    }
    if (oldest === null) return;
    queue[oldest].shift();
    if (_inFlight && _inFlight[oldest] > 0) _inFlight[oldest]--;
    over--;
  }
}

/** Serialized read-modify-write append. Resolves true iff the queue was persisted. */
function updateQueue(mutate: (queue: StatsQueue) => void): Promise<boolean> {
  return withQueueLock(async () => {
    const queue = await getQueue();
    mutate(queue);
    enforceCap(queue);
    return saveQueue(queue);
  });
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
let _flushRequested = false;

/** A full queue (MAX_QUEUE) drains in MAX_QUEUE / MAX_BATCH requests; leave headroom. */
const MAX_BATCHES_PER_FLUSH = 10;

/**
 * Sends one batch of at most MAX_BATCH items and trims exactly what was sent.
 * Resolves true iff a batch was sent successfully AND items remain.
 */
async function flushBatch(): Promise<boolean> {
  // Snapshot what we send. Events appended while the request is in flight
  // are not in the snapshot and must survive a successful flush.
  const queue = await withQueueLock(getQueue);
  let budget = MAX_BATCH;
  const take = (key: QueueKey): number => {
    const n = Math.min(queue[key].length, budget);
    budget -= n;
    return n;
  };
  const sent: Record<QueueKey, number> = {
    app_opens: take('app_opens'),
    connections: take('connections'),
    funnel: take('funnel'),
  };
  const total = MAX_BATCH - budget;
  if (total === 0) return false;

  _inFlight = sent;
  try {
    // The lock is NOT held across the network call.
    const resp = await cloudApi.request('POST', '/api/stats/events', {
      app_opens: queue.app_opens.slice(0, sent.app_opens),
      connections: queue.connections.slice(0, sent.connections),
      funnel: queue.funnel.slice(0, sent.funnel),
    });

    if (resp.code !== 0) {
      console.warn('[Stats] Flush failed, will retry:', resp.code);
      return false;
    }

    // Re-read under the lock: drop only the sent prefix of each array
    // (`sent` is live — the cap may have evicted part of that prefix already).
    const remaining = await withQueueLock(async () => {
      const latest = await getQueue();
      const rest: StatsQueue = {
        app_opens: latest.app_opens.slice(sent.app_opens),
        connections: latest.connections.slice(sent.connections),
        funnel: latest.funnel.slice(sent.funnel),
      };
      _inFlight = null;
      const left = queueSize(rest);
      if (left > 0) {
        await saveQueue(rest);
      } else {
        await clearQueue();
      }
      return left;
    });
    console.debug(`[Stats] Flushed ${total} events`);
    return remaining > 0;
  } finally {
    _inFlight = null;
  }
}

async function flush(): Promise<void> {
  if (_flushing) {
    // Don't drop the request: an event queued right before the app hands off
    // to an external browser must not wait for the next trigger.
    _flushRequested = true;
    return;
  }
  _flushing = true;

  try {
    // A flush requested while one was in flight gets exactly one follow-up
    // pass (which is a no-op request-wise when the queue is already empty).
    do {
      _flushRequested = false;
      try {
        // Drain in batches; stop on the first failure or when empty.
        for (let i = 0; i < MAX_BATCHES_PER_FLUSH; i++) {
          if (!(await flushBatch())) break;
        }
      } catch (err) {
        console.warn('[Stats] Flush error, will retry:', err);
      }
    } while (_flushRequested);
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

      await updateQueue((queue) => {
        queue.app_opens.push({
          device_hash: deviceHash,
          os,
          app_version,
          locale,
          created_at: new Date().toISOString(),
        });
      });
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

      await updateQueue((queue) => {
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
      });
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

      await updateQueue((queue) => {
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
      });
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

  /** Like trackFunnel, but at most once per install (flag set only after the queue write succeeded). */
  async trackFunnelOnce(event: AppFunnelEvent, props?: FunnelProps): Promise<void> {
    if (_onceInProgress.has(event)) return;
    _onceInProgress.add(event);
    try {
      const key = `funnel_once:${event}`;
      if (await window._platform?.storage?.get(key)) return;
      if (await enqueueFunnel(event, props)) {
        await window._platform?.storage?.set(key, true);
      }
    } catch (err) {
      console.warn('[Stats] trackFunnelOnce failed:', err);
    } finally {
      _onceInProgress.delete(event);
    }
  },

  /**
   * Like trackFunnel, but at most once per UTC day per install. Storage key
   * `funnel_day:<event>` holds the UTC date of the last enqueue and is written
   * only after the queue write succeeded.
   */
  async trackFunnelDaily(event: AppFunnelEvent, props?: FunnelProps): Promise<void> {
    if (_dailyInProgress.has(event)) return;
    _dailyInProgress.add(event);
    try {
      const key = `funnel_day:${event}`;
      const today = new Date().toISOString().slice(0, 10);
      if ((await window._platform?.storage?.get<string>(key)) === today) return;
      if (await enqueueFunnel(event, props)) {
        await window._platform?.storage?.set(key, today);
      }
    } catch (err) {
      console.warn('[Stats] trackFunnelDaily failed:', err);
    } finally {
      _dailyInProgress.delete(event);
    }
  },
};

const _onceInProgress = new Set<string>();
const _dailyInProgress = new Set<string>();

// ========================= Existing-install seeding =========================

/** Events reported at most once per install (storage flag `funnel_once:<event>`). */
const ONCE_EVENTS: readonly AppFunnelEvent[] = [
  'app_first_open',
  'first_connect_attempt',
  'first_connect_ok',
];
const DEVICE_UDID_KEY = 'device-udid'; // owned by services/device-udid.ts
const SEED_CHECKED_KEY = 'funnel_once:_seed_checked';

/**
 * Installs that predate the funnel have no `funnel_once:*` flags, so without
 * this they would all report `app_first_open` (and the first-connect pair) on
 * the first launch after the web OTA and read as brand-new installs.
 *
 * An install is "existing" iff `device-udid` is already in storage — a fresh
 * install only gets that key from the first getDeviceUdid() call, so this MUST
 * run before anything can call it (main.tsx, right after bridge injection).
 * For an existing install the once-flags are written WITHOUT enqueueing events.
 *
 * The decision is taken once per install (`_seed_checked` marker): a fresh
 * install whose first `app_first_open` enqueue failed still has no once-flag on
 * the next launch but does have a `device-udid` by then, and must not be
 * mistaken for an existing install.
 *
 * Never throws; storage unavailable → no-op (retried next launch).
 */
export async function seedFunnelOnceFlagsForExistingInstall(): Promise<void> {
  try {
    const storage = window._platform?.storage;
    if (!storage) return;
    if (await storage.get(SEED_CHECKED_KEY)) return;
    const existing = !!(await storage.get(DEVICE_UDID_KEY));
    if (existing && !(await storage.get(`funnel_once:${ONCE_EVENTS[0]}`))) {
      for (const event of ONCE_EVENTS) {
        await storage.set(`funnel_once:${event}`, true);
      }
    }
    // Written last: a failed flag write above is retried on the next launch.
    await storage.set(SEED_CHECKED_KEY, true);
  } catch (err) {
    console.warn('[Stats] seedFunnelOnceFlags failed:', err);
  }
}

/** Resolves true iff the event was persisted to the queue. */
async function enqueueFunnel(event: AppFunnelEvent, props?: FunnelProps): Promise<boolean> {
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

  const ok = await updateQueue((queue) => {
    queue.funnel.push(item);
  });
  flush(); // fire-and-forget
  return ok;
}
