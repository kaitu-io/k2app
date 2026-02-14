import { useTranslation } from 'react-i18next';
import { cn } from '../lib/cn';
import type { Server } from '../stores/servers.store';

interface Props {
  servers: Server[];
  selectedId: string | null;
  onSelect: (server: Server) => void;
}

// Simple country code to flag emoji
function countryFlag(code: string): string {
  if (!code || code.length !== 2) return '\u{1F310}';
  const offset = 127397;
  return String.fromCodePoint(
    ...code.toUpperCase().split('').map((c) => c.charCodeAt(0) + offset)
  );
}

export function ServerList({ servers, selectedId, onSelect }: Props) {
  const { t } = useTranslation();

  if (servers.length === 0) {
    return (
      <div className="text-center text-gray-500 py-8">
        {t('common:loading')}
      </div>
    );
  }

  return (
    <div className="space-y-1">
      {servers.map((server) => (
        <button
          key={server.id}
          onClick={() => onSelect(server)}
          className={cn(
            'w-full flex items-center gap-3 px-4 py-3 rounded-lg transition-colors text-left',
            server.id === selectedId
              ? 'bg-blue-50 border border-blue-200'
              : 'hover:bg-gray-50'
          )}
        >
          <span className="text-2xl">{countryFlag(server.countryCode)}</span>
          <div className="flex-1 min-w-0">
            <p className="font-medium truncate">{server.name}</p>
            <p className="text-sm text-gray-500">
              {server.city ? `${server.city}, ${server.country}` : server.country}
            </p>
          </div>
          {server.load !== undefined && (
            <div className="text-xs text-gray-400">
              {server.load}%
            </div>
          )}
        </button>
      ))}
    </div>
  );
}
