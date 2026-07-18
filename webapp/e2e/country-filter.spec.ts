/**
 * Auto-pick country exclusion filter — E2E walkthrough.
 *
 * Covers the user-visible states from the approved design
 * (docs/superpowers/specs/2026-07-17-auto-select-country-filter-design.md):
 *   A. Default Auto row: grey funnel, no badge, default subtitle
 *   B. Filter dialog: one row per country (count-desc), checkbox = exclude
 *   C. Active filter: primary funnel + badge, "已排除 N" subtitle,
 *      excluded-country rows keep working for manual selection (chip, no hide)
 *
 * Persistence across restart is covered by connection.store unit tests —
 * the e2e bridge-mock storage is in-memory per page load.
 */
import { test, expect, testUtils } from './fixtures/test-base';

const tunnels = [
  {
    id: 1,
    domain: 'hk1.example.com',
    name: 'HK-01',
    serverUrl: 'k2v5://hk1.example.com:443',
    recommendScore: 0.9,
    node: { country: 'HK', region: 'hk', ipv4: '1.1.1.1', ipv6: '', load: 25 },
  },
  {
    id: 2,
    domain: 'hk2.example.com',
    name: 'HK-02',
    serverUrl: 'k2v5://hk2.example.com:443',
    recommendScore: 0.8,
    node: { country: 'HK', region: 'hk', ipv4: '1.1.1.2', ipv6: '', load: 30 },
  },
  {
    id: 3,
    domain: 'jp1.example.com',
    name: 'Tokyo-01',
    serverUrl: 'k2v5://jp1.example.com:443',
    recommendScore: 0.95,
    node: { country: 'JP', region: 'jp', ipv4: '2.2.2.2', ipv6: '', load: 45 },
  },
  {
    id: 4,
    domain: 'sg1.example.com',
    name: 'SG-01',
    serverUrl: 'k2v5://sg1.example.com:443',
    recommendScore: 0.85,
    node: { country: 'SG', region: 'sg', ipv4: '3.3.3.3', ipv6: '', load: 50 },
  },
];

test.describe('Auto-pick country exclusion filter', () => {
  test.beforeEach(async ({ page, mockApi, mockBridge }) => {
    // Catch-all FIRST (later-registered routes take precedence in Playwright):
    // any unmocked cloud API call returns an empty success instead of hitting
    // the real backend — a real 401 would trigger the app's logout side-effect
    // and lock the tunnel list behind the login overlay.
    await page.route('**/api/**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ code: 0, data: {} }),
      });
    });
    // The versioned tunnels endpoint actually used by CloudTunnelList
    // (the fixture's mockTunnels pattern `**/api/tunnels**` predates it).
    await page.route('**/api/v20260717/tunnels**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ code: 0, data: { items: tunnels, echConfigList: '' } }),
      });
    });
    await mockApi.mockTunnels(tunnels);
    await mockApi.mockUserInfo({
      id: 'test-user-id',
      email: 'test@example.com',
      nickname: 'Test User',
    });
    await mockBridge.setup({ initialVpnState: 'disconnected' });
    // Seed auth tokens into the bridge-mock storage (keys from auth-service.ts)
    // so the tunnel list renders unlocked. The stale `authenticatedPage`
    // fixture writes legacy localStorage keys the app no longer reads.
    await page.addInitScript(() => {
      const trySet = () => {
        const storage = (window as any)._platform?.storage;
        if (storage) {
          void storage.set('k2.auth.token', 'mock-access-token');
          void storage.set('k2.auth.refresh', 'mock-refresh-token');
        } else {
          setTimeout(trySet, 0);
        }
      };
      trySet();
    });
    await page.goto('/');
    await testUtils.waitForLoadingToDisappear(page);
  });

  test('walkthrough: default → dialog → exclude HK → chip + badge → manual still works', async ({ page }) => {
    // A. Default state: funnel visible, no badge, default subtitle.
    const filterBtn = page.getByTestId('auto-country-filter-btn');
    await expect(filterBtn).toBeVisible();
    await expect(page.getByTestId('auto-row-secondary')).toHaveText('自动选择更优的节点');
    await testUtils.takeScreenshot(page, 'country-filter-01-default');

    // Opening the dialog must NOT select the Auto row (stopPropagation).
    const autoRadio = page.locator('input[type="radio"][value="__auto__"]');
    const autoWasChecked = await autoRadio.isChecked();
    await filterBtn.click();
    await testUtils.waitForDialogOpen(page);
    expect(await autoRadio.isChecked()).toBe(autoWasChecked);

    // B. Dialog: count-desc rows (HK ×2 first), node counts shown.
    const dialog = page.locator('[role="dialog"]');
    await expect(dialog.getByText('排除国家/地区')).toBeVisible();
    await expect(dialog.getByText('2 个节点')).toBeVisible();
    await testUtils.takeScreenshot(page, 'country-filter-02-dialog');

    // Exclude Hong Kong: checkbox reflects immediately.
    await dialog.getByText('香港').click();
    await expect(dialog.locator('input[type="checkbox"]').first()).toBeChecked();
    await testUtils.takeScreenshot(page, 'country-filter-03-dialog-checked');

    await page.getByTestId('country-filter-done').click();
    await testUtils.waitForDialogClose(page);

    // C. Active filter: subtitle switches, badge=1, HK rows keep chip but stay listed.
    await expect(page.getByTestId('auto-row-secondary')).toHaveText('已排除 1 个国家/地区');
    await expect(filterBtn.locator('.MuiBadge-badge')).toHaveText('1');
    await expect(page.getByTestId('auto-excluded-chip')).toHaveCount(2);
    await expect(page.getByText('HK-01')).toBeVisible();
    await testUtils.takeScreenshot(page, 'country-filter-04-filtered');

    // Manual selection of an excluded country still works (escape hatch).
    await page.getByText('HK-01').click();
    await expect(
      page.locator('input[type="radio"][value="hk1.example.com"]'),
    ).toBeChecked();
    await testUtils.takeScreenshot(page, 'country-filter-05-manual-excluded-ok');

    // Clear from the dialog restores the default state.
    await filterBtn.click();
    await testUtils.waitForDialogOpen(page);
    await page.getByTestId('country-filter-clear').click();
    await page.getByTestId('country-filter-done').click();
    await testUtils.waitForDialogClose(page);
    await expect(page.getByTestId('auto-row-secondary')).toHaveText('自动选择更优的节点');
    await expect(page.getByTestId('auto-excluded-chip')).toHaveCount(0);
  });
});
