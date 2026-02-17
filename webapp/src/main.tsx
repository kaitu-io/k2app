import React from "react";
import ReactDOM from "react-dom/client";
import * as Sentry from "@sentry/react";
import App from "./App";
import { i18nPromise } from "./i18n/i18n";
import { initializeAllStores } from "./stores";

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
