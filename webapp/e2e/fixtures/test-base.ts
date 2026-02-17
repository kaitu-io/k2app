/**
 * Playwright Test Fixtures
 *
 * Provides test infrastructure and utilities:
 * - Bridge mock (VPN control, Platform, Storage)
 * - API mock (HTTP request interception)
 * - Authentication state management
 * - Common test utilities
 */
import { test as base, expect, Page } from '@playwright/test';
import { setupBridgeMock, bridgeMockHelpers, type BridgeMockConfig, type VpnState } from './bridge-mock';

// ============================================================
// Types
// ============================================================

export interface TestFixtures {
  /** Page with Bridge mock setup */
  page: Page;
  /** Authenticated page (with mock login state) */
  authenticatedPage: Page;
  /** API mock utilities */
  mockApi: MockApi;
  /** Bridge mock utilities */
  mockBridge: MockBridge;
}

interface MockApi {
  /** Mock tunnels list */
  mockTunnels(tunnels: any[]): Promise<void>;
  /** Mock user info */
  mockUserInfo(user: any): Promise<void>;
  /** Mock authentication endpoints */
  mockAuth(options?: MockAuthOptions): Promise<void>;
  /** Mock API error */
  mockApiError(path: string, statusCode: number, message: string): Promise<void>;
  /** Mock API success response */
  mockApiSuccess(path: string, data: any): Promise<void>;
  /** Mock API with delay (for loading state tests) */
  mockApiWithDelay(path: string, data: any, delayMs: number): Promise<void>;
}

interface MockAuthOptions {
  /** Should send code succeed */
  sendCodeSuccess?: boolean;
  /** Should verify code succeed */
  verifyCodeSuccess?: boolean;
  /** User exists */
  userExists?: boolean;
  /** User activated */
  isActivated?: boolean;
  /** Error message for failures */
  errorMessage?: string;
}

interface MockBridge {
  /** Setup Bridge mock with config */
  setup(config?: BridgeMockConfig): Promise<void>;
  /** Get current VPN state */
  getVpnState(): Promise<VpnState>;
  /** Set VPN state (for error simulation) */
  setVpnState(state: VpnState): Promise<void>;
  /** Simulate VPN connection error */
  simulateVpnError(): Promise<void>;
  /** Get storage contents */
  getStorage(): Promise<Record<string, string>>;
  /** Get clipboard contents */
  getClipboard(): Promise<string>;
}

// ============================================================
// Test Extension
// ============================================================

export const test = base.extend<TestFixtures>({
  // Override default page to include Bridge mock
  page: async ({ page }, use) => {
    // Setup Bridge mock by default
    await setupBridgeMock(page, { platform: 'desktop', os: 'macos' });
    await use(page);
  },

  // Authenticated page fixture
  authenticatedPage: async ({ page }, use) => {
    // Setup Bridge mock
    await setupBridgeMock(page, { platform: 'desktop', os: 'macos' });

    // Set authentication state in localStorage
    await page.addInitScript(() => {
      localStorage.setItem('auth_token', 'mock-token');
      localStorage.setItem('refresh_token', 'mock-refresh-token');
      localStorage.setItem('user_info', JSON.stringify({
        id: 'test-user',
        email: 'test@example.com',
        nickname: 'Test User',
      }));
    });

    await use(page);
  },

  // Mock API fixture
  mockApi: async ({ page }, use) => {
    const mockApi: MockApi = {
      async mockTunnels(tunnels: any[]) {
        await page.route('**/api/tunnels**', async (route) => {
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({
              code: 0,
              data: { items: tunnels },
            }),
          });
        });
      },

      async mockUserInfo(user: any) {
        await page.route('**/api/user/info**', async (route) => {
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({
              code: 0,
              data: user,
            }),
          });
        });
      },

      async mockAuth(options: MockAuthOptions = {}) {
        const {
          sendCodeSuccess = true,
          verifyCodeSuccess = true,
          userExists = true,
          isActivated = true,
          errorMessage = 'Authentication failed',
        } = options;

        // Mock send verification code
        await page.route('**/api/auth/code**', async (route) => {
          if (sendCodeSuccess) {
            await route.fulfill({
              status: 200,
              contentType: 'application/json',
              body: JSON.stringify({
                code: 0,
                data: {
                  userExists,
                  isActivated,
                  isFirstOrderDone: false,
                },
              }),
            });
          } else {
            await route.fulfill({
              status: 400,
              contentType: 'application/json',
              body: JSON.stringify({
                code: 400,
                message: errorMessage,
              }),
            });
          }
        });

        // Mock verify code / login
        await page.route('**/api/auth/login**', async (route) => {
          if (verifyCodeSuccess) {
            await route.fulfill({
              status: 200,
              contentType: 'application/json',
              body: JSON.stringify({
                code: 0,
                data: {
                  accessToken: 'mock-access-token',
                  refreshToken: 'mock-refresh-token',
                  user: {
                    id: 'test-user-id',
                    email: 'test@example.com',
                    nickname: 'Test User',
                  },
                },
              }),
            });
          } else {
            await route.fulfill({
              status: 401,
              contentType: 'application/json',
              body: JSON.stringify({
                code: 401,
                message: errorMessage,
              }),
            });
          }
        });
      },

      async mockApiError(path: string, statusCode: number, message: string) {
        await page.route(`**${path}**`, async (route) => {
          await route.fulfill({
            status: statusCode,
            contentType: 'application/json',
            body: JSON.stringify({
              code: statusCode,
              message,
            }),
          });
        });
      },

      async mockApiSuccess(path: string, data: any) {
        await page.route(`**${path}**`, async (route) => {
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({
              code: 0,
              data,
            }),
          });
        });
      },

      async mockApiWithDelay(path: string, data: any, delayMs: number) {
        await page.route(`**${path}**`, async (route) => {
          await new Promise((resolve) => setTimeout(resolve, delayMs));
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({
              code: 0,
              data,
            }),
          });
        });
      },
    };

    await use(mockApi);
  },

  // Mock Bridge fixture
  mockBridge: async ({ page }, use) => {
    const mockBridge: MockBridge = {
      async setup(config?: BridgeMockConfig) {
        await setupBridgeMock(page, config);
      },

      async getVpnState() {
        return bridgeMockHelpers.getVpnState(page);
      },

      async setVpnState(state: VpnState) {
        await bridgeMockHelpers.setVpnState(page, state);
      },

      async simulateVpnError() {
        await bridgeMockHelpers.simulateVpnError(page);
      },

      async getStorage() {
        return bridgeMockHelpers.getStorage(page);
      },

      async getClipboard() {
        return bridgeMockHelpers.getClipboard(page);
      },
    };

    await use(mockBridge);
  },
});

