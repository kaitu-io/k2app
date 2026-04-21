import { test, expect, Page } from '@playwright/test';

/**
 * E2E tests for the delegate-pay v2 flows.
 *
 * Covers:
 *  A. Empty-state inline delegate pay (no delegate set → fill inline email + send)
 *  B. Set-state one-click delegate pay (delegate already set → primary CTA)
 *  C. /account/delegate management (set + modify + remove)
 *
 * Backend is fully mocked via page.route(). No real API needed.
 */

const TEST_EMAIL = 'bob@example.com';
const TEST_DELEGATE_EMAIL = 'alice@example.com';
const TEST_DELEGATE_EMAIL_2 = 'charlie@example.com';

async function setupCommonMocks(
  page: Page,
  opts: {
    delegate?: { email: string; setAt: number } | null;
    dynamicDelegate?: boolean;
  } = {}
) {
  const { delegate: initialDelegate = null, dynamicDelegate = false } = opts;

  // Mutable copy so PUT/DELETE can update subsequent GETs when dynamicDelegate is true
  let currentDelegate: { email: string; setAt: number } | null = initialDelegate;

  // /api/user/info — both AuthContext.getCurrentUser and PurchaseClient.getUserProfile
  // hit this endpoint. Return a shape that satisfies both callers.
  await page.route('**/api/user/info', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        code: 0,
        data: {
          id: 1,
          uuid: 'user-bob',
          email: TEST_EMAIL,
          isAdmin: false,
          roles: 1,
          expiredAt: 0,
          isFirstOrderDone: false,
          loginIdentifies: [{ type: 'email', value: TEST_EMAIL }],
          deviceCount: 1,
        },
      }),
    });
  });

  // /api/app/config — minimal
  await page.route('**/api/app/config', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ code: 0, data: {} }),
    });
  });

  // /api/plans — must be ListResult<Plan> shape (items array)
  await page.route('**/api/plans*', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        code: 0,
        data: {
          items: [
            {
              pid: 'pro-1y',
              label: '1 年 Pro',
              price: 4990,
              originPrice: 4990,
              month: 12,
              highlight: true,
            },
          ],
        },
      }),
    });
  });

  // /api/user/delegate — GET / PUT / DELETE
  await page.route('**/api/user/delegate', async (route) => {
    const method = route.request().method();

    if (method === 'GET') {
      if (currentDelegate) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ code: 0, data: currentDelegate }),
        });
      } else {
        // No delegate — backend returns 404 in the code field; api.request()
        // throws ApiError(code=404) which DelegateClient / PurchaseClient
        // both recognize as "empty state".
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ code: 404, message: 'no delegate' }),
        });
      }
      return;
    }

    if (method === 'PUT') {
      const body = JSON.parse(route.request().postData() || '{}');
      const saved = {
        email: body.email,
        setAt: Math.floor(Date.now() / 1000),
      };
      if (dynamicDelegate) currentDelegate = saved;
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ code: 0, data: saved }),
      });
      return;
    }

    if (method === 'DELETE') {
      if (dynamicDelegate) currentDelegate = null;
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ code: 0 }),
      });
      return;
    }

    await route.continue();
  });
}

async function setupOrderMocks(page: Page) {
  await page.route('**/api/user/orders', async (route) => {
    if (route.request().method() === 'POST') {
      const body = JSON.parse(route.request().postData() || '{}');
      // Preview requests return an order but no payUrl redirect side-effect.
      // The real (non-preview) request returns a payUrl that the app would
      // navigate to — we set it to a fake URL so Playwright doesn't actually
      // leave the origin in the middle of the assertion. But with delegate
      // flows the app doesn't follow payUrl at all; see handleDelegatePay
      // which skips window.location.href.
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          code: 0,
          data: {
            payUrl: 'https://pay.example.com/cs_test123',
            order: {
              uuid: body.preview ? 'preview-order-uuid' : 'order-uuid-123',
              userId: 1,
              payAmount: 4990,
              originAmount: 4990,
              isPaid: false,
            },
          },
        }),
      });
      return;
    }
    await route.continue();
  });

  await page.route('**/api/user/orders/*/notify-delegate', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        code: 0,
        data: { delegateEmail: TEST_DELEGATE_EMAIL },
      }),
    });
  });
}

