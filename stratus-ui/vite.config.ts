/// <reference types="vitest/config" />
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// Las operaciones esperan al script hasta 15 minutos; el proxy da 16 min de margen.
const API_TIMEOUT_MS = 960000;

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    // "/src" es relativo a la raíz del proyecto; evita depender de @types/node.
    alias: {
      "@": "/src",
    },
  },
  server: {
    proxy: {
      "/api": {
        target: "http://127.0.0.1:8080",
        timeout: API_TIMEOUT_MS,
        proxyTimeout: API_TIMEOUT_MS,
      },
    },
  },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test/setup.ts"],
  },
});
