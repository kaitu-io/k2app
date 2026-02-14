import { create } from 'zustand';
import { cloudApi } from '../api/cloud';

export interface Server {
  id: string;
  name: string;
  country: string;
  countryCode: string;
  city?: string;
  wireUrl: string;
  load?: number;
  latency?: number;
}

interface ServersStore {
  servers: Server[];
  selectedServerId: string | null;
  isLoading: boolean;
  error: string | null;

  fetchServers: () => Promise<void>;
  selectServer: (id: string) => void;
  getSelectedServer: () => Server | undefined;
}

export const useServersStore = create<ServersStore>((set, get) => ({
  servers: [],
  selectedServerId: null,
  isLoading: false,
  error: null,

  fetchServers: async () => {
    set({ isLoading: true, error: null });
    try {
      const resp = await cloudApi.getTunnels();
      const data = resp.data as Array<{
        id: string;
        name: string;
        country: string;
        countryCode: string;
        city?: string;
        wireUrl: string;
        load?: number;
      }>;
      const servers: Server[] = (data || []).map((s) => ({
        id: s.id,
        name: s.name,
        country: s.country,
        countryCode: s.countryCode,
        city: s.city,
        wireUrl: s.wireUrl,
        load: s.load,
      }));
      set({ servers, isLoading: false });

      // Auto-select first server if none selected
      if (!get().selectedServerId && servers.length > 0) {
        set({ selectedServerId: servers[0]!.id });
      }
    } catch (e) {
      set({
        isLoading: false,
        error: e instanceof Error ? e.message : 'Failed to load servers',
      });
    }
  },

  selectServer: (id: string) => {
    set({ selectedServerId: id });
  },

  getSelectedServer: () => {
    const { servers, selectedServerId } = get();
    return servers.find((s) => s.id === selectedServerId);
  },
}));
