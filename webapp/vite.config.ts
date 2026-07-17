import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { resolve } from 'path';
import { copyFileSync, existsSync, createReadStream } from 'fs';
import type { Plugin } from 'vite';

const host = process.env.TAURI_DEV_HOST;

// Brand at build time. Config files can't import src/brands (TS under Vite's
// own transform), so the product-name map is duplicated here — keep in sync
// with src/brands/{kaitu,overleap}/index.ts productName.
const K2_BRAND = process.env.K2_BRAND === 'overleap' ? 'overleap' : 'kaitu';
const BRAND_PRODUCT_NAME = K2_BRAND === 'overleap' ? 'Overleap' : 'Kaitu';

/**
 * - Rewrites <title> in index.html to the brand product name.
 * - Serves /favicon.png & /icon-*.png from src/brands/<brand>/assets in dev.
 * - After bundling, copies the same files into dist/ (public/ no longer holds
 *   any brand icon — src/brands/<brand>/assets/ is the single source).
 */
function brandPlugin(): Plugin {
  const iconFiles = ['favicon.png', 'icon-192x192.png', 'icon-512x512.png'];
  const assetDir = resolve(__dirname, 'src', 'brands', K2_BRAND, 'assets');
  return {
    name: 'k2-brand',
    transformIndexHtml(html) {
      return html.replace('<title>Kaitu</title>', `<title>${BRAND_PRODUCT_NAME}</title>`);
    },
    configureServer(server) {
      server.middlewares.use((req, res, next) => {
        const f = iconFiles.find((n) => req.url === `/${n}`);
        if (!f) return next();
        const p = resolve(assetDir, f);
        if (!existsSync(p)) return next();
        res.setHeader('Content-Type', 'image/png');
        createReadStream(p).pipe(res);
      });
    },
    writeBundle(options) {
      const outDir = options.dir || resolve(__dirname, 'dist');
      for (const f of iconFiles) {
        const src = resolve(assetDir, f);
        if (existsSync(src)) copyFileSync(src, resolve(outDir, f));
      }
    },
  };
}

// https://vitejs.dev/config/
// NOTE: This config is for Web standalone only.
// Desktop uses desktop-tauri/vite.config.ts
// Mobile uses mobile-capacitor/vite.config.ts
export default defineConfig({
  plugins: [react(), brandPlugin()],

  // 1. prevent vite from obscuring rust errors
  clearScreen: false,

  // Server configuration
  server: {
    port: 1420,
    strictPort: true,
    host: host || true,
    hmr: host
      ? {
          protocol: "ws",
          host,
          port: 1421,
        }
      : undefined,
    watch: {
      // 3. tell vite to ignore watching `src-tauri`
      ignored: ["**/src-tauri/**"],
    },
    proxy: (() => {
      const daemonPort = process.env.K2_DAEMON_PORT || '1777';
      const daemon = `http://127.0.0.1:${daemonPort}`;
      return {
        '/core': daemon,
        '/ping': daemon,
        '/api/core': daemon,
        '/api/helper': daemon,
        '/api/device': daemon,
      };
    })(),
  },

  // Build configuration
  build: {
    outDir: 'dist',
    // Floor set for Android System WebView on stock CN ROMs (no Play Store),
    // where users are stuck on Chrome 60–75. Vite's default 'modules' target
    // (Chrome 87+) keeps `??` / `?.` in the bundle and parse fails with
    // SyntaxError → blank screen / unresponsive login. esbuild transpiles
    // those operators down for chrome70.
    target: ['chrome70', 'safari14', 'firefox78', 'edge88'],
    rollupOptions: {
      input: {
        main: resolve(__dirname, 'index.html'),
        debug: resolve(__dirname, 'debug.html'),
      },
    },
  },

  // Base path for assets
  base: '/',

  // Module resolution
  resolve: {
    alias: {
      '@': resolve(__dirname, './src'),
    },
  },

  // Environment variables
  define: {
    'import.meta.env.VITE_KAITU_ENTRY_URL': JSON.stringify(process.env.VITE_KAITU_ENTRY_URL || 'https://k2.52j.me'),
    'import.meta.env.VITE_USE_MOCK': JSON.stringify(process.env.VITE_USE_MOCK || 'false'),
    'import.meta.env.VITE_CLIENT_IS_ROUTER': JSON.stringify(process.env.VITE_CLIENT_IS_ROUTER || 'false'),
    '__K2_BUILD_LOG_LEVEL__': JSON.stringify(process.env.K2_BUILD_LOG_LEVEL || 'debug'),
    '__K2_BUILD_COMMIT__': JSON.stringify(process.env.K2_COMMIT || ''),
    '__K2_BRAND__': JSON.stringify(process.env.K2_BRAND === 'overleap' ? 'overleap' : 'kaitu'),
  }
});
