import { defineConfig } from "vitest/config";

// The emulator the dev server proxies to. Override with EMULATOR_URL when the
// Go server runs on a non-default port (PORT is read by cmd/server).
const emulator = process.env.EMULATOR_URL ?? "http://localhost:8080";

export default defineConfig({
  build: {
    // Vite owns this directory and may empty it freely; scripts/publish-dist.mjs
    // copies the result into server/ui/dist, which go:embed reads.
    outDir: "dist",
    emptyOutDir: true,
    // Source maps would be embedded in every release binary and served
    // publicly, so they stay out of the production bundle.
    sourcemap: false,
  },
  test: {
    // The modules that render server data build real DOM nodes, so their tests
    // need a document. happy-dom is enough for that and starts far faster than
    // a browser.
    environment: "happy-dom",
  },
  server: {
    port: 5173,
    strictPort: true,
    // Same-origin in production (the binary serves the UI), so the proxy exists
    // only to keep development free of CORS as well. The console talks to
    // /api/v2 exclusively; the gosnowflake protocol routes (/session, /queries,
    // /telemetry) are for drivers and are deliberately not proxied.
    proxy: {
      "/api": { target: emulator, changeOrigin: false },
      "/health": { target: emulator, changeOrigin: false },
    },
  },
});
