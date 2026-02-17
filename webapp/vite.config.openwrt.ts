/**
 * Vite Configuration for OpenWRT Router Build
 *
 * This configuration produces a static build optimized for OpenWRT routers:
 * - Desktop layout (side navigation) forced via VITE_CLIENT_IS_ROUTER=true
 * - Relative base path for uhttpd deployment
 * - Optimized bundle size
 * - HTTP control via kaitu-service
 */

import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { resolve } from 'path';

export default defineConfig({
  plugins: [react()],

  // Prevent vite from obscuring errors
  clearScreen: false,

  // Development server (for testing OpenWRT build locally)
  server: {
    port: 1422,
    strictPort: false,
    host: true,
    cors: true,
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
