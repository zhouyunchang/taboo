import { defineConfig } from '@playwright/test';
export default defineConfig({
  testDir: './tests', fullyParallel: false, workers: 1,
  use: { baseURL: 'http://127.0.0.1:18888', trace: 'off', screenshot: 'only-on-failure' },
  webServer: { command: '../../scripts/serve-federation-web-test.sh', url: 'http://127.0.0.1:18888/api/v1/readyz', reuseExistingServer: false, timeout: 120_000 },
});
