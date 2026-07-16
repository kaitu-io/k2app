import type { CapacitorConfig } from '@capacitor/cli';

// Brand at build time — same K2_BRAND contract as webapp/desktop.
// appId must match the Android flavor applicationId / iOS bundle id actually
// being built; Makefile exports K2_BRAND before every cap sync.
const BRAND = process.env.K2_BRAND === 'overleap' ? 'overleap' : 'kaitu';

const config: CapacitorConfig = {
  appId: BRAND === 'overleap' ? 'io.overleap' : 'io.kaitu',
  appName: BRAND === 'overleap' ? 'Overleap' : '开途',
  webDir: '../webapp/dist',
  ios: {
    contentInset: 'never',
    allowsLinkPreview: false,
    scrollEnabled: false,
  },
  android: {
    allowMixedContent: true,
    backgroundColor: '#0F0F13',
  },
  plugins: {
    StatusBar: {
      overlaysWebView: true,
      style: 'DARK',
      backgroundColor: '#00000000',
    },
    EdgeToEdge: {
      backgroundColor: '#0F0F13',
    },
  },
};

export default config;
