import { defineConfig, devices } from '@playwright/test';
export default defineConfig({
  testDir: './e2e', fullyParallel: true,
  forbidOnly: !!process.env.CI, retries: process.env.CI ? 1 : 0,
  reporter: [['list'], ['html', { open: 'never' }]],
  use: { baseURL: 'http://127.0.0.1:4173', trace: 'retain-on-failure', screenshot: 'only-on-failure' },
  projects: [
    { name: 'chromium', testIgnore: '**/live.spec.ts', use: { ...devices['Desktop Chrome'] } },
    { name: 'mobile-chromium', testIgnore: '**/live.spec.ts', use: { ...devices['Pixel 7'] } },
    { name: 'webkit', testIgnore: '**/live.spec.ts', use: { ...devices['Desktop Safari'] } },
    { name: 'live', testMatch: '**/live.spec.ts', use: { ...devices['Desktop Chrome'], baseURL: process.env.QIMENG_E2E_BASE_URL ?? 'http://127.0.0.1:18481' } },
  ],
  webServer: process.env.QIMENG_E2E_BASE_URL ? undefined : { command: 'npm run preview', url: 'http://127.0.0.1:4173', reuseExistingServer: false },
});
