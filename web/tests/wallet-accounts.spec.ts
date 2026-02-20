import { test, expect } from '@playwright/test';

/**
 * Wallet Accounts Tests
 * Tests for withdraw account management functionality
 */

test.describe('Wallet Accounts Page', () => {
  test.beforeEach(async ({ page }) => {
    // Mock API responses
    await page.route('**/api/wallet/withdraw-accounts', async (route) => {
      if (route.request().method() === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            code: 0,
            data: [
              {
                id: 1,
                accountType: 'tron',
                accountId: 'TXYZabc123456789012345678901234',
                currency: 'usdt',
                label: '主账户',
                isDefault: true,
              },
              {
                id: 2,
                accountType: 'paypal',
                accountId: 'test@example.com',
                currency: 'usd',
                label: 'PayPal 账户',
                isDefault: false,
              },
            ],
          }),
        });
      } else if (route.request().method() === 'POST') {
        const body = route.request().postDataJSON();
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            code: 0,
            data: {
              id: 3,
              accountType: body.accountType,
              accountId: body.accountId,
              currency: body.currency,
              label: body.label,
              isDefault: false,
            },
          }),
        });
      } else {
        await route.continue();
      }
    });

    // Mock auth check
    await page.route('**/api/auth/check', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          code: 0,
          data: { isAuthenticated: true, user: { email: 'test@test.com' } },
        }),
      });
    });
  });

  test('should display accounts list correctly', async ({ page }) => {
    await page.goto('/en-US/account/wallet/accounts');

    // Wait for accounts to load
    await expect(page.locator('table')).toBeVisible({ timeout: 10000 });

    // Check that accounts are displayed
    await expect(page.getByText('TXYZabc1...901234')).toBeVisible();
    await expect(page.getByText('test@example.com')).toBeVisible();

    // Check channel type badges
    await expect(page.getByText('TRON (TRC20)')).toBeVisible();
    await expect(page.getByText('PayPal')).toBeVisible();

    // Check currency badges
    await expect(page.getByText('USDT')).toBeVisible();
    await expect(page.getByText('USD')).toBeVisible();
  });

  test('should open add account dialog', async ({ page }) => {
    await page.goto('/en-US/account/wallet/accounts');

    // Click add account button
    await page.getByRole('button', { name: /add/i }).click();

    // Check dialog is open
    await expect(page.getByRole('dialog')).toBeVisible();

    // Check channel type selection cards
    await expect(page.getByText('加密货币钱包')).toBeVisible();
    await expect(page.getByText('PayPal')).toBeVisible();
  });

  test('should validate TRON address format', async ({ page }) => {
    await page.goto('/en-US/account/wallet/accounts');

    // Open add dialog
    await page.getByRole('button', { name: /add/i }).click();

    // Select crypto wallet (default)
    await expect(page.getByText('TRON (TRC20)')).toBeVisible();

    // Enter invalid address
    await page.getByPlaceholder('T...').fill('invalid_address');

    // Should show validation error
    await expect(page.getByText(/格式不正确/)).toBeVisible();
  });

  test('should validate EVM address format for Polygon', async ({ page }) => {
    await page.goto('/en-US/account/wallet/accounts');

    // Open add dialog
    await page.getByRole('button', { name: /add/i }).click();

    // Select Polygon network
    await page.getByRole('combobox').first().click();
    await page.getByRole('option', { name: /Polygon/ }).click();

    // Enter invalid address (should be 0x...)
    await page.getByPlaceholder('0x...').fill('invalid');

    // Should show validation error
    await expect(page.getByText(/格式不正确/)).toBeVisible();

    // Enter valid EVM address
    await page.getByPlaceholder('0x...').fill('0x1234567890123456789012345678901234567890');

    // Error should disappear
    await expect(page.getByText(/格式不正确/)).not.toBeVisible();
  });

  test('should validate PayPal email format', async ({ page }) => {
    await page.goto('/en-US/account/wallet/accounts');

    // Open add dialog
    await page.getByRole('button', { name: /add/i }).click();

    // Select PayPal
    await page.getByText('PayPal').click();

    // PayPal email field should appear
    await expect(page.getByPlaceholder('your@email.com')).toBeVisible();

    // Enter invalid email
    await page.getByPlaceholder('your@email.com').fill('invalid-email');

    // Should show validation error
    await expect(page.getByText(/邮箱格式不正确/)).toBeVisible();

    // Enter valid email
    await page.getByPlaceholder('your@email.com').fill('valid@example.com');

    // Error should disappear
    await expect(page.getByText(/邮箱格式不正确/)).not.toBeVisible();
  });

  test('should submit crypto wallet account successfully', async ({ page }) => {
    await page.goto('/en-US/account/wallet/accounts');

    // Open add dialog
    await page.getByRole('button', { name: /add/i }).click();

    // Fill form with valid TRON address
    await page.getByPlaceholder('T...').fill('TXYZabc123456789012345678901235');

    // Add label
    await page.getByPlaceholder(/账户标签/).fill('备用账户');

    // Submit
    await page.getByRole('button', { name: /确认|confirm/i }).click();

    // Dialog should close on success
    await expect(page.getByRole('dialog')).not.toBeVisible({ timeout: 5000 });
  });

  test('should submit PayPal account successfully', async ({ page }) => {
    await page.goto('/en-US/account/wallet/accounts');

    // Open add dialog
    await page.getByRole('button', { name: /add/i }).click();

    // Select PayPal
    await page.getByText('PayPal').click();

    // Fill email
    await page.getByPlaceholder('your@email.com').fill('newpaypal@example.com');

    // Add label
    await page.getByPlaceholder(/账户标签/).fill('PayPal 主账户');

    // Submit
    await page.getByRole('button', { name: /确认|confirm/i }).click();

    // Dialog should close on success
    await expect(page.getByRole('dialog')).not.toBeVisible({ timeout: 5000 });
  });

  test('should show currency guide for crypto accounts', async ({ page }) => {
    await page.goto('/en-US/account/wallet/accounts');

    // Open add dialog
    await page.getByRole('button', { name: /add/i }).click();

    // Currency guide should be visible for crypto
    await expect(page.getByText(/USDT.*稳定币/)).toBeVisible();
  });

  test('should show PayPal guide for PayPal accounts', async ({ page }) => {
    await page.goto('/en-US/account/wallet/accounts');

    // Open add dialog
    await page.getByRole('button', { name: /add/i }).click();

    // Select PayPal
    await page.getByText('PayPal').click();

    // PayPal guide should be visible
    await expect(page.getByText(/PayPal.*手续费/)).toBeVisible();
  });
});

