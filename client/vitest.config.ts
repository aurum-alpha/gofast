import { defineConfig } from "vitest/config";
import path from "path";

// No `include` list. The shared job scopes the run with --dir, and a pinned
// include silently overrides it — which is how a repo ends up with a test job
// that runs the wrong unit's tests (CI_STANDARD "Standard repository layout").
export default defineConfig({
  test: {
    environment: "node",
    coverage: {
      provider: "v8",
      include: ["src/**"],
    },
  },
  resolve: {
    alias: {
      "@": path.resolve(import.meta.dirname, "src"),
    },
  },
});
