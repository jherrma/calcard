import { defineVitestConfig } from '@nuxt/test-utils/config';

// Wires Nuxt aliases (~/…), auto-imports (ref, computed, useApi, defineStore, …)
// and the Nuxt runtime env into Vitest. We use the `nuxt` test environment with
// happy-dom so component/store specs run against a lightweight DOM without a
// real browser. Do NOT hand-roll unimport here — defineVitestConfig owns that.
export default defineVitestConfig({
  test: {
    environment: 'nuxt',
    environmentOptions: {
      nuxt: {
        domEnvironment: 'happy-dom',
      },
    },
    // Co-locate specs next to sources as *.spec.ts.
    include: ['**/*.spec.ts'],
    globals: true,
  },
});
