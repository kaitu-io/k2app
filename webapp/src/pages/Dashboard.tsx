import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useVpnStore } from '../stores/vpn.store';
import { ConnectionButton } from '../components/ConnectionButton';

export function Dashboard() {
  const { t } = useTranslation('dashboard');
  const { state, error, connect, disconnect } = useVpnStore();
  const [uptime, setUptime] = useState(0);

  // Uptime counter when connected
  useEffect(() => {
    if (state !== 'connected') {
      setUptime(0);
      return;
    }
    const interval = setInterval(() => {
      setUptime((prev) => prev + 1);
    }, 1000);
    return () => clearInterval(interval);
  }, [state]);

  const formatUptime = (seconds: number): string => {
    const h = Math.floor(seconds / 3600);
    const m = Math.floor((seconds % 3600) / 60);
    const s = seconds % 60;
    return `${h.toString().padStart(2, '0')}:${m.toString().padStart(2, '0')}:${s.toString().padStart(2, '0')}`;
  };

  // For now, use a placeholder wire_url. The real URL comes from server selection (W4).
  const handleConnect = () => {
    connect('k2v5://connect');
  };

  return (
    <div className="flex flex-col items-center justify-center h-full p-6 gap-8">
      <div className="text-center">
        <h1 className="text-xl font-semibold mb-2">{t('title')}</h1>
        <p className="text-gray-500 text-sm">
          {state === 'connected' ? t('connected') : t('disconnected')}
        </p>
      </div>

      <ConnectionButton
        state={state}
        onConnect={handleConnect}
        onDisconnect={disconnect}
      />

      {state === 'connected' && (
        <div className="text-center">
          <p className="text-sm text-gray-500">{t('uptime')}</p>
          <p className="text-2xl font-mono">{formatUptime(uptime)}</p>
        </div>
      )}

      {error && (
        <div className="w-full max-w-sm p-3 bg-red-50 text-red-700 rounded text-sm text-center">
          {t('error')}: {error}
        </div>
      )}
    </div>
  );
}
