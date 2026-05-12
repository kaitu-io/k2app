/**
 * App-Bypass E2E Tests
 *
 * Covers three user-facing flows for the "不走代理的应用" feature:
 *   1. Entry visible under Advanced Settings + navigates to /app-bypass
 *   2. Manual-add adds an entry and the count persists back on the Dashboard
 *   3. Advanced Settings locks (shows disconnect-prompt) when VPN is connected
 *
 * KNOWN LIMITATIONS (documented for future-self):
 *
 *   - Feature flag gate: `appConfig.features.appBypass` is `false` by default
 *     (see `webapp/src/config/apps.ts`). The Advanced-Settings entry and the
 *     /app-bypass route are gated on this flag, so all three tests are
 *     expected to FAIL until the flag is flipped to `true` in beta builds.
 *
 *   - `useVPNMachineStore` is an ES-module export, NOT a window global. The
 *     `page.evaluate(() => window.useVPNMachineStore.setState(...))` in the
 *     third test will throw "useVPNMachineStore is undefined" unless the
 *     store is explicitly exposed on `window` for E2E (or until we add an
 *     in-app debug hook). Leaving it as a marker for the future.
 *
 *   - The Dashboard's "高级设置" button only renders once stores are
 *     initialized. The `test-base` fixture (`fixtures/test-base.ts`) sets up
 *     a Bridge mock + idle VPN state by default, which is enough for the
 *     button to render, but the entire flow relies on the feature-flag fix
 *     above.
 *
 * When the flag flips, this spec becomes the canonical regression net.
 */
import { test, expect } from '@playwright/test';

test('app-bypass entry visible + navigates to page', async ({ page }) => {
  await page.goto('/');
  await page.getByText('高级设置').click();
  await expect(page.getByText('不走代理的应用')).toBeVisible();
  await page.getByText('不走代理的应用').click();
  await expect(page).toHaveURL(/\/app-bypass/);
  await expect(page.getByRole('heading', { name: '不走代理的应用' })).toBeVisible();
});

test('manual-add flow adds entry and persists in count', async ({ page }) => {
  await page.goto('/app-bypass');
  await page.getByText('+ 手动添加').click();
  await page.getByPlaceholder(/chrome\.exe/).fill('test-process.exe');
  await page.getByText('添加').click();
  await expect(page.getByText('test-process.exe')).toBeVisible();
  await page.goto('/');
  await page.getByText('高级设置').click();
  await expect(page.getByText('1 个')).toBeVisible();
});

test('Advanced Settings locks when VPN connected', async ({ page }) => {
  await page.goto('/');
  await page.getByText('高级设置').click();
  // Inject mock VPN connected state.
  // NOTE: useVPNMachineStore is a module export, not a window global. This
  // call will only work once we expose it (e.g. in main.tsx behind an E2E
  // flag) or rewrite this test to use the bridge mock + dispatch path.
  await page.evaluate(() => {
    (window as any).useVPNMachineStore?.setState({ state: 'connected' });
  });
  await expect(page.getByText(/请先断开/)).toBeVisible();
  await expect(page.getByText('断开 VPN')).toBeVisible();
});
