/// <reference types="vitest/config" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test/setup.ts"],
    css: true,
    // Node's os.cpus() reports the CI host's full physical core count, not
    // the container's cgroup CPU quota (a well-known Node/Docker
    // limitation) - vitest's thread pool defaults maxThreads to that
    // number, so on CircleCI it spawns far more worker threads than the
    // resource class actually grants, and every thread gets throttled
    // fighting for the real quota. That thrashing is what caused ~30x
    // wall-clock blowup and flaky waitFor timeouts in CI (a different
    // random subset of tests failing each run) even after bumping
    // resource_class - more real cores just raised the ceiling being
    // oversubscribed, it never fixed the oversubscription itself. Capping
    // maxThreads keeps each worker's share of CPU real instead of
    // throttled slices of an imaginary one.
    poolOptions: {
      threads: {
        maxThreads: 4,
      },
    },
  },
});
