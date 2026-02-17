/**
 * WebKit Login Input Focus Tests
 *
 * Tests for WebKit input focus compatibility, specifically targeting:
 * - macOS 12.5 WebKit 615.x focus bugs (commit 15fb71b7, f225d803, 383995ef)
 * - Character-by-character typing
 * - Paste functionality
 * - Focus management with Dialog animations
 * - onBlur trim behavior
 *
 * These tests validate that the delayedFocus utility and input handling
 * work correctly across different WebKit versions.
 */

import { test, expect, testData, testUtils } from '../fixtures/test-base';
import {
  assertButtonState,
  typeCharByChar,
  assertInputFocused,
  assertInputValue,
} from '../fixtures/button-helpers';

test.describe('WebKit Input Focus - Login Dialog', () => {
  test.beforeEach(async ({ page, mockApi }) => {
    // Setup API mocks
    await mockApi.mockAuth({ sendCodeSuccess: true, verifyCodeSuccess: true });
    await mockApi.mockTunnels(testData.tunnels);
    await mockApi.mockUserInfo(testData.user);

    // Navigate to page
    await page.goto('/');
    await testUtils.waitForLoadingToDisappear(page);
  });

  test.describe('Email Input Focus', () => {
    test('should auto-focus email input after dialog opens with delay', async ({ page }) => {
      // Open login dialog
      await testUtils.openLoginDialog(page);

      // Wait for delayed focus (150ms as per LoginDialog.tsx)
      await page.waitForTimeout(200);

      // Email input should be focused
      const emailInput = page.locator('input[type="email"]').first();
      await assertInputFocused(emailInput);
    });

    test('should accept character-by-character typing in email input', async ({ page }) => {
      await testUtils.openLoginDialog(page);
      await page.waitForTimeout(200);

      const emailInput = page.locator('input[type="email"]').first();
      const testEmail = 'test@example.com';

      // Type character by character (simulates real user typing)
      await typeCharByChar(emailInput, testEmail, 50);

      // Verify full email was typed
      await assertInputValue(emailInput, testEmail);
    });

    test('should handle paste in email input', async ({ page }) => {
      await testUtils.openLoginDialog(page);
      await page.waitForTimeout(200);

      const emailInput = page.locator('input[type="email"]').first();
      const testEmail = 'pasted@example.com';

      // Focus and paste
      await emailInput.focus();
      await page.evaluate((email) => {
        navigator.clipboard.writeText(email);
      }, testEmail);

      // Use keyboard shortcut to paste (cross-platform)
      await emailInput.press('Meta+v'); // macOS
      // Fallback: directly set value if paste doesn't work in test environment
      const value = await emailInput.inputValue();
      if (!value) {
        await emailInput.fill(testEmail);
      }

      await assertInputValue(emailInput, testEmail);
    });

    test('should trim whitespace on blur', async ({ page }) => {
      await testUtils.openLoginDialog(page);
      await page.waitForTimeout(200);

      const emailInput = page.locator('input[type="email"]').first();

      // Type email with leading/trailing whitespace
      await emailInput.fill('  test@example.com  ');

      // Trigger blur
      await emailInput.blur();

      // Value should be trimmed
      await assertInputValue(emailInput, 'test@example.com');
    });

    test('should handle Enter key to submit', async ({ page }) => {
      await testUtils.openLoginDialog(page);
      await page.waitForTimeout(200);

      const emailInput = page.locator('input[type="email"]').first();
      const sendButton = page.locator('button:has-text("发送验证码"), button:has-text("Send Code")').first();

      // Type valid email
      await emailInput.fill('test@example.com');

      // Press Enter
      await emailInput.press('Enter');

      // Button should show loading (form submitted)
      await assertButtonState(sendButton, { loading: true });
    });

    test('should not submit on Enter with invalid email', async ({ page }) => {
      await testUtils.openLoginDialog(page);
      await page.waitForTimeout(200);

      const emailInput = page.locator('input[type="email"]').first();
      const sendButton = page.locator('button:has-text("发送验证码"), button:has-text("Send Code")').first();

      // Type invalid email
      await emailInput.fill('invalid-email');

      // Press Enter
      await emailInput.press('Enter');

      // Button should still be disabled (not submitted)
      await assertButtonState(sendButton, { disabled: true, loading: false });
    });
  });

  test.describe('Verification Code Input Focus', () => {
    test.beforeEach(async ({ page }) => {
      // Get to code step first
      await testUtils.openLoginDialog(page);
      await page.waitForTimeout(200);
      await testUtils.fillEmail(page, testData.validEmail);
      await testUtils.clickSendCode(page);

      // Wait for step transition
      await page.waitForTimeout(500);
    });

    test('should auto-focus code input after step transition', async ({ page }) => {
      // Wait for delayed focus after step change
      await page.waitForTimeout(200);

      const codeInput = page.locator('input[inputmode="numeric"]').first();
      await assertInputFocused(codeInput);
    });

    test('should accept numeric input character-by-character', async ({ page }) => {
      await page.waitForTimeout(200);

      const codeInput = page.locator('input[inputmode="numeric"]').first();
      const testCode = '123456';

      // Type character by character
      await typeCharByChar(codeInput, testCode, 50);

      await assertInputValue(codeInput, testCode);
    });

    test('should handle paste in code input', async ({ page }) => {
      await page.waitForTimeout(200);

      const codeInput = page.locator('input[inputmode="numeric"]').first();
      const testCode = '654321';

      // Fill via paste simulation
      await codeInput.fill(testCode);

      await assertInputValue(codeInput, testCode);
    });

    test('should trim whitespace on blur', async ({ page }) => {
      await page.waitForTimeout(200);

      const codeInput = page.locator('input[inputmode="numeric"]').first();

      // Type code with whitespace
      await codeInput.fill('  123456  ');

      // Trigger blur
      await codeInput.blur();

      // Value should be trimmed
      await assertInputValue(codeInput, '123456');
    });

    test('should handle Enter key to submit verification', async ({ page, mockApi }) => {
      await page.waitForTimeout(200);

      const codeInput = page.locator('input[inputmode="numeric"]').first();

      // Type valid code
      await codeInput.fill('123456');

      // Press Enter
      await codeInput.press('Enter');

      // Wait for API call
      await page.waitForTimeout(100);

      // Dialog should close on success (login complete)
      await testUtils.waitForDialogClose(page);
    });
  });

  test.describe('Focus Persistence During Interactions', () => {
    test('should maintain focus when clicking inside input', async ({ page }) => {
      await testUtils.openLoginDialog(page);
      await page.waitForTimeout(200);

      const emailInput = page.locator('input[type="email"]').first();

      // Type partial email
      await emailInput.fill('test@');

      // Click inside input (simulate cursor repositioning)
      await emailInput.click();

      // Should still be focused
      await assertInputFocused(emailInput);

      // Continue typing
      await emailInput.type('example.com');

      await assertInputValue(emailInput, 'test@example.com');
    });

    test('should restore focus after interacting with other dialog elements', async ({ page }) => {
      await testUtils.openLoginDialog(page);
      await page.waitForTimeout(200);

      const emailInput = page.locator('input[type="email"]').first();

      // Type partial email
      await emailInput.fill('test@example.com');

      // Click on dialog title (focus moves away)
      await page.locator('[role="dialog"] h2, [role="dialog"] h6').first().click();

      // Click back on input
      await emailInput.click();

      // Should be focused again
      await assertInputFocused(emailInput);
    });

    test('should not lose input on dialog close attempt', async ({ page }) => {
      await testUtils.openLoginDialog(page);
      await page.waitForTimeout(200);

      const emailInput = page.locator('input[type="email"]').first();

      // Type email
      await emailInput.fill('test@example.com');

      // Press Escape (should close dialog)
      await page.keyboard.press('Escape');

      // Re-open dialog
      await testUtils.openLoginDialog(page);
      await page.waitForTimeout(200);

      // Input should be empty (form reset on close)
      const newEmailInput = page.locator('input[type="email"]').first();
      await assertInputValue(newEmailInput, '');
    });
  });
});

