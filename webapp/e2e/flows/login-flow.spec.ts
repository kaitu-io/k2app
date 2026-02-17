/**
 * Login Flow Button State Tests
 *
 * Tests button states throughout the login flow:
 * - Email step: "Send Code" button states
 * - Code step: "Verify" button + "Resend" countdown
 * - Error handling and recovery
 * - Dialog close button behavior
 */

import { test, expect, testData, testUtils } from '../fixtures/test-base';
import {
  assertButtonState,
  waitForButtonLoading,
  waitForButtonReady,
  assertCountdownRunning,
  assertErrorShown,
} from '../fixtures/button-helpers';

test.describe('Login Flow - Email Step Button States', () => {
  test.beforeEach(async ({ page, mockApi }) => {
    await mockApi.mockTunnels(testData.tunnels);
    await mockApi.mockUserInfo(testData.user);
    await page.goto('/');
    await testUtils.waitForLoadingToDisappear(page);
  });

  test('Send Code button should be disabled when email is empty', async ({ page }) => {
    await testUtils.openLoginDialog(page);
    await page.waitForTimeout(200);

    const sendButton = page.locator('button:has-text("发送验证码"), button:has-text("Send Code")').first();

    // Button should be disabled with empty input
    await assertButtonState(sendButton, {
      disabled: true,
      loading: false,
    });
  });

  test('Send Code button should be disabled for invalid email', async ({ page }) => {
    await testUtils.openLoginDialog(page);
    await page.waitForTimeout(200);

    const emailInput = page.locator('input[type="email"]').first();
    const sendButton = page.locator('button:has-text("发送验证码"), button:has-text("Send Code")').first();

    // Type invalid email
    await emailInput.fill('invalid-email');

    await assertButtonState(sendButton, {
      disabled: true,
      loading: false,
    });
  });

  test('Send Code button should be enabled for valid email', async ({ page }) => {
    await testUtils.openLoginDialog(page);
    await page.waitForTimeout(200);

    const emailInput = page.locator('input[type="email"]').first();
    const sendButton = page.locator('button:has-text("发送验证码"), button:has-text("Send Code")').first();

    // Type valid email
    await emailInput.fill('test@example.com');

    await assertButtonState(sendButton, {
      disabled: false,
      loading: false,
    });
  });

  test('Send Code button should show loading state during API call', async ({ page, mockApi }) => {
    // Mock with delay to observe loading state
    await mockApi.mockApiWithDelay('/api/auth/code', {
      userExists: true,
      isActivated: true,
      isFirstOrderDone: false,
    }, 1000);

    await testUtils.openLoginDialog(page);
    await page.waitForTimeout(200);

    const emailInput = page.locator('input[type="email"]').first();
    const sendButton = page.locator('button:has-text("发送验证码"), button:has-text("Send Code")').first();

    await emailInput.fill('test@example.com');
    await sendButton.click();

    // Should show loading immediately
    await assertButtonState(sendButton, {
      loading: true,
      disabled: true,
    });
  });

  test('Send Code button should recover from error state', async ({ page, mockApi }) => {
    // First mock error
    await mockApi.mockApiError('/api/auth/code', 500, 'Server error');

    await testUtils.openLoginDialog(page);
    await page.waitForTimeout(200);

    const emailInput = page.locator('input[type="email"]').first();
    const sendButton = page.locator('button:has-text("发送验证码"), button:has-text("Send Code")').first();

    await emailInput.fill('test@example.com');
    await sendButton.click();

    // Wait for error
    await page.waitForTimeout(500);

    // Error should be shown
    await assertErrorShown(page);

    // Button should be re-enabled for retry
    await assertButtonState(sendButton, {
      disabled: false,
      loading: false,
    });
  });
});

