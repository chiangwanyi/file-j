import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";
// @ts-expect-error type error without @types/node package
import process from "node:process";
const host = process.env.TAURI_DEV_HOST;

// https://vite.dev/config/
export default defineConfig(() => ({
  plugins: [vue()],

  build: {
    // macOS 12 ships Safari 15 / WKWebView. Vite's default target is newer, so
    // we pin the output to what the oldest supported OS can parse. This is also
    // why Tailwind stays on v3 (v4 needs Safari 16.4 for @property/color-mix).
    target: ["safari15"],
    cssTarget: ["safari15"],
    sourcemap: false,
  },
  esbuild: {
    target: "safari15",
  },

  // Vite options tailored for Tauri development and only applied in `tauri dev` or `tauri build`
  //
  // 1. prevent Vite from obscuring rust errors
  clearScreen: false,
  // 2. tauri expects a fixed port, fail if that port is not available
  server: {
    port: 1420,
    strictPort: true,
    host: host || false,
    hmr: host
      ? {
          protocol: "ws",
          host,
          port: 1421,
        }
      : undefined,
    watch: {
      // 3. tell Vite to ignore watching `src-tauri`
      ignored: ["**/src-tauri/**"],
    },
  },
}));
