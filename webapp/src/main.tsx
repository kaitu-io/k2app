import React from "react";
import ReactDOM from "react-dom/client";
import * as Sentry from "@sentry/react";
import App from "./App";
import { i18nPromise } from "./i18n/i18n";
import { initializeAllStores } from "./stores";

// ==================== Viewport Scaling ====================

/**
 * Design width for the UI
 * The UI is designed for 430px width and will be scaled proportionally when window is narrower
 */
const DESIGN_WIDTH = 430;

/**
 * Setup viewport scaling for narrow windows.
 * When window is smaller than design size, scale the UI proportionally.
 *
 * Applies transform to body instead of #root so that MUI Portals (Dialogs, Popovers, etc.)
 * are also scaled - they render as direct children of body.
 */
function setupViewportScaling() {
  function applyScale() {
    const windowWidth = window.innerWidth;
    const windowHeight = window.innerHeight;

    // Only use width for scaling calculation (height affected by titlebar)
    const scaleX = windowWidth / DESIGN_WIDTH;

    // Use width-based scale, never scale up
    const scale = Math.min(scaleX, 1);

    if (scale < 1) {
      // Apply to body so Portal content (MUI Dialogs) is also scaled
      document.body.style.width = `${DESIGN_WIDTH}px`;
      document.body.style.height = `${windowHeight / scale}px`;
      document.body.style.transform = `scale(${scale})`;
      document.body.style.transformOrigin = "top left";

      console.info(`[Viewport] Scaling UI: ${scale.toFixed(4)}x (window: ${windowWidth}x${windowHeight}, design width: ${DESIGN_WIDTH})`);
    } else {
      // No scaling needed
      document.body.style.width = "";
      document.body.style.height = "";
      document.body.style.transform = "";
      document.body.style.transformOrigin = "";
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
    } else if (!window._k2 || !window._platform) {
      console.warn('[WebApp] Globals missing, injecting standalone implementation...');
      const { ensureK2Injected } = await import('./services/standalone-k2');
      ensureK2Injected();
    } else {
      console.info('[WebApp] K2 and platform already injected by host');
    }
  }

  // Log source
  const { getK2Source } = await import('./services/standalone-k2');
  console.info(`[WebApp] K2 source: ${getK2Source()}`);

  // 初始化所有 Stores
  const cleanupStores = initializeAllStores();
  console.info('[WebApp] Stores initialized');

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
