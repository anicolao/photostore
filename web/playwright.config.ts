import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './tests/e2e',
  fullyParallel: false,
  retries: 0,
  reporter: 'html',
  timeout: 60_000,
  expect: {
    timeout: 2_000,
    toHaveScreenshot: {
      maxDiffPixels: 0
    }
  },
  use: {
    baseURL: 'http://127.0.0.1:5174',
    trace: 'on-first-retry',
    timezoneId: 'America/New_York',
    locale: 'en-CA',
    viewport: { width: 1280, height: 900 },
    deviceScaleFactor: 1
  },
  snapshotPathTemplate: '{testDir}/{testFileDir}/screenshots/{arg}.png',
  projects: [
    {
      name: 'chromium',
      use: {
        ...devices['Desktop Chrome'],
        launchOptions: {
          args: [
            '--disable-gpu',
            '--disable-dev-shm-usage',
            '--disable-font-subpixel-positioning',
            '--disable-lcd-text',
            '--font-render-hinting=none',
            '--use-gl=swiftshader'
          ]
        }
      }
    }
  ],
  webServer: {
    command: 'bun run test-server',
    url: 'http://127.0.0.1:5174',
    reuseExistingServer: !process.env.CI
  }
});