// =====================================================================
// Flow A: Empty-state inline delegate pay
// =====================================================================

test.describe('Delegate pay · empty state inline', () => {
  test('fills email inline and sees confirmation', async ({ page }) => {
    await setupCommonMocks(page, { delegate: null });
    await setupOrderMocks(page);

    await page.goto('/zh-CN/purchase');

    // Wait for the inline empty-state form to render. This only appears
    // after: auth loads + delegateLoaded=true + no delegate.
    const inlineInput = page.getByPlaceholder('代付人邮箱 friend@example.com');
    await expect(inlineInput).toBeVisible({ timeout: 15_000 });

    await inlineInput.fill(TEST_DELEGATE_EMAIL);
    await page.getByRole('button', { name: '发送代付请求' }).click();

    // Confirmation state: "已请求 alice@example.com 代付"
    await expect(
      page.getByRole('heading', { name: `已请求 ${TEST_DELEGATE_EMAIL} 代付` })
    ).toBeVisible({ timeout: 10_000 });
  });
});

// =====================================================================
// Flow B: Set-state one-click delegate pay
// =====================================================================

test.describe('Delegate pay · set state one-click', () => {
  test('uses existing delegate and sees confirmation', async ({ page }) => {
    await setupCommonMocks(page, {
      delegate: { email: TEST_DELEGATE_EMAIL, setAt: 1745000000 },
    });
    await setupOrderMocks(page);

    await page.goto('/zh-CN/purchase');

    // Chip showing current delegate
    await expect(
      page.getByText(`代付人：${TEST_DELEGATE_EMAIL}`)
    ).toBeVisible({ timeout: 15_000 });

    // Primary CTA "请 alice@example.com 代付"
    const primaryCta = page.getByRole('button', {
      name: new RegExp(`请\\s*${TEST_DELEGATE_EMAIL}\\s*代付`),
    });
    await expect(primaryCta).toBeVisible();
    await primaryCta.click();

    // Confirmation heading
    await expect(
      page.getByRole('heading', { name: `已请求 ${TEST_DELEGATE_EMAIL} 代付` })
    ).toBeVisible({ timeout: 10_000 });
  });
});

// =====================================================================
// Flow C: /account/delegate management
// =====================================================================

test.describe('Delegate pay · /account/delegate management', () => {
  test('set + modify + remove', async ({ page }) => {
    // dynamicDelegate: true so PUT/DELETE mutate the backing store and
    // subsequent GETs (e.g. after reload) would return updated data. The
    // DelegateClient itself doesn't re-fetch after save — it applies the
    // PUT response directly — but the dynamic mock keeps behavior sane if
    // the component ever did.
    await setupCommonMocks(page, { delegate: null, dynamicDelegate: true });

    await page.goto('/zh-CN/account/delegate');

    // --- Empty state: input + 保存 button visible ---
    const emailInput = page.getByPlaceholder('friend@example.com');
    await expect(emailInput).toBeVisible({ timeout: 15_000 });

    // --- Set ---
    await emailInput.fill(TEST_DELEGATE_EMAIL);
    await page.getByRole('button', { name: '保存' }).click();

    // Current delegate display shows the saved email
    await expect(
      page.getByText(TEST_DELEGATE_EMAIL, { exact: false }).first()
    ).toBeVisible({ timeout: 10_000 });
    await expect(
      page.getByRole('button', { name: '修改' })
    ).toBeVisible();

    // --- Modify ---
    await page.getByRole('button', { name: '修改' }).click();
    const editInput = page.getByPlaceholder('friend@example.com');
    await expect(editInput).toBeVisible();
    await editInput.fill(TEST_DELEGATE_EMAIL_2);
    await page.getByRole('button', { name: '保存' }).click();

    await expect(
      page.getByText(TEST_DELEGATE_EMAIL_2, { exact: false }).first()
    ).toBeVisible({ timeout: 10_000 });

    // --- Remove (auto-accept the native confirm dialog) ---
    page.on('dialog', (d) => d.accept());
    await page.getByRole('button', { name: '移除代付人' }).click();

    // Back to empty state (form + save visible again)
    await expect(
      page.getByPlaceholder('friend@example.com')
    ).toBeVisible({ timeout: 10_000 });
    await expect(page.getByRole('button', { name: '保存' })).toBeVisible();
  });
});
