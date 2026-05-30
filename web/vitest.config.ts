import { defineConfig } from 'vitest/config';

// Kept separate from vite.config.ts so the production build (outDir ->
// cmd/claude-monitor/static) is untouched. `vitest/config` re-exports vite's
// defineConfig, so no extra dependency is required.
export default defineConfig({
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.ts'],
    globals: false,
  },
});
