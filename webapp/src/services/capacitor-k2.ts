/**
 * Capacitor Mobile Bridge
 *
 * Injects window._k2 (VPN control via K2Plugin) and window._platform (mobile capabilities)
 * when running inside a Capacitor native app (iOS/Android).
 *
 * Detection: Capacitor.isNativePlatform() returns true in native context.
 */

declare const __K2_BUILD_COMMIT__: string;

import { Capacitor, CapacitorHttp } from '@capacitor/core';
import { Clipboard } from '@capacitor/clipboard';
import { Share } from '@capacitor/share';
import { getDeviceUdid } from './device-udid';
import { K2Plugin } from 'k2-plugin';
import type { IK2Vpn, IPlatform, IUpdater, UpdateInfo, SResponse, InstalledApp, IIap, RouterRequestOptions, RouterResponse } from '../types/kaitu-core';
import type { StatusResponseData } from './vpn-types';
import { transformStatus } from './status-transform';
import { createCapacitorStorage } from './capacitor-storage';
import { mapInstalledApp, type AndroidInstalledApp } from './capacitor-app-map';

/**
 * Check if running inside a Capacitor native environment.
 */
export function isCapacitorNative(): boolean {
  return Capacitor.isNativePlatform();
}

/**
 * IPv4 literal + RFC1918-private-or-loopback check. Mirrors the desktop bridge's
 * `is_private_host` gate (desktop/src-tauri/src/router_bridge.rs) exactly:
 * 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16, 127.0.0.0/8. Hostnames and IPv6
 * literals are rejected (same as the Rust side, which only matches `Host::Ipv4`).
 */
function isPrivateIPv4Literal(hostname: string): boolean {
  const m = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/.exec(hostname);
  if (!m) return false;
  const octets = m.slice(1, 5).map(Number);
  if (octets.some((n) => n > 255)) return false;
  const [a, b] = octets;
  if (a === 10) return true; // 10.0.0.0/8
  if (a === 172 && b >= 16 && b <= 31) return true; // 172.16.0.0/12
  if (a === 192 && b === 168) return true; // 192.168.0.0/16
  if (a === 127) return true; // 127.0.0.0/8 loopback
  return false;
}

/**
 * TS-side SSRF gate for routerRequest — the mobile twin of the Rust
 * `router_http_request` guard, since CapacitorHttp has no native URL allowlist.
 * Must run BEFORE issuing the request: only http:// to a private/loopback IPv4
 * literal host. Throws (not fail-soft) so a bad target surfaces immediately
 * instead of silently no-opping.
 */
function assertRouterUrlAllowed(rawUrl: string): void {
  let parsed: URL;
  try {
    parsed = new URL(rawUrl);
  } catch {
    throw new Error('routerRequest: invalid URL');
  }
  if (parsed.protocol !== 'http:' || !isPrivateIPv4Literal(parsed.hostname)) {
    throw new Error('routerRequest: only http:// to private IPv4 allowed');
  }
}

/**
 * Build the iOS StoreKit IAP bridge over K2Plugin. Native returns raw
 * transactionIds; the verify→finish orchestration lives in the webapp
 * (useIapPurchase) where the auth'd cloudApi call belongs — the bridge stays
 * a thin primitive layer per the constitutional bridge-boundary rule.
 */
function buildIapBridge(): IIap {
  return {
    getProducts: async (productIds: string[]) => {
      const res = await K2Plugin.iapGetProducts({ productIds });
      return res.products;
    },
    purchase: async (productId: string, accountToken: string) => {
      return await K2Plugin.iapPurchase({ productId, accountToken });
    },
    restore: async () => {
      const res = await K2Plugin.iapRestore();
      return res.transactions;
    },
    finishTransaction: async (transactionId: string) => {
      await K2Plugin.iapFinishTransaction({ transactionId });
    },
    onTransactionUpdate: (cb) => {
      const handle = K2Plugin.addListener('iapTransactionUpdate', (data: { transactionId: string; productId: string }) => {
        cb(data);
      });
      return () => { handle.then(h => h.remove()); };
    },
  };
}

/**
 * Module-level capacitor run dispatcher.
 * Extracted from injectCapacitorGlobals so it can be tested directly
 * without going through the injected window._k2 global.
 */
