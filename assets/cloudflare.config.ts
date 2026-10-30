import { defineConfig, triggers } from "cf/config";

export default defineConfig({
  worker: {
    name: "lucy-static-assets",
    compatibilityDate: "2026-10-30",
    triggers: [
      triggers.fetch({ pattern: "lucy.lc/sh", zone: "lucy.lc" }),
      triggers.fetch({ pattern: "lucy.lc/ps1", zone: "lucy.lc" }),
      triggers.fetch({ pattern: "lucy.lc/manifest.schema.json", zone: "lucy.lc" }),
    ],
  },
});
