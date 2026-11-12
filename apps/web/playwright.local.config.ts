import { defineConfig } from '@playwright/test';
// 本地账号（非 OIDC）回归：服务端不配置任何 OIDC 环境变量，
// 前端因此走 localStorage token 路径。/api/v1/auth/session 返回 404。
export default defineConfig({
  testDir: './tests',
  testMatch: ['local-account.spec.ts'],
  fullyParallel: false,
  workers: 1,
  retries: 0,
  timeout: 60_000,
  use: { baseURL: 'http://127.0.0.1:18888', trace: 'off', screenshot: 'only-on-failure' },
  webServer: { command: '../../scripts/run-local-account-tests.sh', url: 'http://127.0.0.1:18888/api/v1/readyz', reuseExistingServer: false, timeout: 180_000 },
});