export async function capacitorRun<T = any>(action: string, params?: any): Promise<SResponse<T>> {
  try {
    switch (action) {
      case 'status': {
        const raw = await K2Plugin.getStatus();
        const data = transformStatus(raw);
        return { code: 0, message: 'ok', data: data as unknown as T };
      }

      case 'up': {
        if (!params || !params.config) {
          return { code: -1, message: 'Config is required for connect' };
        }
        try {
          await K2Plugin.connect({
            config: JSON.stringify(params.config),
            alwaysOn: params.alwaysOn === true,
          });
          return { code: 0, message: 'ok' };
        } catch (connectErr) {
          const msg = connectErr instanceof Error ? connectErr.message : String(connectErr);
          // Detect VPN permission denial from native plugin rejection
          const isPermissionDenied = /permission|denied|revoked|not granted/i.test(msg);
          const code = isPermissionDenied ? 580 : -1;
          console.warn('[K2:Capacitor] connect rejected:', code, msg);
          return { code, message: msg };
        }
      }

      case 'down': {
        await K2Plugin.disconnect();
        return { code: 0, message: 'ok' };
      }

      case 'version': {
        const versionInfo = await K2Plugin.getVersion();
        return { code: 0, message: 'ok', data: versionInfo as unknown as T };
      }

      case 'classify-apps': {
        // App Bypass region-default badges. Forward {region, installed} to
        // native (which runs the same krs.MatchInstalled codepath as the
        // engine). installed is JSON-stringified for the gomobile boundary.
        const region: string = params?.region ?? '';
        const installed = Array.isArray(params?.installed) ? params.installed : [];
        const res = await K2Plugin.classifyApps({
          region,
          installed: JSON.stringify(installed),
        });
        return { code: 0, message: 'ok', data: res as unknown as T };
      }

      case 'relay-fetch': {
        // Antiblock control-plane relay through a camouflage node. The Go core
        // (wire.RelayFetchJSON) is gomobile-exported as appext.RelayFetch and
        // runs in-process — a single VPN-independent outbound. The boundary is
        // string-in/string-out, so we serialize the RelayRequest and parse the
        // {code,message,data} envelope back verbatim (identical to the daemon).
        // If the native method is absent (older build), the catch below returns
        // code:-1 so the webapp learns relay is unsupported and uses direct.
        const res = await K2Plugin.relayFetch({ request: JSON.stringify(params ?? {}) });
        return JSON.parse(res.response) as SResponse<T>;
      }

      case 'relay-add-nodes': {
        // Feed camouflage-node descriptors to the Go RelayManager (incremental,
        // deduped by IP in Go). Go owns node storage/ranking/health — the webapp
        // only forwards what it discovers (embedded seed + /api/tunnels). The
        // nodes array is JSON-stringified for the gomobile string boundary.
        const nodes = Array.isArray(params?.nodes) ? params.nodes : [];
        const res = await K2Plugin.relayAddNodes({ nodes: JSON.stringify(nodes) });
        return JSON.parse(res.response) as SResponse<T>;
      }

      default:
        return { code: -1, message: `Unknown action: ${action}` };
    }
  } catch (error) {
    return {
      code: -1,
      message: error instanceof Error ? error.message : String(error),
    };
  }
}

/** Test alias — lets unit tests call the switch directly without injecting globals. */
export const __testCapacitorRun = capacitorRun;

/**
 * Inject Capacitor-specific _k2 and _platform globals.
 * Must be called before store initialization.
 */
