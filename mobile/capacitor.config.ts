import type { CapacitorConfig } from '@capacitor/cli';

// Brand at build time — same K2_BRAND contract as webapp/desktop.
// appId must match the Android flavor applicationId / iOS bundle id actually
// being built; Makefile exports K2_BRAND before every cap sync.
const BRAND = process.env.K2_BRAND === 'overleap' ? 'overleap' : 'kaitu';

const config: CapacitorConfig = {
  // appId/appName are consumed only by `cap init` / `cap add` when generating
  // a native project. Both platforms already exist, so these values are inert
  // — `cap sync` never reads them. The real ids live per-brand in each native
  // project (Android product flavors in android/app/build.gradle:
  // io.kaitu / io.overleap; iOS PRODUCT_BUNDLE_IDENTIFIER = $(K2_BUNDLE_ID) in
  // ios/App/App.xcodeproj/project.pbxproj). kaitu's legacy iOS id
  // (com.allnationconnect.anc.wgios) is permanent — App Store bundle ids
  // cannot change post-publish. Overleap is a separate, newly-created app
  // record, so it is free to use io.overleap from day one. Keep these values
  // brand-conditional to match reality even though nothing actually reads them.
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