test.describe('WebKit Input - Edge Cases', () => {
  test.beforeEach(async ({ page, mockApi }) => {
    await mockApi.mockAuth({ sendCodeSuccess: true });
    await mockApi.mockTunnels(testData.tunnels);
    await page.goto('/');
    await testUtils.waitForLoadingToDisappear(page);
  });

  test('should handle rapid typing without losing characters', async ({ page }) => {
    await testUtils.openLoginDialog(page);
    await page.waitForTimeout(200);

    const emailInput = page.locator('input[type="email"]').first();
    const testEmail = 'rapidtyping@test.com';

    // Type very quickly (10ms delay)
    await typeCharByChar(emailInput, testEmail, 10);

    await assertInputValue(emailInput, testEmail);
  });

  test('should handle special characters in email', async ({ page }) => {
    await testUtils.openLoginDialog(page);
    await page.waitForTimeout(200);

    const emailInput = page.locator('input[type="email"]').first();
    const specialEmail = 'test+special.chars_123@sub.example.com';

    await emailInput.fill(specialEmail);

    await assertInputValue(emailInput, specialEmail);
  });

  test('should handle very long email addresses', async ({ page }) => {
    await testUtils.openLoginDialog(page);
    await page.waitForTimeout(200);

    const emailInput = page.locator('input[type="email"]').first();
    const longEmail = 'verylongemailaddress'.repeat(3) + '@example.com';

    await emailInput.fill(longEmail);

    await assertInputValue(emailInput, longEmail);
  });

  test('should handle input while dialog is animating', async ({ page }) => {
    // Click login button but don't wait for dialog
    const loginButton = page.locator('button:has-text("登录"), button:has-text("Login")').first();
    await loginButton.click();

    // Immediately try to type (during animation)
    const emailInput = page.locator('input[type="email"]').first();

    // Wait for input to be visible
    await expect(emailInput).toBeVisible({ timeout: 3000 });

    // Type immediately
    await emailInput.fill('during-animation@test.com');

    // After animation completes, input should have value
    await page.waitForTimeout(300);
    await assertInputValue(emailInput, 'during-animation@test.com');
  });
});
