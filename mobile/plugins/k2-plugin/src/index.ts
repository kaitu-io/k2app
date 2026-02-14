import { registerPlugin } from '@capacitor/core';

import type { K2PluginInterface } from './definitions';

const K2Plugin = registerPlugin<K2PluginInterface>('K2Plugin', {
  web: () => import('./web').then((m) => new m.K2PluginWeb()),
});

export * from './definitions';
export { K2Plugin };
