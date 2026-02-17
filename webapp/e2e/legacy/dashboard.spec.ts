/**
 * Dashboard Page E2E Tests (Legacy)
 *
 * Tests dashboard page end-to-end user flows.
 * Migrated to use new test fixtures with Bridge mock.
 */
import { test, expect, testData, testUtils } from '../fixtures/test-base';

test.describe('Dashboard Page', () => {
  test.beforeEach(async ({ page, mockApi }) => {
    // Mock API responses
    await mockApi.mockTunnels(testData.tunnels);
    await mockApi.mockUserInfo(testData.user);
  });

  test('should correctly load and display tunnel list', async ({ page }) => {
    await page.goto('/');

    // Wait for page load
    await testUtils.waitForLoadingToDisappear(page);

    // Check tunnel list
    await expect(page.getByText('Hong Kong 1')).toBeVisible();
    await expect(page.getByText('Japan 1')).toBeVisible();
    await expect(page.getByText('United States 1')).toBeVisible();
  });

  test('should display tunnel load info', async ({ page }) => {
    await page.goto('/');
    await testUtils.waitForLoadingToDisappear(page);

    // Check load percentages displayed
    await expect(page.getByText('25%')).toBeVisible();
    await expect(page.getByText('45%')).toBeVisible();
    await expect(page.getByText('70%')).toBeVisible();
  });

  test('clicking tunnel should select it', async ({ page }) => {
    await page.goto('/');
    await testUtils.waitForLoadingToDisappear(page);

    // Click Hong Kong tunnel
    await page.getByText('Hong Kong 1').click();

    // Check selected state (via Radio button)
    const radioButtons = page.locator('input[type="radio"]');
    await expect(radioButtons.first()).toBeChecked();
  });

  test('refresh button should trigger data refresh', async ({ page, mockApi }) => {
    await page.goto('/');
    await testUtils.waitForLoadingToDisappear(page);

    // Update mock data
    await mockApi.mockTunnels([
      ...testData.tunnels,
      {
        id: 'tunnel-sg-1',
        domain: 'sg1.example.com',
        name: 'Singapore 1',
        node: { country: 'SG', city: 'Singapore', load: 30 },
      },
    ]);

    // Click refresh button
    await page.getByRole('button', { name: /刷新|refresh/i }).click();

    // Wait for new data to load
    await expect(page.getByText('Singapore 1')).toBeVisible({ timeout: 5000 });
  });

  test('empty tunnel list should show empty state', async ({ page, mockApi }) => {
    // Mock empty data
    await mockApi.mockTunnels([]);

    await page.goto('/');
    await testUtils.waitForLoadingToDisappear(page);

    // Check for empty state message
    await expect(page.getByRole('heading')).toBeVisible();
  });
});

test.describe('Dashboard Connection Button', () => {
  test.beforeEach(async ({ page, mockApi }) => {
    await mockApi.mockTunnels(testData.tunnels);
    await mockApi.mockUserInfo(testData.user);
  });

  test('connect button should be disabled when no tunnel selected', async ({ page }) => {
    await page.goto('/');
    await testUtils.waitForLoadingToDisappear(page);

    // Find large connect button
    const connectButton = page
      .locator('button')
      .filter({ hasText: /断开|连接|disconnected/i })
      .first();
    await expect(connectButton).toBeDisabled();
  });

  test('connect button should be enabled after tunnel selection', async ({ page }) => {
    await page.goto('/');
    await testUtils.waitForLoadingToDisappear(page);

    // First select a tunnel
    await page.getByText('Hong Kong 1').click();

    // Check connect button
    const connectButton = page
      .locator('button')
      .filter({ hasText: /断开|连接|disconnected/i })
      .first();
    await expect(connectButton).toBeEnabled();
  });
});

test.describe('Dashboard Advanced Settings', () => {
  test.beforeEach(async ({ page, mockApi }) => {
    await mockApi.mockTunnels(testData.tunnels);
    await mockApi.mockUserInfo(testData.user);
  });

  test('should be able to expand/collapse advanced settings', async ({ page }) => {
    await page.goto('/');
    await testUtils.waitForLoadingToDisappear(page);

    // Click advanced settings button
    const advancedButton = page.getByRole('button', { name: /高级设置|advanced/i });
    await advancedButton.click();

    // Check settings panel expanded
    await expect(page.getByText(/代理规则|proxy/i)).toBeVisible();

    // Click again to collapse
    await advancedButton.click();

    // Settings panel should hide
    await expect(page.getByText(/代理规则|proxy/i)).toBeHidden();
  });

  test('should be able to switch proxy rules', async ({ page }) => {
    await page.goto('/');
    await testUtils.waitForLoadingToDisappear(page);

    // Expand advanced settings
    await page.getByRole('button', { name: /高级设置|advanced/i }).click();

    // Check proxy rule buttons
    const globalButton = page.getByRole('button', { name: /全局|global/i });
    const cnButton = page.getByRole('button', { name: /白名单|whitelist/i });

    await expect(globalButton).toBeVisible();
    await expect(cnButton).toBeVisible();
  });
});

test.describe('Dashboard Responsive Design', () => {
  test('mobile view should display correctly', async ({ page, mockApi }) => {
    await mockApi.mockTunnels(testData.tunnels);

    // Set mobile viewport
    await page.setViewportSize({ width: 375, height: 667 });

    await page.goto('/');
    await testUtils.waitForLoadingToDisappear(page);

    // Check core elements visible
    await expect(page.getByText('Hong Kong 1')).toBeVisible();
  });

  test('tablet view should display correctly', async ({ page, mockApi }) => {
    await mockApi.mockTunnels(testData.tunnels);

    // Set tablet viewport
    await page.setViewportSize({ width: 768, height: 1024 });

    await page.goto('/');
    await testUtils.waitForLoadingToDisappear(page);

    // Check core elements visible
    await expect(page.getByText('Hong Kong 1')).toBeVisible();
  });
});

test.describe('Dashboard Error Handling', () => {
  test('API error should show error message', async ({ page, mockApi }) => {
    // Mock API error
    await mockApi.mockApiError('/api/tunnels', 500, 'Server Error');

    await page.goto('/');

    // Wait for error state
    await expect(page.getByText(/失败|error|failed/i)).toBeVisible({ timeout: 10000 });
  });

  test('network timeout should allow retry', async ({ page, mockApi }) => {
    // First mock error
    await mockApi.mockApiError('/api/tunnels', 500, 'Server Error');

    await page.goto('/');
    await expect(page.getByText(/失败|error|failed/i)).toBeVisible({ timeout: 10000 });

    // Then mock success response
    await mockApi.mockTunnels(testData.tunnels);

    // Click refresh
    await page.getByRole('button', { name: /刷新|refresh/i }).click();

    // Should display data
    await expect(page.getByText('Hong Kong 1')).toBeVisible({ timeout: 5000 });
  });
});
