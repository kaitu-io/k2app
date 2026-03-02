import { registerPlugin } from '@capacitor/core';
const K2Plugin = registerPlugin('K2Plugin', {
    web: () => import('./web').then((m) => new m.K2PluginWeb()),
});
export * from './definitions';
export { K2Plugin };