export async function injectCapacitorGlobals(): Promise<void> {
  // Get app version from K2Plugin
  let appVersion = 'unknown';
  try {
    const readyResult = await K2Plugin.checkReady();
    if (readyResult?.version) {
      appVersion = readyResult.version;
    }
  } catch (err) {
    console.warn('[K2:Capacitor] checkReady failed, using fallback version:', err);
  }

  const platform = Capacitor.getPlatform() as IPlatform['os'];

  // Build _k2: VPN control via K2Plugin
  const capacitorK2: IK2Vpn = {
    run: capacitorRun,

    // Event-driven mode: wire native K2Plugin events to VPN machine store.
    // This prevents the 2s polling fallback which causes stale BACKEND_DISCONNECTED
    // to interrupt the connecting state (flash-to-disconnected race condition).
    onServiceStateChange: (callback: (available: boolean) => void): (() => void) => {
      // K2Plugin is always available in native context
      setTimeout(() => callback(true), 0);
      return () => {};
    },

    onStatusChange: (callback: (status: StatusResponseData) => void): (() => void) => {
      // Wire vpnStateChange native events through the VPN machine's event-driven path
      const handle = K2Plugin.addListener('vpnStateChange', (event: any) => {
        console.debug('[K2:Capacitor] vpnStateChange:', event.state,
          event.connectedAt ? `connectedAt=${event.connectedAt}` : '');
        callback(transformStatus(event));
      });

      // Also wire vpnError events — synthesize error status for the VPN machine
      const errorHandle = K2Plugin.addListener('vpnError', (event: any) => {
        console.warn('[K2:Capacitor] vpnError:', event.code ?? 'no-code', event.message ?? event);
        const errorCode = typeof event.code === 'number' ? event.code : 570;
        callback({
          state: 'error',
          running: false,
          networkAvailable: true,
          error: { code: errorCode, message: event.message ?? String(event) },
          retrying: false,
        });
      });

      return () => {
        handle.then(h => h.remove());
        errorHandle.then(h => h.remove());
      };
    },
  };

  // Build updater: native update support
  let updateReadyCallbacks: ((info: UpdateInfo) => void)[] = [];
  let storedUpdateUrl: string | null = null;

  const updater: IUpdater = {
    isUpdateReady: false,
    updateInfo: null,
    isChecking: false,
    error: null,
    channel: 'stable',
    applyUpdateNow: async () => {
      if (storedUpdateUrl) {
        await K2Plugin.openUrl({ url: storedUpdateUrl });
      }
    },
    onUpdateReady: (callback: (info: UpdateInfo) => void) => {
      updateReadyCallbacks.push(callback);
      return () => {
        updateReadyCallbacks = updateReadyCallbacks.filter(cb => cb !== callback);
      };
    },
  };

  // Android: initialize channel from native + provide setChannel
  if (Capacitor.getPlatform() === 'android') {
    try {
      const channelResult = await K2Plugin.getUpdateChannel();
      updater.channel = channelResult.channel as 'stable' | 'beta';
    } catch {
      // getUpdateChannel not available (old plugin version), default stable
    }
    updater.setChannel = async (channel: 'stable' | 'beta') => {
      await K2Plugin.setUpdateChannel({ channel });
      updater.channel = channel;
      return channel;
    };
  }
  // iOS: no setChannel — beta is API-only subscription

  // Build _platform: mobile capabilities
  const capacitorPlatform: IPlatform = {
    os: platform,
    platformType: 'mobile',
    version: appVersion,
    arch: 'arm64',
    commit: typeof __K2_BUILD_COMMIT__ !== 'undefined' ? __K2_BUILD_COMMIT__ : '',

    storage: createCapacitorStorage(K2Plugin),

    openExternal: async (url: string): Promise<void> => {
      await K2Plugin.openUrl({ url });
    },

    writeClipboard: async (text: string): Promise<void> => {
      await Clipboard.write({ string: text });
    },

    readClipboard: async (): Promise<string> => {
      const result = await Clipboard.read();
      return result.value ?? '';
    },

    share: async (params: { text: string; title?: string; url?: string }): Promise<void> => {
      await Share.share({
        title: params.title,
        text: params.text,
        url: params.url,
        dialogTitle: params.title,
      });
    },

    syncLocale: async (_locale: string): Promise<void> => {
      // No-op on mobile — no tray menu to update
    },

    getDefaultGateway: async (): Promise<string | null> => {
      try {
        const { gateway } = await K2Plugin.getDefaultGateway();
        return gateway ?? null;
      } catch {
        return null;
      }
    },

    // CapacitorHttp 走原生 URLSession/HttpURLConnection——绕过 WebView 的 CORS
    // 与 mixed-content 限制,这是 app 触达 http://LAN 的唯一合规通道。TS 侧
    // assertRouterUrlAllowed 镜像 desktop router_bridge.rs 的 is_private_host
    // 门（仅 http:// + 私网/回环 IPv4 字面量），在发起请求前校验——CapacitorHttp
    // 没有独立的"重定向策略"API,但 HttpOptions.disableRedirects 在 Android
    // (HttpURLConnection#setInstanceFollowRedirects) 与 iOS
    // (URLSessionTaskDelegate willPerformHTTPRedirection) 都原生生效,与 B4
    // desktop 侧 reqwest Policy::none() 语义对齐：校验只覆盖被请求的 URL,不
    // 关闭重定向就会让一个合法私网目标把客户端重定向到任意公网地址,重开
    // 这个模块本该关闭的 SSRF 口子。
    routerRequest: async (opts: RouterRequestOptions): Promise<RouterResponse> => {
      assertRouterUrlAllowed(opts.url);
      const resp = await CapacitorHttp.request({
        url: opts.url,
        method: opts.method ?? 'GET',
        headers: opts.headers ?? {},
        data: opts.body,
        connectTimeout: opts.timeoutMs ?? 5000,
        readTimeout: opts.timeoutMs ?? 5000,
        responseType: 'text',
        disableRedirects: true,
      });
      return {
        status: resp.status,
        body: typeof resp.data === 'string' ? resp.data : JSON.stringify(resp.data ?? ''),
      };
    },

    updater,

    uploadLogs: async (params) => {
      const udid = await getDeviceUdid();
      const result = await K2Plugin.uploadLogs({
        email: params.email ?? undefined,
        reason: params.reason,
        feedbackId: params.feedbackId,
        platform: params.platform,
        version: params.version,
        udid,
      });
      return result;
    },

    setLogLevel: (level: string): void => {
      localStorage.setItem('k2_log_level', level);
      K2Plugin.setLogLevel({ level }).catch(() => {});
    },

    setDevEnabled: (enabled: boolean): void => {
      K2Plugin.setDevEnabled({ enabled }).catch(() => {});
    },

    appList: Capacitor.getPlatform() === 'android' ? {
      listInstalled: async (): Promise<InstalledApp[]> => {
        const res = await K2Plugin.listInstalledApps();
        return res.apps.map((a: AndroidInstalledApp) => mapInstalledApp(a));
      },
    } : undefined,

    // IAP: iOS-only (StoreKit 2). Android/web keep WordGate external-link flow
    // (capability stays undefined → Purchase.tsx falls back automatically).
    iap: platform === 'ios' ? buildIapBridge() : undefined,
  };

  // Inject globals
  (window as any)._k2 = capacitorK2;
  (window as any)._platform = capacitorPlatform;

  // Native event listeners (vpnStateChange, vpnError) are now registered
  // via onStatusChange() above, wired through VPN machine's event-driven mode.
  // This eliminates the 2s polling fallback and prevents stale-status race conditions.

  K2Plugin.addListener('nativeUpdateAvailable', (event: any) => {
    // iOS sends appStoreUrl, Android sends url
    storedUpdateUrl = event.appStoreUrl ?? event.url ?? null;
    const info: UpdateInfo = {
      currentVersion: appVersion,
      newVersion: event.version,
    };
    updater.isUpdateReady = true;
    updater.updateInfo = info;
    updateReadyCallbacks.forEach(cb => cb(info));
  });

  console.info(`[K2:Capacitor] Injected - os=${platform}, version=${appVersion}`);

  // Auto-restore dev mode from previous session
  if (localStorage.getItem('k2_developer_mode') === 'true') {
    K2Plugin.setDevEnabled({ enabled: true }).catch(() => {});
  }

  // Forward WebView console.* to native file logging (webapp.log)
  // Buffers entries and flushes to K2Plugin.appendLogs() on threshold/timer/visibility
  try {
    function formatArgs(args: any[]): string {
      return args.map(a => {
        if (typeof a === 'string') return a;
        if (a instanceof Error) return `${a.message}${a.stack ? '\n' + a.stack : ''}`;
        try { return JSON.stringify(a); } catch { return String(a); }
      }).join(' ');
    }

    let buffer: Array<{ level: string; message: string; timestamp: number }> = [];
    let flushTimer: ReturnType<typeof setTimeout> | null = null;
    const FLUSH_THRESHOLD = 50;
    const FLUSH_INTERVAL = 3000;

    function flushBuffer() {
      if (flushTimer) { clearTimeout(flushTimer); flushTimer = null; }
      if (buffer.length === 0) return;
      const entries = buffer;
      buffer = [];
      K2Plugin.appendLogs?.({ entries })?.catch?.(() => {});
    }

    function scheduleFlush() {
      if (flushTimer) return;
      flushTimer = setTimeout(flushBuffer, FLUSH_INTERVAL);
    }

    function pushEntry(level: string, args: any[]) {
      buffer.push({ level, message: formatArgs(args), timestamp: Date.now() });
      if (buffer.length >= FLUSH_THRESHOLD) {
        flushBuffer();
      } else {
        scheduleFlush();
      }
    }

    const _log = console.log;
    const _debug = console.debug;
    const _info = console.info;
    const _warn = console.warn;
    const _error = console.error;

    console.log = (...args: any[]) => { _log(...args); pushEntry('log', args); };
    console.debug = (...args: any[]) => { _debug(...args); pushEntry('debug', args); };
    console.info = (...args: any[]) => { _info(...args); pushEntry('info', args); };
    console.warn = (...args: any[]) => { _warn(...args); pushEntry('warn', args); flushBuffer(); };
    console.error = (...args: any[]) => { _error(...args); pushEntry('error', args); flushBuffer(); };

    document.addEventListener('visibilitychange', () => {
      if (document.visibilityState === 'hidden') flushBuffer();
    });
  } catch {
    // Console interceptor setup failed, skip
  }
}
