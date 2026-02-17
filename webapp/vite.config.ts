import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "path";
import pkg from "../package.json";

const k2DaemonUrl = `http://127.0.0.1:${process.env.K2_DAEMON_PORT || "1777"}`;

export default defineConfig({
  define: {
    __APP_VERSION__: JSON.stringify(pkg.version),
  },
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  build: {
    rollupOptions: {
      input: {
        main: path.resolve(__dirname, "index.html"),
        debug: path.resolve(__dirname, "debug.html"),
      },
    },
  },
  server: {
    port: 1420,
    proxy: {
      "/api": {
        target: k2DaemonUrl,
        changeOrigin: true,
      },
      "/ping": {
        target: k2DaemonUrl,
        changeOrigin: true,
      },
    },
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test-setup.ts"],
  },
});