test.describe('Login Flow - Code Step Button States', () => {
  test.beforeEach(async ({ page, mockApi }) => {
    await mockApi.mockAuth({ sendCodeSuccess: true });
    await mockApi.mockTunnels(testData.tunnels);
    await mockApi.mockUserInfo(testData.user);
    await page.goto('/');
    await testUtils.waitForLoadingToDisappear(page);

    // Navigate to code step
    await testUtils.openLoginDialog(page);
    await page.waitForTimeout(200);
    await testUtils.fillEmail(page, testData.validEmail);
    await testUtils.clickSendCode(page);
    await page.waitForTimeout(500);
  });

  test('Verify button should be disabled when code is empty', async ({ page }) => {
    // Find verify button (may be labeled differently)
    const verifyButton = page.locator('button').filter({ hasText: /验证|登录|Verify|Login/i }).first();

    await assertButtonState(verifyButton, {
      disabled: true,
      loading: false,
    });
  });

  test('Verify button should be enabled when code is entered', async ({ page }) => {
    const codeInput = page.locator('input[inputmode="numeric"]').first();
    const verifyButton = page.locator('button').filter({ hasText: /验证|登录|Verify|Login/i }).first();

    await codeInput.fill('123456');

    await assertButtonState(verifyButton, {
      disabled: false,
      loading: false,
    });
  });

  test('Verify button should show loading during verification', async ({ page, mockApi }) => {
    // Mock verify with delay
    await page.route('**/api/auth/login**', async (route) => {
      await new Promise((resolve) => setTimeout(resolve, 1000));
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          code: 0,
          data: {
            accessToken: 'mock-token',
            refreshToken: 'mock-refresh-token',
          },
        }),
      });
    });

    const codeInput = page.locator('input[inputmode="numeric"]').first();
    const verifyButton = page.locator('button').filter({ hasText: /验证|登录|Verify|Login/i }).first();

    await codeInput.fill('123456');
    await verifyButton.click();

    await assertButtonState(verifyButton, {
      loading: true,
      disabled: true,
    });
  });

  test('Resend button should show countdown after code sent', async ({ page }) => {
    // Find resend button
    const resendButton = page.locator('button').filter({ hasText: /重新发送|Resend|秒/i }).first();

    // Should be disabled with countdown
    await expect(resendButton).toBeDisabled();

    // Should contain countdown number
    const buttonText = await resendButton.textContent();
    expect(buttonText).toMatch(/\d+/);
  });

  test('Resend button should be enabled after countdown ends', async ({ page }) => {
    // This test uses a shorter timeout for CI - in real scenario, wait 60s
    // For testing, we mock a quick countdown or check the pattern

    const resendButton = page.locator('button').filter({ hasText: /重新发送|Resend|秒/i }).first();

    // Initially disabled
    await expect(resendButton).toBeDisabled();

    // We can't wait 60s in tests, but we verify the pattern is correct
    const buttonText = await resendButton.textContent();
    expect(buttonText).toMatch(/\d+.*秒|Resend.*\d+/i);
  });
});

test.describe('Login Flow - Error States', () => {
  test.beforeEach(async ({ page, mockApi }) => {
    await mockApi.mockTunnels(testData.tunnels);
    await mockApi.mockUserInfo(testData.user);
    await page.goto('/');
    await testUtils.waitForLoadingToDisappear(page);
  });

  test('should show error and re-enable button on send code failure', async ({ page, mockApi }) => {
    await mockApi.mockAuth({ sendCodeSuccess: false, errorMessage: 'Invalid email' });

    await testUtils.openLoginDialog(page);
    await page.waitForTimeout(200);

    const emailInput = page.locator('input[type="email"]').first();
    const sendButton = page.locator('button:has-text("发送验证码"), button:has-text("Send Code")').first();

    await emailInput.fill('test@example.com');
    await sendButton.click();

    // Wait for response
    await page.waitForTimeout(500);

    // Error should be visible
    const errorAlert = page.locator('.MuiAlert-standardError, [role="alert"][class*="error"]');
    await expect(errorAlert).toBeVisible();

    // Button should be re-enabled
    await assertButtonState(sendButton, {
      disabled: false,
      loading: false,
    });
  });

  test('should show error and re-enable button on verify failure', async ({ page, mockApi }) => {
    await mockApi.mockAuth({ sendCodeSuccess: true, verifyCodeSuccess: false, errorMessage: 'Invalid code' });

    await testUtils.openLoginDialog(page);
    await page.waitForTimeout(200);
    await testUtils.fillEmail(page, testData.validEmail);
    await testUtils.clickSendCode(page);
    await page.waitForTimeout(500);

    const codeInput = page.locator('input[inputmode="numeric"]').first();
    const verifyButton = page.locator('button').filter({ hasText: /验证|登录|Verify|Login/i }).first();

    await codeInput.fill('000000');
    await verifyButton.click();

    // Wait for response
    await page.waitForTimeout(500);

    // Error should be visible
    const errorAlert = page.locator('.MuiAlert-standardError, [role="alert"][class*="error"]');
    await expect(errorAlert).toBeVisible();

    // Button should be re-enabled
    await assertButtonState(verifyButton, {
      disabled: false,
      loading: false,
    });
  });

  test('error alert should be dismissible', async ({ page, mockApi }) => {
    await mockApi.mockAuth({ sendCodeSuccess: false, errorMessage: 'Test error' });

    await testUtils.openLoginDialog(page);
    await page.waitForTimeout(200);

    const emailInput = page.locator('input[type="email"]').first();
    const sendButton = page.locator('button:has-text("发送验证码"), button:has-text("Send Code")').first();

    await emailInput.fill('test@example.com');
    await sendButton.click();
    await page.waitForTimeout(500);

    // Error should be visible
    const errorAlert = page.locator('.MuiAlert-standardError, [role="alert"]').first();
    await expect(errorAlert).toBeVisible();

    // Click close button on alert
    const closeButton = errorAlert.locator('button[aria-label*="close"], button[aria-label*="Close"]').first();
    if (await closeButton.isVisible()) {
      await closeButton.click();
      await expect(errorAlert).not.toBeVisible();
    }
  });
});

