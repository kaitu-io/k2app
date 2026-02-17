/**
 * Playwright E2E Test Configuration
 *
 * Multi-browser testing for:
 * - WebKit (Safari) - critical for macOS 12.5 input focus fixes
 * - Chromium (Chrome) - primary browser
 * - Firefox - optional secondary browser
 * - Mobile viewports - iOS Safari, Android Chrome
 */
import { defineConfig, devices } from '@playwright/test';

/**
 * See https://playwright.dev/docs/test-configuration
 */
export default defineConfig({
  // Test directory
  testDir: './e2e',

  // Test file pattern
  testMatch: '**/*.spec.ts',

  // Run tests in parallel
  fullyParallel: true,

  // Fail CI on test.only
  forbidOnly: !!process.env.CI,

  // Retry on CI
  retries: process.env.CI ? 2 : 0,

  // Parallel workers
  workers: process.env.CI ? 1 : undefined,

  // Reporters
  reporter: [
    ['html', { outputFolder: 'playwright-report' }],
    ['json', { outputFile: 'test-results/e2e-results.json' }],
    ['list'],
    // GitHub Actions compatible reporter
    ...(process.env.CI ? [['github' as const]] : []),
  ],

  // Global timeout
  timeout: 30000,

  // Expect timeout
  expect: {
    timeout: 5000,
  },

  // Shared settings
  use: {
    // Base URL
    baseURL: process.env.PLAYWRIGHT_BASE_URL || 'http://localhost:5173',

    // Trace on first retry
    trace: 'on-first-retry',

    // Screenshot on failure
    screenshot: 'only-on-failure',

    // Video on first retry
    video: 'on-first-retry',

    // Default viewport
    viewport: { width: 1280, height: 720 },

    // Ignore HTTPS errors
    ignoreHTTPSErrors: true,

    // Chinese locale
    locale: 'zh-CN',

    // Timezone
    timezoneId: 'Asia/Shanghai',
  },

  // Browser projects
  projects: [
    // =====================================================
    // Desktop Browsers
    // =====================================================
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
    {
      name: 'webkit',
      use: { ...devices['Desktop Safari'] },
    },
    {
      name: 'firefox',
      use: { ...devices['Desktop Firefox'] },
    },

    // =====================================================
    // WebKit-specific tests (for macOS Safari validation)
    // =====================================================
    {
      name: 'webkit-input-tests',
      testMatch: '**/webkit/**/*.spec.ts',
      use: { ...devices['Desktop Safari'] },
    },

    // =====================================================
    // Mobile Browsers
    // =====================================================
    {
      name: 'mobile-chrome',
      use: { ...devices['Pixel 5'] },
    },
    {
      name: 'mobile-safari',
      use: { ...devices['iPhone 12'] },
    },
    {
      name: 'tablet-safari',
      use: { ...devices['iPad (gen 7)'] },
    },

    // =====================================================
    // Safari Channel (real Safari on macOS)
    // Only works on macOS with Safari installed
    // =====================================================
    ...(process.platform === 'darwin'
      ? [
          {
            name: 'safari',
            testMatch: '**/webkit/**/*.spec.ts',
            use: {
              ...devices['Desktop Safari'],
              channel: 'webkit', // Uses system WebKit
            },
          },
        ]
      : []),
  ],

  // Dev server configuration
  webServer: {
    command: 'yarn dev',
    url: 'http://localhost:5173',
    reuseExistingServer: !process.env.CI,
    timeout: 120000,
  },

  // Output directory
  outputDir: 'test-results/e2e',
});
