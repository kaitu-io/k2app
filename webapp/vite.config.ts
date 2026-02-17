import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { resolve } from 'path';

const host = process.env.TAURI_DEV_HOST;

// https://vitejs.dev/config/
// NOTE: This config is for Web standalone only.
// Desktop uses desktop-tauri/vite.config.ts
// Mobile uses mobile-capacitor/vite.config.ts
export default defineConfig({
  plugins: [react()],

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
  },

  // Build configuration
  build: {
    outDir: 'dist',
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
  }
});