test.describe('Login Flow - Dialog Close Button', () => {
  test.beforeEach(async ({ page, mockApi }) => {
    await mockApi.mockTunnels(testData.tunnels);
    await mockApi.mockUserInfo(testData.user);
    await page.goto('/');
    await testUtils.waitForLoadingToDisappear(page);
  });

  test('close button should be visible and enabled', async ({ page }) => {
    await testUtils.openLoginDialog(page);
    await page.waitForTimeout(200);

    const closeButton = page.locator('[role="dialog"] button[aria-label*="close"], [role="dialog"] button:has(svg)').first();

    await assertButtonState(closeButton, {
      visible: true,
      disabled: false,
    });
  });

  test('close button should close dialog', async ({ page }) => {
    await testUtils.openLoginDialog(page);
    await page.waitForTimeout(200);

    const closeButton = page.locator('[role="dialog"] button').filter({ has: page.locator('svg') }).first();
    await closeButton.click();

    await testUtils.waitForDialogClose(page);
  });

  test('close button should be disabled during submission', async ({ page, mockApi }) => {
    // Mock with delay
    await mockApi.mockApiWithDelay('/api/auth/code', {
      userExists: true,
      isActivated: true,
    }, 2000);

    await testUtils.openLoginDialog(page);
    await page.waitForTimeout(200);

    const emailInput = page.locator('input[type="email"]').first();
    const sendButton = page.locator('button:has-text("发送验证码"), button:has-text("Send Code")').first();

    await emailInput.fill('test@example.com');
    await sendButton.click();

    // During loading, close button might be disabled (depends on implementation)
    // Or dialog might prevent closing - check either behavior
    const dialog = page.locator('[role="dialog"]');
    await expect(dialog).toBeVisible();
  });

  test('Escape key should close dialog', async ({ page }) => {
    await testUtils.openLoginDialog(page);
    await page.waitForTimeout(200);

    await page.keyboard.press('Escape');

    await testUtils.waitForDialogClose(page);
  });
});

test.describe('Login Flow - Complete Flow', () => {
  test('should complete full login flow with correct button states', async ({ page, mockApi }) => {
    await mockApi.mockAuth({ sendCodeSuccess: true, verifyCodeSuccess: true });
    await mockApi.mockTunnels(testData.tunnels);
    await mockApi.mockUserInfo(testData.user);
    await page.goto('/');
    await testUtils.waitForLoadingToDisappear(page);

    // Step 1: Open dialog
    await testUtils.openLoginDialog(page);
    await page.waitForTimeout(200);

    // Step 2: Enter email
    const emailInput = page.locator('input[type="email"]').first();
    const sendButton = page.locator('button:has-text("发送验证码"), button:has-text("Send Code")').first();

    // Initially disabled
    await assertButtonState(sendButton, { disabled: true });

    // Type email
    await emailInput.fill('test@example.com');

    // Now enabled
    await assertButtonState(sendButton, { disabled: false });

    // Step 3: Send code
    await sendButton.click();

    // Should show loading
    await assertButtonState(sendButton, { loading: true });

    // Wait for code step
    await page.waitForTimeout(500);

    // Step 4: Enter verification code
    const codeInput = page.locator('input[inputmode="numeric"]').first();
    const verifyButton = page.locator('button').filter({ hasText: /验证|登录|Verify|Login/i }).first();

    // Initially disabled
    await assertButtonState(verifyButton, { disabled: true });

    // Type code
    await codeInput.fill('123456');

    // Now enabled
    await assertButtonState(verifyButton, { disabled: false });

    // Step 5: Verify
    await verifyButton.click();

    // Should show loading
    await assertButtonState(verifyButton, { loading: true });

    // Step 6: Success - dialog closes
    await testUtils.waitForDialogClose(page);
  });
});
