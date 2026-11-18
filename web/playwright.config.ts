import { defineConfig, devices } from '@playwright/test';
const liveSuites = ['**/live.spec.ts', '**/*.live.spec.ts'];
export default defineConfig({
  testDir: './e2e', fullyParallel: true,
  forbidOnly: !!process.env.CI, retries: process.env.CI ? 1 : 0,
  reporter: process.env.QIMENG_E2E_BASE_URL ? [['list']] : [['list'], ['html', { open: 'never' }]],
  use: { baseURL: 'http://127.0.0.1:4173', trace: 'retain-on-failure', screenshot: 'only-on-failure' },
  projects: [
    { name: 'chromium', testIgnore: liveSuites, use: { ...devices['Desktop Chrome'], serviceWorkers: 'block' } },
    { name: 'mobile-chromium', testIgnore: liveSuites, use: { ...devices['Pixel 7'], serviceWorkers: 'block' } },
    { name: 'webkit', testIgnore: liveSuites, use: { ...devices['Desktop Safari'], serviceWorkers: 'block' } },
    // Live requests contain the fixture password, session cookies and one-time credentials.
    // Keep the synthetic UI captures; a network/DOM trace also captures those secrets.
    { name: 'live', testMatch: liveSuites, use: { ...devices['Desktop Chrome'], trace: 'off', baseURL: process.env.QIMENG_E2E_BASE_URL ?? 'http://127.0.0.1:18481' } },
  ],
  webServer: process.env.QIMENG_E2E_BASE_URL ? undefined : { command: 'npm run preview', url: 'http://127.0.0.1:4173', reuseExistingServer: false },
});
