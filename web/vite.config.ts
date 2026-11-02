import { defineConfig, type Plugin } from "vite";
import { readdirSync, rmSync } from "node:fs";
import { join } from "node:path";

// Vite's own cleaning would remove dist/.gitkeep, which keeps `go build`
// working in a checkout without a build. This removes everything else.
function cleanDist(): Plugin {
  return {
    name: "clean-dist-keep-placeholder",
    apply: "build",
    buildStart() {
      const dir = join(import.meta.dirname, "dist");
      for (const name of readdirSync(dir)) {
        if (name !== ".gitkeep") rmSync(join(dir, name), { recursive: true, force: true });
      }
    },
  };
}

// The app is served by the Go binary under /, assets under /assets/ with a hash
// in the name. Nothing in the output is inline: the CSP allows 'self' only.
export default defineConfig({
  plugins: [cleanDist()],
  build: {
    outDir: "dist",
    emptyOutDir: false, // dist/.gitkeep stays
    assetsDir: "assets",
    target: "es2022",
    cssCodeSplit: false,
    assetsInlineLimit: 0, // no data: URLs, they would need img-src data:
    sourcemap: false,
    chunkSizeWarningLimit: 300,
  },
  server: {
    proxy: { "/api": "http://127.0.0.1:7468" },
  },
});
