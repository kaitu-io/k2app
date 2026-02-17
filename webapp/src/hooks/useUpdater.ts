/**
 * Universal Updater Hook
 *
 * Works across all platforms through window._platform.updater interface.
 * Desktop (Tauri) provides full implementation.
 * Mobile/Web can provide stub or platform-specific implementation.
 */

import { useState, useEffect, useCallback } from 'react';
import type { UpdateInfo } from '../types/kaitu-core';

interface UpdaterState {
  updateReady: boolean;
  updateInfo: UpdateInfo | null;
  checking: boolean;
  installing: boolean;
  dismissed: boolean;
  error: string | null;
}

/**
 * Hook for managing app updates across platforms
 *
 * Requires window._platform.updater to be injected by platform.
 * If not available, returns empty state.
 */
export function useUpdater() {
  const [state, setState] = useState<UpdaterState>({
    updateReady: false,
    updateInfo: null,
    checking: false,
    installing: false,
    dismissed: false,
    error: null,
  });

  // Get updater from window._platform
  const updater = typeof window !== 'undefined' ? window._platform?.updater : null;

  // Sync state with updater
  useEffect(() => {
    if (!updater) {
      console.debug('[useUpdater] No updater available on this platform');
      return;
    }

    // Set initial state from updater
    setState(prev => ({
      ...prev,
      updateReady: updater.isUpdateReady,
      updateInfo: updater.updateInfo,
      checking: updater.isChecking,
      error: updater.error,
    }));

    // Listen for update-ready events if available
    if (updater.onUpdateReady) {
      const unsubscribe = updater.onUpdateReady((info: UpdateInfo) => {
        console.info('[useUpdater] Update ready: ' + JSON.stringify(info));
        setState(prev => ({
          ...prev,
          updateReady: true,
          updateInfo: info,
        }));
      });

      return unsubscribe;
    }
  }, [updater]);

  // Apply update now (restarts app)
  const applyUpdateNow = useCallback(async () => {
    if (!updater) {
      console.warn('[useUpdater] No updater available');
      return;
    }

    try {
      console.info('[useUpdater] Applying update now...');
      setState(prev => ({ ...prev, installing: true }));
      await updater.applyUpdateNow();
      // App will restart, this line won't be reached
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : String(error);
      console.error('[useUpdater] Failed to apply update:', error);
      setState(prev => ({
        ...prev,
        installing: false,
        error: errorMessage,
      }));
    }
  }, [updater]);

  // Dismiss update notification
  const dismissUpdate = useCallback(() => {
    setState(prev => ({ ...prev, dismissed: true }));
  }, []);

  // Manual check for updates
  const checkUpdateManual = useCallback(async () => {
    if (!updater?.checkUpdateManual) {
      console.warn('[useUpdater] Manual check not available');
      return 'Not supported on this platform';
    }

    setState(prev => ({ ...prev, checking: true, error: null }));

    try {
      console.info('[useUpdater] Manual update check...');
      const result = await updater.checkUpdateManual();
      console.info('[useUpdater] Check result: ' + JSON.stringify(result));

      setState(prev => ({
        ...prev,
        checking: false,
      }));

      return result;
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : String(error);
      console.error('[useUpdater] Manual check failed:', error);

      setState(prev => ({
        ...prev,
        checking: false,
        error: errorMessage,
      }));

      throw error;
    }
  }, [updater]);

  return {
    ...state,
    applyUpdateNow,
    checkUpdateManual,
    dismissUpdate,
    // Convenience aliases
    isUpdateReady: state.updateReady,
    isUpdateDownloaded: state.updateReady && !state.dismissed,
    isChecking: state.checking,
    isInstalling: state.installing,
    // Platform availability
    isAvailable: !!updater,
  };
}
