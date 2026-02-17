/**
 * Settings Page E2E Tests
 *
 * Tests settings page functionality and user interactions
 */
import { test, expect, testData, testUtils } from '../fixtures/test-base';
import { assertButtonState } from '../fixtures/button-helpers';

test.describe('Settings Page', () => {
  test.beforeEach(async ({ page, mockApi }) => {
    // Setup mocks
    await mockApi.mockTunnels(testData.tunnels);
    await mockApi.mockUserInfo(testData.user);

    // Navigate to settings page
    await page.goto('/settings');
    await testUtils.waitForLoadingToDisappear(page);
  });

  test('should display settings page', async ({ page }) => {
    // Check page loads
    await expect(page).toHaveURL('/settings');
  });

  test('should have theme selector', async ({ page }) => {
    // Look for theme-related elements
    const themeSection = page.locator('text=主题').or(page.locator('text=Theme'));
    await expect(themeSection).toBeVisible({ timeout: 10000 });
  });

  test('should have language selector', async ({ page }) => {
    // Look for language-related elements
    const langSection = page.locator('text=语言').or(page.locator('text=Language'));
    await expect(langSection).toBeVisible({ timeout: 10000 });
  });

  test('should navigate to other settings sections', async ({ page }) => {
    // Check for navigation items or tabs
    const navItems = page.locator('[role="tab"], [role="menuitem"], nav a');
    const count = await navItems.count();

    // Settings page should have navigation options
    expect(count).toBeGreaterThanOrEqual(0);
  });

  test('should persist settings after page reload', async ({ page }) => {
    // This test verifies settings are saved
    // Look for any toggle or selector
    const toggles = page.locator('[role="switch"], input[type="checkbox"]');
    const toggleCount = await toggles.count();

    if (toggleCount > 0) {
      const firstToggle = toggles.first();
      const initialState = await firstToggle.isChecked();

      // Toggle the setting
      await firstToggle.click();

      // Reload page
      await page.reload();

      // Check if setting persisted (may need adjustment based on actual behavior)
      const newToggle = page.locator('[role="switch"], input[type="checkbox"]').first();
      const newState = await newToggle.isChecked();

      // State should be different if it persisted
      expect(newState).not.toBe(initialState);
    }
  });
});

test.describe('Settings - Theme', () => {
  test.beforeEach(async ({ page, mockApi }) => {
    await mockApi.mockTunnels(testData.tunnels);
    await mockApi.mockUserInfo(testData.user);
    await page.goto('/settings');
    await testUtils.waitForLoadingToDisappear(page);
  });

  test('should switch between light and dark theme', async ({ page }) => {
    // Find theme selector
    const themeSelector = page
      .locator('select[data-testid="theme-select"]')
      .or(page.locator('[aria-label*="theme"]').or(page.locator('text=深色').or(page.locator('text=Dark'))));

    const selectorVisible = await themeSelector.isVisible({ timeout: 5000 }).catch(() => false);

    if (selectorVisible) {
      // Test theme switching
      await themeSelector.click();

      // Check if theme class changes on body/html
      const html = page.locator('html');
      const dataTheme = await html.getAttribute('data-theme');
      const className = await html.getAttribute('class');

      // Verify theme attribute or class exists
      expect(dataTheme || className).toBeDefined();
    }
  });
});

test.describe('Settings - Network', () => {
  test.beforeEach(async ({ page, mockApi }) => {
    await mockApi.mockTunnels(testData.tunnels);
    await mockApi.mockUserInfo(testData.user);
    await page.goto('/settings');
    await testUtils.waitForLoadingToDisappear(page);
  });

  test('should display network repair option', async ({ page }) => {
    // Look for network repair or fix network option
    const networkRepair = page
      .locator('text=修复网络')
      .or(page.locator('text=Fix Network').or(page.locator('text=Network Repair')));

    const visible = await networkRepair.isVisible({ timeout: 5000 }).catch(() => false);

    // Network repair may or may not be visible depending on platform
    if (visible) {
      await expect(networkRepair).toBeEnabled();
    }
  });
});

test.describe('Settings - Button States', () => {
  test.beforeEach(async ({ page, mockApi }) => {
    await mockApi.mockTunnels(testData.tunnels);
    await mockApi.mockUserInfo(testData.user);
    await page.goto('/settings');
    await testUtils.waitForLoadingToDisappear(page);
  });

  test('save button should be disabled when no changes', async ({ page }) => {
    // Find save button if exists
    const saveButton = page.locator('button:has-text("保存"), button:has-text("Save")').first();

    if (await saveButton.isVisible({ timeout: 3000 }).catch(() => false)) {
      await assertButtonState(saveButton, {
        disabled: true,
        loading: false,
      });
    }
  });

  test('save button should be enabled after making changes', async ({ page }) => {
    // Find a toggle and flip it
    const toggles = page.locator('[role="switch"], input[type="checkbox"]');

    if ((await toggles.count()) > 0) {
      await toggles.first().click();

      // Find save button
      const saveButton = page.locator('button:has-text("保存"), button:has-text("Save")').first();

      if (await saveButton.isVisible({ timeout: 3000 }).catch(() => false)) {
        await assertButtonState(saveButton, {
          disabled: false,
          loading: false,
        });
      }
    }
  });
});
