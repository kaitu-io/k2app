/**
 * Custom hook for integrating Kaitu WebView Bridge with React components
 * 用于 iframe bridge 事件监听（Web 环境下为 stub 实现）
 */

import { useEffect, useCallback, useRef } from 'react';

type UnlistenFn = () => void;

// Web 环境下不支持原生事件监听，返回空操作
const listen = async (_event: string, _handler: (event: any) => void): Promise<UnlistenFn> => {
  return () => {};
};

interface UseKaituBridgeOptions {
  /**
   * Callback when bridge login is requested from web content
   */
  onLoginRequested?: () => void;
  
  /**
   * Callback when bridge logout is requested from web content
   */
  onLogoutRequested?: () => void;
  
  /**
   * Callback when bridge requests to show a toast
   */
  onShowToast?: (message: string, type: string) => void;
  
  /**
   * Callback when bridge requests to show an alert
   */
  onShowAlert?: (title: string, message: string) => void;
  
  /**
   * Enable automatic iframe injection when ref changes
   */
  autoInject?: boolean;
}

interface UseKaituBridgeReturn {
  /** Inject bridge into an iframe element */
  injectBridge: (iframe: HTMLIFrameElement) => void;
  /** Manually cleanup bridge resources */
  cleanup: () => void;
  /** Check if bridge is currently active */
  isActive: boolean;
  /** Send authentication state change to iframe */
  broadcastAuthStateChange: (isAuthenticated: boolean) => Promise<void>;
  /** Send custom event to iframe */
  broadcastCustomEvent: (eventType: string, data: any) => Promise<void>;
}

export function useKaituBridge(options: UseKaituBridgeOptions = {}): UseKaituBridgeReturn {
  const {
    onLoginRequested,
    onLogoutRequested,
    onShowToast,
    onShowAlert
  } = options;

  const isActiveRef = useRef(false);
  const listenersRef = useRef<UnlistenFn[]>([]);

  // Setup bridge event listeners
  useEffect(() => {
    const setupListeners = async () => {
      const listeners: UnlistenFn[] = [];

      if (onLoginRequested) {
        const unlisten = await listen('kaitu://bridge_login_requested', () => onLoginRequested());
        listeners.push(unlisten);
      }

      if (onLogoutRequested) {
        const unlisten = await listen('kaitu://bridge_logout_requested', () => onLogoutRequested());
        listeners.push(unlisten);
      }

      if (onShowToast) {
        const unlisten = await listen('kaitu://bridge_show_toast', (event: any) => {
          const { message, type } = event.payload;
          onShowToast(message, type);
        });
        listeners.push(unlisten);
      }

      if (onShowAlert) {
        const unlisten = await listen('kaitu://bridge_show_alert', (event: any) => {
          const { title, message } = event.payload;
          onShowAlert(title, message);
        });
        listeners.push(unlisten);
      }

      listenersRef.current = listeners;
    };

    setupListeners();

    return () => {
      // Cleanup listeners
      listenersRef.current.forEach(unlisten => unlisten());
      listenersRef.current = [];
    };
  }, [onLoginRequested, onLogoutRequested, onShowToast, onShowAlert]);

  // Inject bridge into iframe (stub - Web 环境不支持)
  const injectBridge = useCallback((_iframe: HTMLIFrameElement) => {
    isActiveRef.current = true;
  }, []);

  // Cleanup bridge (stub - Web 环境不支持)
  const cleanup = useCallback(() => {
    isActiveRef.current = false;
  }, []);

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      cleanup();
    };
  }, [cleanup]);

  // Broadcast auth state change to iframe (stub - Web 环境不支持)
  const broadcastAuthStateChange = useCallback(async (_isAuthenticated: boolean) => {}, []);

  // Broadcast custom event to iframe (stub - Web 环境不支持)
  const broadcastCustomEvent = useCallback(async (_eventType: string, _data: any) => {}, []);

  return {
    injectBridge,
    cleanup,
    isActive: isActiveRef.current,
    broadcastAuthStateChange,
    broadcastCustomEvent,
  };
}

/**
 * Hook for integration with existing Auth context
 * 
 * Example usage:
 * ```tsx
 * function DiscoverPage() {
 *   const { login, logout } = useAuth(); // Your existing auth hook
 *   const { showToast, showAlert } = useUI(); // Your existing UI hook
 *   
 *   const { injectBridge } = useKaituBridgeWithAuth({
 *     onLogin: login,
 *     onLogout: logout,
 *     onToast: showToast,
 *     onAlert: showAlert,
 *   });
 *   
 *   const handleIframeLoad = () => {
 *     if (iframeRef.current) {
 *       injectBridge(iframeRef.current);
 *     }
 *   };
 *   
 *   return <iframe ref={iframeRef} onLoad={handleIframeLoad} />
 * }
 * ```
 */
export function useKaituBridgeWithAuth(integration: {
  onLogin?: () => void | Promise<void>;
  onLogout?: () => void | Promise<void>;
  onToast?: (message: string, type: string) => void;
  onAlert?: (title: string, message: string) => void;
}) {
  return useKaituBridge({
    onLoginRequested: integration.onLogin,
    onLogoutRequested: integration.onLogout,
    onShowToast: integration.onToast,
    onShowAlert: integration.onAlert,
  });
}

