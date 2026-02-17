/**
 * Button State Assertion Helpers
 *
 * Provides reusable helpers for asserting button states
 * across different user flows (login, VPN control, purchase, settings)
 */

import { expect, type Locator, type Page } from '@playwright/test';

/**
 * Button State Options
 */
export interface ButtonStateOptions {
  /** Expected button text (partial match) */
  text?: string | RegExp;
  /** Expected disabled state */
  disabled?: boolean;
  /** Expected loading state (looks for CircularProgress) */
  loading?: boolean;
  /** Expected visibility */
  visible?: boolean;
  /** Expected enabled state (inverse of disabled) */
  enabled?: boolean;
}

/**
 * Assert button state
 *
 * @example
 * await assertButtonState(sendCodeButton, {
 *   text: '发送验证码',
 *   disabled: false,
 *   loading: false,
 * });
 */
export async function assertButtonState(
  button: Locator,
  options: ButtonStateOptions
): Promise<void> {
  const { text, disabled, enabled, loading, visible } = options;

  // Check visibility
  if (visible !== undefined) {
    if (visible) {
      await expect(button).toBeVisible({ timeout: 5000 });
    } else {
      await expect(button).toBeHidden({ timeout: 5000 });
    }
  }

  // Check text content
  if (text !== undefined) {
    if (typeof text === 'string') {
      await expect(button).toContainText(text, { timeout: 3000 });
    } else {
      await expect(button).toHaveText(text, { timeout: 3000 });
    }
  }

  // Check disabled state
  if (disabled !== undefined) {
    if (disabled) {
      await expect(button).toBeDisabled({ timeout: 3000 });
    } else {
      await expect(button).toBeEnabled({ timeout: 3000 });
    }
  }

  // Check enabled state (inverse of disabled)
  if (enabled !== undefined) {
    if (enabled) {
      await expect(button).toBeEnabled({ timeout: 3000 });
    } else {
      await expect(button).toBeDisabled({ timeout: 3000 });
    }
  }

  // Check loading state (MUI CircularProgress)
  if (loading !== undefined) {
    const spinner = button.locator('.MuiCircularProgress-root');
    if (loading) {
      await expect(spinner).toBeVisible({ timeout: 3000 });
    } else {
      await expect(spinner).not.toBeVisible({ timeout: 3000 });
    }
  }
}

/**
 * Wait for button to transition from loading to ready
 *
 * @example
 * await waitForButtonReady(submitButton);
 */
export async function waitForButtonReady(
  button: Locator,
  timeout = 10000
): Promise<void> {
  // Wait for loading spinner to disappear
  const spinner = button.locator('.MuiCircularProgress-root');
  await expect(spinner).not.toBeVisible({ timeout });

  // Wait for button to be enabled
  await expect(button).toBeEnabled({ timeout: 3000 });
}

/**
 * Wait for button to show loading state
 *
 * @example
 * await waitForButtonLoading(submitButton);
 */
export async function waitForButtonLoading(
  button: Locator,
  timeout = 5000
): Promise<void> {
  const spinner = button.locator('.MuiCircularProgress-root');
  await expect(spinner).toBeVisible({ timeout });
}

/**
 * Find button by text (flexible search)
 *
 * @example
 * const sendButton = await findButtonByText(page, '发送验证码');
 */
export async function findButtonByText(
  page: Page,
  text: string | RegExp
): Promise<Locator> {
  if (typeof text === 'string') {
    return page.locator(`button:has-text("${text}")`).first();
  } else {
    return page.locator('button').filter({ hasText: text }).first();
  }
}

/**
 * Assert countdown timer is running
 *
 * @example
 * await assertCountdownRunning(page, '重新发送', 60);
 */
export async function assertCountdownRunning(
  button: Locator,
  baseText: string,
  expectedSeconds?: number
): Promise<void> {
  // Check button shows countdown text
  const buttonText = await button.textContent();
  expect(buttonText).toMatch(/\d+/); // Contains a number

  if (expectedSeconds !== undefined) {
    // Verify the number is close to expected
    const match = buttonText?.match(/(\d+)/);
    if (match) {
      const seconds = parseInt(match[1], 10);
      expect(seconds).toBeGreaterThan(0);
      expect(seconds).toBeLessThanOrEqual(expectedSeconds);
    }
  }

  // Button should be disabled during countdown
  await expect(button).toBeDisabled();
}

/**
 * Wait for countdown to finish
 *
 * @example
 * await waitForCountdownEnd(resendButton, '重新发送');
 */
export async function waitForCountdownEnd(
  button: Locator,
  expectedText: string,
  timeout = 65000
): Promise<void> {
  // Wait for countdown to finish and button text to change
  await expect(button).toContainText(expectedText, { timeout });
  await expect(button).toBeEnabled({ timeout: 5000 });
}

/**
 * Assert error state is shown
 *
 * @example
 * await assertErrorShown(page, '登录失败');
 */
export async function assertErrorShown(
  page: Page,
  errorText?: string | RegExp
): Promise<void> {
  // Look for MUI Alert with error severity
  const errorAlert = page.locator('.MuiAlert-standardError, [role="alert"]');
  await expect(errorAlert).toBeVisible({ timeout: 5000 });

  if (errorText) {
    await expect(errorAlert).toContainText(errorText as string);
  }
}

/**
 * Assert success state is shown
 *
 * @example
 * await assertSuccessShown(page, '登录成功');
 */
export async function assertSuccessShown(
  page: Page,
  successText?: string | RegExp
): Promise<void> {
  // Look for MUI Alert with success severity
  const successAlert = page.locator('.MuiAlert-standardSuccess');
  await expect(successAlert).toBeVisible({ timeout: 5000 });

  if (successText) {
    await expect(successAlert).toContainText(successText as string);
  }
}

/**
 * Type text character by character (simulates real user typing)
 * Important for WebKit input focus tests
 *
 * @example
 * await typeCharByChar(emailInput, 'test@example.com', 50);
 */
export async function typeCharByChar(
  input: Locator,
  text: string,
  delayMs = 100
): Promise<void> {
  await input.focus();

  for (const char of text) {
    await input.type(char, { delay: delayMs });
  }
}

/**
 * Assert input focus state
 *
 * @example
 * await assertInputFocused(emailInput);
 */
export async function assertInputFocused(input: Locator): Promise<void> {
  await expect(input).toBeFocused({ timeout: 3000 });
}

/**
 * Assert input value and trim behavior
 *
 * @example
 * await assertInputValue(emailInput, 'test@example.com');
 */
export async function assertInputValue(
  input: Locator,
  expectedValue: string
): Promise<void> {
  await expect(input).toHaveValue(expectedValue, { timeout: 3000 });
}
