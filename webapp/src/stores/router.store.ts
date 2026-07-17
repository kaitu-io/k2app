/**
 * router.store — 路由器控制状态（主语=路由器,与 connection.store 的本机状态
 * 严格分离,互不渗透）。四态:none(从未见过)/unconfigured(待首配)/online/offline。
 * 轮询:Router tab 可见时 2s 拉 /api/core status(不用 SSE——CapacitorHttp 不支持流式)。
 */
import { create } from 'zustand';
import {
  probeRouter,
  routerCore,
  getControlKey,
  saveLastRouter,
  clearLastRouter,
  type RouterInfo,
} from '../services/router-service';
import { mintGatewayCredential } from '../services/private-node-service';

export type RouterPhase = 'none' | 'unconfigured' | 'online' | 'offline';

export interface RouterStatus {
  state: string;
  [k: string]: unknown;
}

interface RouterState {
  phase: RouterPhase;
  router: RouterInfo | null;
  status: RouterStatus | null;
  discovering: boolean;
  setupError: string | null;
}

interface RouterActions {
  runDiscovery: () => Promise<void>;
  startPolling: () => void;
  stopPolling: () => void;
  connectRouter: () => Promise<boolean>;
  disconnectRouter: () => Promise<boolean>;
  setupRouter: () => Promise<boolean>;
  unbindRouter: () => Promise<boolean>;
}

const POLL_INTERVAL = 2000;
let pollTimer: ReturnType<typeof setInterval> | null = null;

export const useRouterStore = create<RouterState & RouterActions>()((set, get) => ({
  phase: 'none',
  router: null,
  status: null,
  discovering: false,
  setupError: null,

  runDiscovery: async () => {
    if (get().discovering) return;
    set({ discovering: true });
    try {
      const info = await probeRouter();
      if (!info) {
        // 曾配对(router 非空)→ offline 置灰;从未见过 → none 不出 tab
        set({ phase: get().router ? 'offline' : 'none', status: null });
        return;
      }
      await saveLastRouter(info);
      set({ router: info, phase: info.configured ? 'online' : 'unconfigured' });
    } finally {
      set({ discovering: false });
    }
  },

  startPolling: () => {
    if (pollTimer) return;
    const poll = async () => {
      if (!get().router || get().phase === 'unconfigured') return;
      try {
        const resp = await routerCore<RouterStatus>('status');
        // unbindRouter() may have completed while this request was in flight —
        // re-check before writing, or a stale response resurrects a cleared router.
        if (!get().router) return;
        if (resp.code === 0 && resp.data) {
          set({ status: resp.data, phase: 'online' });
        } else if (resp.code === 401) {
          set({ status: null });
        }
      } catch {
        if (!get().router) return;
        set({ phase: 'offline', status: null });
      }
    };
    void poll();
    pollTimer = setInterval(() => void poll(), POLL_INTERVAL);
  },

  stopPolling: () => {
    if (pollTimer) {
      clearInterval(pollTimer);
      pollTimer = null;
    }
  },

  connectRouter: async () => {
    if (!get().router) return false;
    const resp = await routerCore('up');
    return resp.code === 0;
  },

  disconnectRouter: async () => {
    if (!get().router) return false;
    const resp = await routerCore('down');
    return resp.code === 0;
  },

  setupRouter: async () => {
    const r = get().router;
    if (!r) return false;
    set({ setupError: null });
    const url = await mintGatewayCredential();
    if (!url) {
      set({ setupError: 'mint_failed' });
      return false;
    }
    const key = await getControlKey();
    if (!key) {
      set({ setupError: 'key_failed' });
      return false;
    }
    const resp = await routerCore('set-credential', { url, controlKey: key });
    if (resp.code !== 0) {
      set({ setupError: 'push_failed' });
      return false;
    }
    set({ router: { ...r, configured: true }, phase: 'online' });
    return true;
  },

  unbindRouter: async () => {
    if (!get().router) return false;
    const resp = await routerCore('reset', undefined);
    if (resp.code !== 0) return false;
    get().stopPolling();
    await clearLastRouter();
    set({ phase: 'none', router: null, status: null });
    return true;
  },
}));

/** Dashboard 横幅/互斥提醒的统一判据:本机流量正被该路由器接管。
 * 锚点可达(phase online)本身即证明 k2r 在本机转发路径上(spec §3.4),无需网关判定。 */
export function isRouterTakeover(s: Pick<RouterState, 'phase' | 'status'>): boolean {
  return s.phase === 'online' && s.status?.state === 'connected';
}
