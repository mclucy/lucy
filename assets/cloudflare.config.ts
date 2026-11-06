import { bindings, defineConfig, triggers } from "cf/config";

export default defineConfig({
  worker: {
    name: "lucy-static-assets",
    compatibilityDate: "2026-10-30",
    placement: { mode: "smart" },
    entrypoint: "./src/index.ts",
    assets: {
      runWorkerFirst: ["/latest.json", "/dl/*"],
    },
    env: {
      DOWNLOADS: bindings.r2({ name: "lucy-releases" }),
    },
    triggers: [
      triggers.fetch({ pattern: "lucy.lc/sh", zone: "lucy.lc" }),
      triggers.fetch({ pattern: "lucy.lc/ps1", zone: "lucy.lc" }),
      triggers.fetch({ pattern: "lucy.lc/manifest.schema.json", zone: "lucy.lc" }),
      triggers.fetch({ pattern: "lucy.lc/latest.json", zone: "lucy.lc" }),
      triggers.fetch({ pattern: "lucy.lc/dl/*", zone: "lucy.lc" }),
    ],
    observability: {
      enabled: true,
      issues: {
        enabled: true,
      },
      traces: {
        enabled: true,
      },
    },
    cache: {
      enabled: true,
    },
  },
});
