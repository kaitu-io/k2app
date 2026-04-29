/**
 * Vite Configuration for OpenWRT Router Build
 *
 * This configuration produces a static build optimized for OpenWRT routers:
 * - Desktop layout (side navigation) forced via VITE_CLIENT_IS_ROUTER=true
 * - Relative base path for uhttpd deployment
 * - Optimized bundle size
 * - HTTP control via kaitu-service
 */

import { defineConfig, type PluginOption } from "vite";
import react from "@vitejs/plugin-react";
import { resolve } from 'path';

// Mirrors what k2/webui/serve.go injects in production. Without this script
// the webapp would fall through to standalone-k2 instead of gateway-k2.
function injectGatewayGlobal(): PluginOption {
  return {
    name: 'inject-k2-gateway-global',
    transformIndexHtml: {
      order: 'pre',
      handler(html) {
        const tag = `<script>window.__K2_GATEWAY__={version:"dev",commit:"dev",arch:"linux/arm64"}</script>`;
        return html.replace('<head>', `<head>${tag}`);
      },
    },
  };
}

const gatewayPort = process.env.K2_GATEWAY_PORT || '1779';
const gatewayProxy = { target: `http://127.0.0.1:${gatewayPort}`, changeOrigin: true };
// SSE endpoint needs streaming — disable proxy buffering by keeping the same
// http-proxy options; vite/http-proxy passes text/event-stream through fine.

export default defineConfig({
  plugins: [react(), injectGatewayGlobal()],

  // Prevent vite from obscuring errors
  clearScreen: false,

  // Development server (for testing OpenWRT build locally — paired with k2r in Docker)
  server: {
    port: 1422,
    strictPort: false,
    host: true,
    cors: true,
    proxy: {
      '/ping': gatewayProxy,
      '/api/core': gatewayProxy,
      '/api/events': gatewayProxy,
      '/api/log-level': gatewayProxy,
      '/api/storage': gatewayProxy,
      '/api/platform': gatewayProxy,
      '/api/upgrade': gatewayProxy,
      '/api/router-devices': gatewayProxy,
    },
  },

  // Build configuration
  build: {
    outDir: 'dist-openwrt',

    // Optimizations
    rollupOptions: {
      output: {
        manualChunks: {
          'react-vendor': ['react', 'react-dom', 'react-router-dom'],
          'mui-vendor': ['@mui/material', '@mui/icons-material'],
          'i18n': ['i18next', 'react-i18next'],
        },
      },
    },

    minify: 'terser',
    terserOptions: {
      compress: {
        drop_console: true,
        drop_debugger: true,
      },
    },

    // Generate smaller chunks
    chunkSizeWarningLimit: 500,
  },

  // Relative base path for OpenWRT uhttpd deployment
  // Allows deployment to any subdirectory (e.g., /kaitu/, /www/kaitu/)
  base: './',

  // Module resolution
  resolve: {
    alias: {
      '@': resolve(__dirname, './src'),
    },
  },

  // Environment variables for OpenWRT/Router mode
  define: {
    // Force desktop layout
    'import.meta.env.VITE_CLIENT_IS_ROUTER': JSON.stringify('true'),

    // API endpoint (can be overridden at build time)
    'import.meta.env.VITE_KAITU_ENTRY_URL': JSON.stringify(
      process.env.VITE_KAITU_ENTRY_URL || 'https://k2.52j.me'
    ),

    // Disable mock mode
    'import.meta.env.VITE_USE_MOCK': JSON.stringify('false'),

    // Mark as OpenWRT build
    'import.meta.env.IS_OPENWRT_BUILD': JSON.stringify(true),
    'import.meta.env.IS_MOBILE_BUILD': JSON.stringify(false),
  },
});
