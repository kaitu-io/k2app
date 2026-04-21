import { test, expect } from '@playwright/test';

const MOBILE_VIEWPORT = { width: 390, height: 844 } as const;

/**
 * E2E: Purchase page must gate WeChat Android webview users to a
 * "open in browser" guide before the purchase flow ever renders.
 *
 * This test is our strongest guarantee because it:
 *   1. Runs a real Chromium with an isolated context (no shared cache).
 *   2. Overrides the UA at the browser context level (every request sees it).
 *   3. Asserts DOM visibility AND the inline zIndex that guarantees the
 *      overlay stacks above CookieConsent (9999) and the Chatwoot widget
 *      (2147483000).
 *
 * If this passes, we have verified the stack end-to-end short of a physical
 * WeChat Android device — which we cannot access.
 */

const WECHAT_ANDROID_UA =
  'Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/107.0.5304.141 Mobile Safari/537.36 MMWEBID/1234 MicroMessenger/8.0.32.2300(0x28002036) WeChat/arm64 Weixin NetType/WIFI Language/zh_CN';

// Use a mobile viewport with the WeChat UA for the WeChat-path tests.
test.describe('WeChat Android — purchase gate', () => {
  test.use({
    userAgent: WECHAT_ANDROID_UA,
    viewport: MOBILE_VIEWPORT,
    isMobile: true,
    hasTouch: true,
  });

  test('shows the guide overlay at /zh-CN/purchase', async ({ page }) => {
    await page.goto('/zh-CN/purchase', { waitUntil: 'domcontentloaded' });

    // The localized title must be visible.
    await expect(page.getByText('请在浏览器中打开此页面')).toBeVisible();
    await expect(page.getByText('点击右上角的「···」菜单')).toBeVisible();
    await expect(page.getByText('选择「在浏览器打开」')).toBeVisible();

    // The overlay's zIndex is the contract that beats CookieConsent / Chatwoot.
    const overlay = page.locator('div[style*="2147483647"]');
    await expect(overlay).toBeVisible();
    const zIndex = await overlay.evaluate((el) => getComputedStyle(el).zIndex);
    expect(zIndex).toBe('2147483647');

    // The purchase page's own <h1> must NOT be visible — the overlay owns the viewport.
    // (Use role to avoid matching the <title> tag, which is always "hidden" per DOM.)
    await expect(page.getByRole('heading', { name: '购买专业版计划' })).toHaveCount(0);

    // Visual evidence artifact.
    await page.screenshot({ path: 'test-results/wechat-gate-zh-CN.png', fullPage: false });
  });

  test('shows the guide overlay at /en-US/purchase', async ({ page }) => {
    await page.goto('/en-US/purchase', { waitUntil: 'domcontentloaded' });

    await expect(page.getByText('Please open this page in your browser')).toBeVisible();
    await expect(page.getByText('Tap the "···" menu in the top-right corner')).toBeVisible();

    const overlay = page.locator('div[style*="2147483647"]');
    await expect(overlay).toBeVisible();

    await page.screenshot({ path: 'test-results/wechat-gate-en-US.png', fullPage: false });
  });
});

// Regular Chrome on Android — must NOT be blocked.
test.describe('Chrome Android — purchase flow passes through', () => {
  test.use({
    viewport: MOBILE_VIEWPORT,
    isMobile: true,
    hasTouch: true,
  });

  test('regular mobile Chrome does NOT see the guide overlay', async ({ page }) => {
    await page.goto('/zh-CN/purchase', { waitUntil: 'domcontentloaded' });

    // The overlay must not render.
    await expect(page.getByText('请在浏览器中打开此页面')).toBeHidden();
    await expect(page.locator('div[style*="2147483647"]')).toHaveCount(0);

    // The actual purchase page must render (target <h1> specifically —
    // `getByText` would also match the <title> tag in strict mode).
    await expect(page.getByRole('heading', { name: '购买专业版计划' })).toBeVisible();
  });
});