// Export expect
export { expect };

// ============================================================
// Test Data
// ============================================================

export const testData = {
  tunnels: [
    {
      id: 'tunnel-hk-1',
      domain: 'hk1.example.com',
      name: 'Hong Kong 1',
      node: { country: 'HK', city: 'Hong Kong', load: 25 },
    },
    {
      id: 'tunnel-jp-1',
      domain: 'jp1.example.com',
      name: 'Japan 1',
      node: { country: 'JP', city: 'Tokyo', load: 45 },
    },
    {
      id: 'tunnel-us-1',
      domain: 'us1.example.com',
      name: 'United States 1',
      node: { country: 'US', city: 'Los Angeles', load: 70 },
    },
  ],

  user: {
    id: 'test-user-id',
    email: 'test@example.com',
    nickname: 'Test User',
    avatar: '',
    plan: {
      id: 'plan-1',
      name: 'Premium',
      expires_at: '2025-12-31T23:59:59Z',
    },
  },

  // Login test data
  validEmail: 'test@example.com',
  invalidEmail: 'invalid-email',
  validCode: '123456',
  invalidCode: '000000',
};

// ============================================================
// Test Utilities
// ============================================================

export const testUtils = {
  /** Wait for page to fully load */
  async waitForPageLoad(page: Page) {
    await page.waitForLoadState('networkidle');
  },

  /** Wait for loading indicator to disappear */
  async waitForLoadingToDisappear(page: Page) {
    await page
      .waitForSelector('[role="progressbar"]', { state: 'hidden', timeout: 10000 })
      .catch(() => {});
  },

  /** Wait for dialog to open */
  async waitForDialogOpen(page: Page) {
    await page.waitForSelector('[role="dialog"]', { state: 'visible', timeout: 5000 });
  },

  /** Wait for dialog to close */
  async waitForDialogClose(page: Page) {
    await page.waitForSelector('[role="dialog"]', { state: 'hidden', timeout: 5000 });
  },

  /** Take screenshot and save */
  async takeScreenshot(page: Page, name: string) {
    await page.screenshot({ path: `test-results/screenshots/${name}.png`, fullPage: true });
  },

  /** Open login dialog */
  async openLoginDialog(page: Page) {
    // Find and click login button (adapt to actual UI)
    const loginButton = page.locator('button:has-text("登录"), button:has-text("Login")').first();
    await loginButton.click();
    await this.waitForDialogOpen(page);
  },

  /** Fill email in login dialog */
  async fillEmail(page: Page, email: string) {
    const emailInput = page.locator('input[type="email"]').first();
    await emailInput.fill(email);
  },

  /** Fill verification code in login dialog */
  async fillVerificationCode(page: Page, code: string) {
    const codeInput = page.locator('input[inputmode="numeric"]').first();
    await codeInput.fill(code);
  },

  /** Click send code button */
  async clickSendCode(page: Page) {
    const sendButton = page.locator('button:has-text("发送验证码"), button:has-text("Send Code")').first();
    await sendButton.click();
  },

  /** Click verify button */
  async clickVerify(page: Page) {
    const verifyButton = page.locator('button:has-text("验证"), button:has-text("Verify")').first();
    await verifyButton.click();
  },
};

// Re-export types and helpers from other fixtures
export type { VpnState, BridgeMockConfig } from './bridge-mock';
export * from './button-helpers';
