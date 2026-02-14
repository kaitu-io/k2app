import { create } from 'zustand';
import { getVpnClient } from '../vpn-client';
import type { VpnState, ReadyState } from '../vpn-client';

interface VpnStore {
  state: VpnState;
  ready: ReadyState | null;
  error: string | null;
  init: () => Promise<void>;
  connect: (wireUrl: string) => Promise<void>;
  disconnect: () => Promise<void>;
}

export const useVpnStore = create<VpnStore>((set) => ({
  state: 'stopped',
  ready: null,
  error: null,

  init: async () => {
    const client = getVpnClient();
    const ready = await client.checkReady();
    set({ ready });
    if (ready.ready) {
      const status = await client.getStatus();
      set({ state: status.state });
      client.subscribe((event) => {
        if (event.type === 'state_change') set({ state: event.state, error: null });
        if (event.type === 'error') set({ error: event.message });
      });
    }
  },

  connect: async (wireUrl: string) => {
    set({ state: 'connecting', error: null });
    try {
      await getVpnClient().connect(wireUrl);
    } catch (e) {
      set({ state: 'stopped', error: e instanceof Error ? e.message : 'Connection failed' });
    }
  },

  disconnect: async () => {
    try {
      await getVpnClient().disconnect();
    } catch (e) {
      set({ error: e instanceof Error ? e.message : 'Disconnect failed' });
    }
  },
}));
