import type { CapacitorConfig } from '@capacitor/cli';

const config: CapacitorConfig = {
  // appId is consumed only by `cap init` / `cap add` when generating a native
  // project. Both platforms already exist, so this value is inert — `cap sync`
  // never reads it. The two platforms genuinely ship under different ids and
  // neither takes its id from here:
  //   iOS      com.allnationconnect.anc.wgios  ← ios/App/App.xcodeproj/project.pbxproj
  //   Android  io.kaitu                        ← android/app/build.gradle
  // iOS is stuck on the legacy id permanently: App Store bundle ids cannot be
  // changed after publishing, and moving to a new app record would reset
  // ratings/rankings and strip auto-renewable subscriptions (they bind to the
  // app record, so existing subscribers keep getting billed on the old app
  // while the new one cannot see them). Do not "fix" the divergence.
  appId: 'io.kaitu',
  appName: 'Kaitu.io',
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
