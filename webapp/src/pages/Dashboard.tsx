import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useVpnStore } from '../stores/vpn.store';
import { useServersStore, type Server } from '../stores/servers.store';
import { ConnectionButton } from '../components/ConnectionButton';
import { ServerList } from '../components/ServerList';
import type { ClientConfig } from '../vpn-client/types';

export function buildConfig(
  server: { wireUrl: string },
  ruleMode: string,
): ClientConfig {
  return {
    server: server.wireUrl,
    rule: { global: ruleMode === 'global' },
  };
}

export function Dashboard() {
  const { t } = useTranslation('dashboard');
  const { state, error, connect, disconnect } = useVpnStore();
  const {
    servers,
    selectedServerId,
    fetchServers,
    selectServer,
    getSelectedServer,
  } = useServersStore();
  const [uptime, setUptime] = useState(0);

  // Fetch servers on mount
  useEffect(() => {
    fetchServers();
  }, [fetchServers]);

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

  const selectedServer = getSelectedServer();

  const handleConnect = () => {
    const server = selectedServer || { wireUrl: 'k2v5://connect' };
    const config = buildConfig(server, 'smart');
    connect(config);
  };

  const handleSelectServer = (server: Server) => {
    selectServer(server.id);
  };

  return (
    <div className="flex flex-col items-center justify-center h-full p-6 gap-8">
      <div className="text-center">
        <h1 className="text-xl font-semibold mb-2">{t('title')}</h1>
        <p className="text-gray-500 text-sm">
          {state === 'connected'
            ? t('connected')
            : state === 'connecting'
              ? t('connecting')
              : t('disconnected')}
        </p>
      </div>

      <ConnectionButton
        state={state}
        onConnect={handleConnect}
        onDisconnect={disconnect}
      />

      {selectedServer && (
        <div
          data-testid="selected-server-info"
          className="w-full max-w-sm p-4 rounded-lg bg-gray-50 border border-gray-200 text-center"
        >
          <p className="text-xs text-gray-400 mb-1">{t('selectedServer')}</p>
          <p className="font-medium">{selectedServer.name}</p>
          <p className="text-sm text-gray-500">
            {selectedServer.city
              ? `${selectedServer.city}, ${selectedServer.country}`
              : selectedServer.country}
          </p>
        </div>
      )}

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

      {servers.length > 0 && (
        <div className="w-full max-w-sm">
          <ServerList
            servers={servers}
            selectedId={selectedServerId}
            onSelect={handleSelectServer}
          />
        </div>
      )}
    </div>
  );
}
