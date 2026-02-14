import { useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { useServersStore, type Server } from '../stores/servers.store';
import { useVpnStore } from '../stores/vpn.store';
import { ServerList } from '../components/ServerList';

export function Servers() {
  const { t } = useTranslation();
  const { servers, selectedServerId, isLoading, error, fetchServers, selectServer } = useServersStore();
  const { connect, state } = useVpnStore();

  useEffect(() => {
    fetchServers();
  }, [fetchServers]);

  const handleSelect = (server: Server) => {
    selectServer(server.id);
    if (state !== 'connected' && state !== 'connecting') {
      connect(server.wireUrl);
    }
  };

  return (
    <div className="p-4">
      <h1 className="text-xl font-semibold mb-4">{t('common:servers')}</h1>

      {error && (
        <div className="mb-4 p-3 bg-red-50 text-red-700 rounded text-sm">
          {error}
        </div>
      )}

      {isLoading ? (
        <div className="space-y-3">
          {[1, 2, 3].map((i) => (
            <div key={i} className="h-16 bg-gray-100 rounded-lg animate-pulse" />
          ))}
        </div>
      ) : (
        <ServerList
          servers={servers}
          selectedId={selectedServerId}
          onSelect={handleSelect}
        />
      )}
    </div>
  );
}
