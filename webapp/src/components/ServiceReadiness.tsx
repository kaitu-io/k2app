import { useEffect, useState, type ReactNode } from 'react';
import { useVpnStore } from '../stores/vpn.store';

interface Props {
  children: ReactNode;
}

export function ServiceReadiness({ children }: Props) {
  const isNativeApp = typeof window !== 'undefined' &&
    ((window as any).__TAURI__ || (window as any).Capacitor);

  const { ready, init } = useVpnStore();
  const [retrying, setRetrying] = useState(false);
  const [timedOut, setTimedOut] = useState(false);

  useEffect(() => {
    if (!isNativeApp) return;
    init();
  }, [init, isNativeApp]);

  useEffect(() => {
    if (!isNativeApp) return;
    if (ready && !ready.ready && ready.reason === 'not_running' && !timedOut) {
      setRetrying(true);
      let attempts = 0;
      const maxAttempts = 20; // 10s at 500ms intervals
      const interval = setInterval(async () => {
        attempts++;
        await init();
        if (attempts >= maxAttempts) {
          clearInterval(interval);
          setRetrying(false);
          setTimedOut(true);
        }
      }, 500);
      return () => clearInterval(interval);
    }
  }, [ready, init, timedOut, isNativeApp]);

  if (!isNativeApp) {
    return <>{children}</>;
  }

  if (!ready) {
    return <div className="flex items-center justify-center h-screen"><p>Loading...</p></div>;
  }

  if (!ready.ready) {
    if (retrying) {
      return (
        <div className="flex flex-col items-center justify-center h-screen gap-4">
          <p>Starting service...</p>
        </div>
      );
    }
    return (
      <div className="flex flex-col items-center justify-center h-screen gap-4">
        <p>Service not available: {ready.reason}</p>
        <button
          className="px-4 py-2 bg-blue-600 text-white rounded"
          onClick={() => { setTimedOut(false); init(); }}
        >
          Retry
        </button>
      </div>
    );
  }

  return <>{children}</>;
}
