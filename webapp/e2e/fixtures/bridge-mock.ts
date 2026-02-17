/**
 * Bridge Mock for E2E Tests
 *
 * Provides realistic mock implementations of:
 * - IVpnControl (with state machine)
 * - IPlatformBridge (with real storage/clipboard)
 * - IControlConn (basic connection simulation)
 */

import type { Page } from '@playwright/test';

/**
 * VPN Connection State
 */
export type VpnState = 'disconnected' | 'connecting' | 'connected' | 'disconnecting';

/**
 * Bridge Mock Configuration
 */
export interface BridgeMockConfig {
  /** Platform type */
  platform?: 'desktop' | 'mobile' | 'web';
  /** OS type */
  os?: 'windows' | 'macos' | 'linux' | 'ios' | 'android' | 'web';
  /** Initial VPN state */
  initialVpnState?: VpnState;
  /** App version */
  version?: string;
}

/**
 * Setup Bridge Mock in page context
 */
export async function setupBridgeMock(
  page: Page,
  config: BridgeMockConfig = {}
): Promise<void> {
  const {
    platform = 'desktop',
    os = 'macos',
    initialVpnState = 'disconnected',
    version = '0.3.22',
  } = config;

  await page.addInitScript(
    ({ platform, os, initialVpnState, version }) => {
      // In-memory storage for realistic storage behavior
      const storage = new Map<string, string>();

      // In-memory clipboard
      let clipboard = '';

      // VPN state machine
      let vpnState: VpnState = initialVpnState as VpnState;
      const vpnStateListeners: Array<(state: VpnState) => void> = [];

      // Simulate VPN state transitions
      function transitionVpnState(newState: VpnState, delay = 500) {
        setTimeout(() => {
          vpnState = newState;
          vpnStateListeners.forEach((listener) => listener(newState));

          // Emit control-status-change event
          window.dispatchEvent(
            new CustomEvent('control-status-change', {
              detail: { state: newState },
            })
          );
        }, delay);
      }

      // Mock window.kaitu Bridge
      (window as any).kaitu = {
        // ==================== VPN Control ====================
        vpnControl: {
          async start() {
            if (vpnState !== 'disconnected') {
              throw new Error('VPN is not disconnected');
            }
            vpnState = 'connecting';
            vpnStateListeners.forEach((listener) => listener(vpnState));

            // Simulate connection delay
            transitionVpnState('connected', 800);
          },

          async stop() {
            if (vpnState !== 'connected') {
              throw new Error('VPN is not connected');
            }
            vpnState = 'disconnecting';
            vpnStateListeners.forEach((listener) => listener(vpnState));

            // Simulate disconnection delay
            transitionVpnState('disconnected', 500);
          },

          isSupported() {
            return platform !== 'web';
          },

          // Helper for tests to listen to state changes
          onStateChange(listener: (state: VpnState) => void) {
            vpnStateListeners.push(listener);
            return () => {
              const index = vpnStateListeners.indexOf(listener);
              if (index > -1) vpnStateListeners.splice(index, 1);
            };
          },

          // Helper for tests to get current state
          getState() {
            return vpnState;
          },
        },

        // ==================== Platform Bridge ====================
        platform: {
          getPlatformInfo() {
            return {
              isDesktop: platform === 'desktop',
              isMobile: platform === 'mobile',
              isWeb: platform === 'web',
              os,
              version,
            };
          },

          async getVersion() {
            return version;
          },

          // Logs
          debug(...args: any[]) {
            console.debug('[Bridge Mock]', ...args);
          },
          info(...args: any[]) {
            console.info('[Bridge Mock]', ...args);
          },
          warn(...args: any[]) {
            console.warn('[Bridge Mock]', ...args);
          },
          error(...args: any[]) {
            console.error('[Bridge Mock]', ...args);
          },

          // Clipboard - real in-memory implementation
          async writeText(text: string) {
            clipboard = text;
          },

          async readText() {
            return clipboard;
          },

          // Storage - real in-memory implementation
          storage: {
            async get(key: string) {
              return storage.get(key) || null;
            },

            async set(key: string, value: string) {
              storage.set(key, value);
            },

            async remove(key: string) {
              storage.delete(key);
            },

            async clear() {
              storage.clear();
            },

            async keys() {
              return Array.from(storage.keys());
            },
          },

          // Optional features
          async openExternal(url: string) {
            console.log('[Bridge Mock] openExternal:', url);
          },

          async showToast(message: string, type: string) {
            console.log(`[Bridge Mock] Toast [${type}]:`, message);
          },
        },

        // ==================== Control Connection ====================
        conn: {
          _connected: false,
          _messageHandler: null as ((data: string) => void) | null,

          async open() {
            this._connected = true;
          },

          close() {
            this._connected = false;
          },

          async send(data: string) {
            if (!this._connected) {
              throw new Error('Connection not open');
            }
            // In real implementation, this would send to kaitu-service
            console.log('[Bridge Mock] send:', data);
          },

          onMessage(handler: (data: string) => void) {
            this._messageHandler = handler;
          },

          onError(handler: (error: Error) => void) {
            // Mock implementation
          },

          onStateChange(handler: (connected: boolean) => void) {
            // Mock implementation
          },

          isConnected() {
            return this._connected;
          },
        },
      };

      // Expose helpers for test control
      (window as any).__testBridge = {
        // Force VPN state change (for error simulation)
        setVpnState(state: VpnState) {
          vpnState = state;
          vpnStateListeners.forEach((listener) => listener(state));
        },

        // Get storage for verification
        getStorage() {
          return Object.fromEntries(storage);
        },

        // Get clipboard for verification
        getClipboard() {
          return clipboard;
        },

        // Simulate VPN connection error
        simulateVpnError() {
          vpnState = 'disconnected';
          vpnStateListeners.forEach((listener) => listener(vpnState));
          window.dispatchEvent(
            new CustomEvent('control-status-change', {
              detail: { state: 'disconnected', error: 'Connection failed' },
            })
          );
        },
      };
    },
    { platform, os, initialVpnState, version }
  );
}

/**
 * Bridge Mock Helpers for Tests
 */
export const bridgeMockHelpers = {
  /**
   * Get current VPN state
   */
  async getVpnState(page: Page): Promise<VpnState> {
    return page.evaluate(() => (window as any).kaitu.vpnControl.getState());
  },

  /**
   * Force VPN state change (for error simulation)
   */
  async setVpnState(page: Page, state: VpnState): Promise<void> {
    await page.evaluate((state) => {
      (window as any).__testBridge.setVpnState(state);
    }, state);
  },

  /**
   * Simulate VPN connection error
   */
  async simulateVpnError(page: Page): Promise<void> {
    await page.evaluate(() => {
      (window as any).__testBridge.simulateVpnError();
    });
  },

  /**
   * Get storage contents (for verification)
   */
  async getStorage(page: Page): Promise<Record<string, string>> {
    return page.evaluate(() => (window as any).__testBridge.getStorage());
  },

  /**
   * Get clipboard contents (for verification)
   */
  async getClipboard(page: Page): Promise<string> {
    return page.evaluate(() => (window as any).__testBridge.getClipboard());
  },
};
