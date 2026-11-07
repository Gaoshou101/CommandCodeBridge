// Test real HTTP clients with production aliases. Upstream's test mode enables demo fixtures.
import { defineConfig, mergeConfig } from 'vitest/config';
import upstream from './vite.config';

export default defineConfig(async () => {
  const base = typeof upstream === 'function'
    ? await upstream({ command: 'serve', mode: 'production', isSsrBuild: false, isPreview: false })
    : upstream;
  return mergeConfig(base, {
    define: { __DEMO_SITE__: 'false' },
    test: { environment: 'node', include: ['behavior-tests/antigravityBehavior.test.ts'], testTimeout: 10000 },
  });
});