test.describe('Admin Withdraws Page', () => {
  test.beforeEach(async ({ page }) => {
    // Mock admin withdraws API
    await page.route('**/app/withdraws*', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          code: 0,
          data: {
            items: [
              {
                id: 1,
                createdAt: Date.now() / 1000,
                user: { uuid: 'user-1', email: 'user@test.com' },
                amount: 10000,         // $100.00
                feeAmount: 100,        // $1.00 fee
                netAmount: 9900,       // $99.00 net
                status: 'pending',
                account: {
                  accountType: 'tron',
                  accountId: 'TXYZabc123456789012345678901234',
                  currency: 'usdt',
                },
              },
              {
                id: 2,
                createdAt: Date.now() / 1000,
                user: { uuid: 'user-2', email: 'user2@test.com' },
                amount: 5000,          // $50.00
                feeAmount: 150,        // $1.50 fee (PayPal 3%)
                netAmount: 4850,       // $48.50 net
                status: 'completed',
                account: {
                  accountType: 'paypal',
                  accountId: 'paypal@example.com',
                  currency: 'usd',
                },
                transaction: {
                  txHash: 'PAYPAL-TX-123',
                  explorerUrl: 'https://www.paypal.com/activity/payment/PAYPAL-TX-123',
                },
              },
            ],
            pagination: {
              page: 1,
              pageSize: 10,
              total: 2,
            },
          },
        }),
      });
    });

    // Mock auth
    await page.route('**/api/auth/check', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          code: 0,
          data: { isAuthenticated: true, user: { email: 'admin@test.com', role: 'admin' } },
        }),
      });
    });
  });

  test('should display fee breakdown in withdraws list', async ({ page }) => {
    await page.goto('/en-US/admin/withdraws');

    // Wait for table to load
    await expect(page.locator('table')).toBeVisible({ timeout: 10000 });

    // Check fee breakdown is displayed
    await expect(page.getByText('$100.00')).toBeVisible();  // Total amount
    await expect(page.getByText('$1.00')).toBeVisible();    // Fee
    await expect(page.getByText('$99.00')).toBeVisible();   // Net amount

    // Check PayPal withdrawal
    await expect(page.getByText('$50.00')).toBeVisible();   // Total amount
    await expect(page.getByText('$1.50')).toBeVisible();    // Fee
    await expect(page.getByText('$48.50')).toBeVisible();   // Net amount
  });

  test('should display account type badges correctly', async ({ page }) => {
    await page.goto('/en-US/admin/withdraws');

    await expect(page.locator('table')).toBeVisible({ timeout: 10000 });

    // Check account type badges
    await expect(page.getByText('TRON')).toBeVisible();
    await expect(page.getByText('PayPal')).toBeVisible();

    // Check currency badges
    await expect(page.getByText('USDT')).toBeVisible();
    await expect(page.getByText('USD')).toBeVisible();
  });

  test('should show PayPal email as account identifier', async ({ page }) => {
    await page.goto('/en-US/admin/withdraws');

    await expect(page.locator('table')).toBeVisible({ timeout: 10000 });

    // Check account identifiers
    await expect(page.getByText('TXYZabc123456789012345678901234')).toBeVisible();
    await expect(page.getByText('paypal@example.com')).toBeVisible();
  });
});
