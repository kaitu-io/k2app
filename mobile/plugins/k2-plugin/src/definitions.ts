import type { PluginListenerHandle } from '@capacitor/core';

export interface WebUpdateInfo {
  available: boolean;
  version?: string;
  size?: number;
}

export interface NativeUpdateInfo {
  available: boolean;
  version?: string;
  size?: number;
  url?: string;
}

export interface K2PluginInterface {
  checkReady(): Promise<{ ready: boolean; version?: string; reason?: string }>;
  getVersion(): Promise<{ version: string; go: string; os: string; arch: string }>;
  getStatus(): Promise<{ state: string; connectedAt?: string; uptimeSeconds?: number; error?: string }>;
  getConfig(): Promise<{ config?: string }>;
  connect(options: { config: string; alwaysOn?: boolean }): Promise<void>;
  disconnect(): Promise<void>;

  checkWebUpdate(): Promise<WebUpdateInfo>;
  checkNativeUpdate(): Promise<NativeUpdateInfo>;
  applyWebUpdate(): Promise<void>;
  openUrl(options: { url: string }): Promise<void>;

  appendLogs(options: { entries: Array<{ level: string; message: string; timestamp: number }> }): Promise<void>;
  uploadLogs(options: { email?: string; reason: string; feedbackId?: string; platform?: string; version?: string; udid?: string }): Promise<{ success: boolean; error?: string; s3Keys?: Array<{ name: string; s3Key: string }> }>;

  setLogLevel(options: { level: string }): Promise<void>;
  setDevEnabled(options: { enabled: boolean }): Promise<void>;
  debugDump(): Promise<Record<string, unknown>>;

  getUpdateChannel(): Promise<{ channel: string }>;
  setUpdateChannel(options: { channel: string }): Promise<{ channel: string }>;

  storageGet(options: { key: string }): Promise<{ value: string | null }>;
  storageSet(options: { key: string; value: string }): Promise<void>;
  storageRemove(options: { key: string }): Promise<void>;

  listInstalledApps(): Promise<{ apps: Array<{ packageName: string; label: string; iconUrl?: string; installerPackageName?: string | null }> }>;

  /**
   * Region-default app routing classifier for the App Bypass page. `installed` is
   * a JSON-stringified array of {id,label,installer_package_name,process_names}.
   * Mirrors the desktop daemon's classify-apps action (same krs.MatchInstalled),
   * so badges agree with engine routing.
   */
  classifyApps(options: { region: string; installed: string }): Promise<{
    classifications: Array<{ id: string; default: 'direct' | 'proxy'; hit_kind?: string; hit_pattern?: string }>;
  }>;

  /**
   * Antiblock control-plane relay: send one HTTP request to Center through a
   * camouflage VPN node (TCP→uTLS(ECH+pin)→HTTP/1.1). `request` is a
   * JSON-stringified wire.RelayRequest; `response` is the JSON-stringified
   * {code,message,data} envelope from wire.RelayFetchJSON (the gomobile-exported
   * relay function). String-in/string-out matches the gomobile boundary and
   * is identical to the desktop daemon's relay-fetch action. Runs in-process,
   * VPN-independent (App process on iOS, plugin process on Android).
   */
  relayFetch(options: { request: string }): Promise<{ response: string }>;

  /**
   * Antiblock relay node feed: incrementally register camouflage-node descriptors
   * with the native RelayManager (gomobile RelayAddNodes). `nodes` is a
   * JSON-stringified array of {ip,port?,pin,ech,score?}. Deduped by IP in Go,
   * which owns node storage/ranking/health. `response` is the JSON-stringified
   * {code,message,data:{added,total}} envelope. Idempotent; safe to call often.
   */
  relayAddNodes(options: { nodes: string }): Promise<{ response: string }>;

  /**
   * 物理接口(WiFi/以太网)默认网关 IPv4；排除 VPN TUN。gateway=null 表示不可用。
   */
  getDefaultGateway(): Promise<{ gateway: string | null }>;

  /**
   * StoreKit 2 IAP (iOS only). Android/web reject as unimplemented.
   * Trust model: native NEVER grants entitlement — it returns transactionId,
   * the webapp calls Center verify, then calls iapFinishTransaction.
   */
  iapGetProducts(options: { productIds: string[] }): Promise<{
    products: Array<{
      id: string;
      displayName: string;
      description: string;
      displayPrice: string;
      price: number;
      periodUnit?: 'day' | 'week' | 'month' | 'year' | 'unknown';
      periodValue?: number;
    }>;
  }>;
  iapPurchase(options: { productId: string; accountToken: string }): Promise<{
    result: 'success' | 'cancelled' | 'pending';
    transactionId?: string;
    originalTransactionId?: string;
    productId?: string;
  }>;
  iapRestore(): Promise<{ transactions: Array<{ transactionId: string; productId: string }> }>;
  iapFinishTransaction(options: { transactionId: string }): Promise<void>;

  addListener(eventName: 'vpnStateChange', handler: (data: { state: string }) => void): Promise<PluginListenerHandle>;
  addListener(eventName: 'vpnError', handler: (data: { message: string }) => void): Promise<PluginListenerHandle>;
  addListener(eventName: 'nativeUpdateAvailable', handler: (data: { version: string; url?: string; appStoreUrl?: string }) => void): Promise<PluginListenerHandle>;
  addListener(eventName: 'iapTransactionUpdate', handler: (data: { transactionId: string; productId: string }) => void): Promise<PluginListenerHandle>;
}
