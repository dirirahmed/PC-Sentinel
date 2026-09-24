import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

// The Go agent serves the built app from web/dist. During development the
// Vite dev server proxies API calls to a locally running agent.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      "/api": { target: "http://127.0.0.1:8787", changeOrigin: true },
    },
  },
  build: {
    outDir: "dist",
    // public/.gitkeep is copied into dist so the committed placeholder that
    // lets `go build` embed dist/ survives a clean build.
    emptyOutDir: true,
    sourcemap: false,
  },
  test: {
    environment: "node",
  },
});
