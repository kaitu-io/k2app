import React from "react";
import ReactDOM from "react-dom/client";
import * as Sentry from "@sentry/react";
import App from "./App";
import { i18nPromise } from "./i18n/i18n";
import { initializeAllStores } from "./stores";
import {
  DESIGN_WIDTH,
  ViewportState,
  computeScaleDecision,
  isAndroidCapacitorWebView,
} from "./utils/viewport-scaling";

// ==================== Viewport Scaling ====================

/**
 * Setup viewport scaling for narrow windows.
 * When window is smaller than design size, scale the UI proportionally.
 *
 * Uses CSS zoom (not transform) so that position:fixed elements (react-joyride
 * overlays, ServiceAlert, MUI Portals) stay relative to the viewport.
 * CSS transform creates a new containing block which breaks fixed positioning.
 *
 * Android-only guard: on Capacitor Android, soft-keyboard show/hide fires
 * resize with width unchanged. Mutating body during that event dismisses
 * the keyboard and closes MUI Dialogs. The decision helper handles this —
 * Tauri desktop and iOS always recompute so window drag triggers reflow.
 */
function setupViewportScaling() {
  let previous: ViewportState | null = null;
  const isAndroid = isAndroidCapacitorWebView();

  function applyScale() {
    const current: ViewportState = {
      windowWidth: window.innerWidth,
      windowHeight: window.innerHeight,
    };
    const decision = computeScaleDecision(current, previous, isAndroid);
    previous = current;

    if (decision.skip || !decision.bodyStyle) return;

    document.body.style.width = decision.bodyStyle.width;
    document.body.style.height = decision.bodyStyle.height;
    document.body.style.zoom = decision.bodyStyle.zoom;

    if (decision.bodyStyle.zoom) {
      console.info(
        `[Viewport] Scaling UI: ${decision.bodyStyle.zoom}x ` +
          `(window: ${current.windowWidth}x${current.windowHeight}, design width: ${DESIGN_WIDTH})`,
      );
    }
  }

  // Apply initial scale
  applyScale();

  // Re-apply on window resize
  window.addEventListener("resize", applyScale);
}

// ==================== Sentry ====================

Sentry.init({
  dsn: "https://937d73f741021925d1df3d90dee95973@o4509813540388864.ingest.us.sentry.io/4509813564112896",
  environment: import.meta.env.MODE,
  tracesSampleRate: 1.0,
  replaysSessionSampleRate: 0.1,
  replaysOnErrorSampleRate: 1.0,
  sendDefaultPii: true,
});

// ==================== 主入口 ====================

async function main() {
  // 等待 i18n 初始化
  await i18nPromise;
  console.info('[WebApp] i18n initialized');

  // Inject platform-specific globals
  if (window.__TAURI__) {
    console.info('[WebApp] Tauri detected, injecting Tauri bridge...');
    const { injectTauriGlobals } = await import('./services/tauri-k2');
    await injectTauriGlobals();
    // Sync current i18n locale to Rust for tray menu i18n
    const { default: i18n } = await import('i18next');
    window._platform?.syncLocale(i18n.language).catch(() => {});
    // Scale UI when window is narrower than design width (e.g. Windows 1080p)
    setupViewportScaling();
  } else {
    const { Capacitor } = await import('@capacitor/core');
    if (Capacitor.isNativePlatform()) {
      console.info('[WebApp] Capacitor native detected, injecting Capacitor bridge...');
      const { injectCapacitorGlobals } = await import('./services/capacitor-k2');
      await injectCapacitorGlobals();
      setupViewportScaling();
    } else if (window.__K2_GATEWAY__) {
      console.info('[WebApp] Gateway detected, injecting gateway bridge...');
      const { injectGatewayGlobals } = await import('./services/gateway-k2');
      await injectGatewayGlobals();
    } else if (!window._k2 || !window._platform) {
      console.warn('[WebApp] Globals missing, injecting standalone implementation...');
      const { ensureK2Injected } = await import('./services/standalone-k2');
      ensureK2Injected();
    } else {
      console.info('[WebApp] K2 and platform already injected by host');
    }
  }

  // One-time: remove orphaned encrypted storage from v0.4.0
  for (let i = localStorage.length - 1; i >= 0; i--) {
    const key = localStorage.key(i);
    if (key?.startsWith('_k2_secure_')) localStorage.removeItem(key);
  }

  // Log source
  const { getK2Source } = await import('./services/standalone-k2');
  console.info(`[WebApp] K2 source: ${getK2Source()}`);

  // 初始化所有 Stores
  const cleanupStores = initializeAllStores();
  console.info('[WebApp] Stores initialized');

  // Track app open for usage analytics
  import('./services/stats').then(({ statsService }) => {
    statsService.trackAppOpen();
  }).catch(() => {});

  // Start beta auto-upload timer (no-op if not on beta channel)
  import('./services/beta-auto-upload').then(({ startBetaAutoUpload }) => {
    startBetaAutoUpload();
  }).catch(() => {});

  // 渲染应用
  ReactDOM.createRoot(document.getElementById("root") as HTMLElement).render(
    <React.StrictMode>
      <App />
    </React.StrictMode>,
  );

  // HMR 清理
  if (import.meta.hot) {
    import.meta.hot.dispose(() => {
      cleanupStores();
    });
  }

  console.info('[WebApp] ✅ App started');
}

main().catch((error) => {
  console.error('[WebApp] Failed to initialize:', error);
});
