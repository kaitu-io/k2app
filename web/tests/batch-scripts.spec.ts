import { test, expect, Page } from '@playwright/test';

/**
 * E2E Tests for Batch Scripts Management
 * Tests CRUD operations for /manager/nodes/batch/scripts
 */

// Test data
const TEST_SCRIPT = {
  name: 'Test Script',
  description: 'A test script for E2E testing',
  content: '#!/bin/bash\necho "Hello from batch script"\ndate\nuname -a',
};

const MOCK_SCRIPTS = [
  {
    id: 1,
    name: 'Health Check',
    description: 'Check server health',
    createdAt: Date.now() - 86400000,
    updatedAt: Date.now() - 86400000,
  },
  {
    id: 2,
    name: 'Disk Usage',
    description: 'Check disk usage',
    createdAt: Date.now() - 172800000,
    updatedAt: Date.now() - 172800000,
  },
];

const MOCK_SCRIPT_DETAIL = {
  id: 1,
  name: 'Health Check',
  description: 'Check server health',
  content: '#!/bin/bash\ndf -h\nfree -m\nuptime',
  createdAt: Date.now() - 86400000,
  updatedAt: Date.now() - 86400000,
};

/**
 * Setup API mocks for batch scripts
 */
async function setupScriptMocks(page: Page) {
  // Mock list scripts
  await page.route('**/app/batch-scripts?*', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        code: 0,
        data: {
          items: MOCK_SCRIPTS,
          pagination: {
            page: 1,
            pageSize: 20,
            total: MOCK_SCRIPTS.length,
          },
        },
      }),
    });
  });

  // Mock get script detail
  await page.route('**/app/batch-scripts/1', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          code: 0,
          data: MOCK_SCRIPT_DETAIL,
        }),
      });
    } else if (route.request().method() === 'DELETE') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          code: 0,
          data: { success: true },
        }),
      });
    }
  });

  // Mock create script
  await page.route('**/app/batch-scripts', async (route) => {
    if (route.request().method() === 'POST') {
      const postData = route.request().postDataJSON();
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          code: 0,
          data: {
            id: 3,
            name: postData.name,
            description: postData.description,
            createdAt: Date.now(),
            updatedAt: Date.now(),
          },
        }),
      });
    }
  });
}

test.describe('Batch Scripts Management', () => {
  test.beforeEach(async ({ page }) => {
    await setupScriptMocks(page);
  });

  test('should display scripts list', async ({ page }) => {
    await page.goto('/manager/nodes/batch/scripts');

    // Wait for page to load
    await expect(page.getByRole('heading', { name: '批量脚本管理' })).toBeVisible();

    // Check if scripts are displayed in table
    await expect(page.getByRole('cell', { name: 'Health Check', exact: true })).toBeVisible();
    await expect(page.getByRole('cell', { name: 'Disk Usage', exact: true })).toBeVisible();
  });

  test('should display empty state when no scripts', async ({ page }) => {
    // Override mock to return empty list
    await page.route('**/app/batch-scripts?*', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          code: 0,
          data: {
            items: [],
            pagination: {
              page: 1,
              pageSize: 20,
              total: 0,
            },
          },
        }),
      });
    });

    await page.goto('/manager/nodes/batch/scripts');

    // Check empty state
    await expect(page.getByText('暂无脚本')).toBeVisible();
    await expect(page.getByText('您还没有创建任何脚本')).toBeVisible();
  });

  test('should create a new script', async ({ page }) => {
    await page.goto('/manager/nodes/batch/scripts');

    // Click create button
    await page.getByRole('button', { name: '创建脚本' }).click();

    // Wait for dialog
    await expect(page.getByText('创建脚本')).toBeVisible();

    // Fill form
    await page.getByLabel('脚本名称').fill(TEST_SCRIPT.name);
    await page.getByLabel('描述（可选）').fill(TEST_SCRIPT.description);
    await page.getByLabel('脚本内容').fill(TEST_SCRIPT.content);

    // Submit form
    await page.getByRole('button', { name: /^创建$/ }).click();

    // Check success message (toast)
    await expect(page.getByText('脚本创建成功')).toBeVisible({ timeout: 10000 });
  });

  test('should show validation errors', async ({ page }) => {
    await page.goto('/manager/nodes/batch/scripts');

    // Click create button
    await page.getByRole('button', { name: '创建脚本' }).click();

    // Wait for dialog
    await expect(page.getByText('创建脚本')).toBeVisible();

    // Try to submit without filling required fields
    await page.getByRole('button', { name: /^创建$/ }).click();

    // Check validation messages (toast)
    await expect(page.getByText('请输入脚本名称')).toBeVisible({ timeout: 10000 });
  });

  test('should view script details', async ({ page }) => {
    await page.goto('/manager/nodes/batch/scripts');

    // Wait for table to load
    await expect(page.getByRole('cell', { name: 'Health Check', exact: true })).toBeVisible();

    // Find the row with Health Check and click its view button
    const row = page.locator('tr', { has: page.getByRole('cell', { name: 'Health Check', exact: true }) });
    await row.getByRole('button', { name: '查看' }).click();

    // Check if detail dialog is shown
    await expect(page.getByText('#!/bin/bash')).toBeVisible();
    await expect(page.getByText('df -h')).toBeVisible();
  });

  test('should delete a script', async ({ page }) => {
    await page.goto('/manager/nodes/batch/scripts');

    // Wait for table to load
    await expect(page.getByRole('cell', { name: 'Health Check', exact: true })).toBeVisible();

    // Find the row with Health Check and click its delete button
    const row = page.locator('tr', { has: page.getByRole('cell', { name: 'Health Check', exact: true }) });
    await row.getByRole('button', { name: '删除' }).click();

    // Confirm deletion in alert dialog
    await expect(page.getByText('确定要删除该脚本吗？')).toBeVisible();
    await page.getByRole('button', { name: /^删除$/ }).click();

    // Check success message (toast)
    await expect(page.getByText('脚本删除成功')).toBeVisible({ timeout: 10000 });
  });

  test('should cancel script creation', async ({ page }) => {
    await page.goto('/manager/nodes/batch/scripts');

    // Click create button
    await page.getByRole('button', { name: '创建脚本' }).click();

    // Fill some data
    await page.getByLabel('脚本名称').fill('Test');

    // Click cancel
    await page.getByRole('button', { name: '取消' }).click();

    // Dialog should be closed
    await expect(page.getByLabel('脚本名称')).not.toBeVisible();
  });

  test('should handle API errors gracefully', async ({ page }) => {
    // Mock API error
    await page.route('**/app/batch-scripts', async (route) => {
      if (route.request().method() === 'POST') {
        await route.fulfill({
          status: 500,
          contentType: 'application/json',
          body: JSON.stringify({
            code: 500,
            message: 'Internal server error',
          }),
        });
      }
    });

    await page.goto('/manager/nodes/batch/scripts');

    // Try to create script
    await page.getByRole('button', { name: '创建脚本' }).click();
    await expect(page.getByText('创建脚本')).toBeVisible();
    await page.getByLabel('脚本名称').fill('Test');
    await page.getByLabel('脚本内容').fill('echo test');
    await page.getByRole('button', { name: /^创建$/ }).click();

    // Check error message (toast)
    await expect(page.getByText('脚本创建失败')).toBeVisible({ timeout: 10000 });
  });
});

// ==================== NEW TESTS FOR SCRIPT VERSIONING ====================

const MOCK_VERSIONS = [
  { version: 3, createdAt: Date.now() - 3600000, createdBy: 1 },
  { version: 2, createdAt: Date.now() - 86400000, createdBy: 1 },
  { version: 1, createdAt: Date.now() - 172800000, createdBy: 1 },
];

const MOCK_VERSION_DETAIL = {
  version: 2,
  content: '#!/bin/bash\n# Old version\necho "Hello v2"',
  createdAt: Date.now() - 86400000,
  createdBy: 1,
};

async function setupVersionMocks(page: Page) {
  await setupScriptMocks(page);

  // Mock versions list
  await page.route('**/app/batch-scripts/1/versions', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        code: 0,
        data: {
          items: MOCK_VERSIONS,
          pagination: { page: 1, pageSize: 100, total: MOCK_VERSIONS.length },
        },
      }),
    });
  });

  // Mock version detail
  await page.route('**/app/batch-scripts/1/versions/2', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          code: 0,
          data: MOCK_VERSION_DETAIL,
        }),
      });
    }
  });

  // Mock restore version
  await page.route('**/app/batch-scripts/1/versions/2/restore', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        code: 0,
        data: {
          ...MOCK_SCRIPT_DETAIL,
          content: MOCK_VERSION_DETAIL.content,
          updatedAt: Date.now(),
        },
      }),
    });
  });
}

test.describe('Script Version History', () => {
  test.beforeEach(async ({ page }) => {
    await setupVersionMocks(page);
  });

  test('should show version history button', async ({ page }) => {
    await page.goto('/manager/nodes/batch/scripts');

    // Check version history button exists
    const row = page.locator('tr', { has: page.getByRole('cell', { name: 'Health Check', exact: true }) });
    await expect(row.getByRole('button', { name: /版本历史/ })).toBeVisible();
  });

  test('should display version list', async ({ page }) => {
    await page.goto('/manager/nodes/batch/scripts');

    // Click version history button
    const row = page.locator('tr', { has: page.getByRole('cell', { name: 'Health Check', exact: true }) });
    await row.getByRole('button', { name: /版本历史/ }).click();

    // Check dialog is open
    await expect(page.getByText('版本历史 - Health Check')).toBeVisible();

    // Check versions are listed
    await expect(page.getByText('v3')).toBeVisible();
    await expect(page.getByText('v2')).toBeVisible();
    await expect(page.getByText('v1')).toBeVisible();
  });

  test('should view version content', async ({ page }) => {
    await page.goto('/manager/nodes/batch/scripts');

    // Open version history
    const row = page.locator('tr', { has: page.getByRole('cell', { name: 'Health Check', exact: true }) });
    await row.getByRole('button', { name: /版本历史/ }).click();

    // Click on version 2
    await page.getByText('v2').click();

    // Check content is displayed
    await expect(page.getByText('# Old version')).toBeVisible();
    await expect(page.getByText('echo "Hello v2"')).toBeVisible();
  });

  test('should restore previous version', async ({ page }) => {
    await page.goto('/manager/nodes/batch/scripts');

    // Open version history
    const row = page.locator('tr', { has: page.getByRole('cell', { name: 'Health Check', exact: true }) });
    await row.getByRole('button', { name: /版本历史/ }).click();

    // Click on version 2
    await page.getByText('v2').click();

    // Click restore button
    await page.getByRole('button', { name: '恢复此版本' }).click();

    // Check success message
    await expect(page.getByText('版本恢复成功')).toBeVisible();
  });

  test('should download version', async ({ page }) => {
    await page.goto('/manager/nodes/batch/scripts');

    // Open version history
    const row = page.locator('tr', { has: page.getByRole('cell', { name: 'Health Check', exact: true }) });
    await row.getByRole('button', { name: /版本历史/ }).click();

    // Click on version 2
    await page.getByText('v2').click();

    // Check download button exists
    await expect(page.getByRole('button', { name: '下载' })).toBeVisible();
  });
});

// ==================== NEW TESTS FOR SCRIPT TESTING ====================

const MOCK_NODES = [
  { id: 1, name: 'Node 1', ipv4: '192.168.1.1', country: 'US', region: '', ipv6: '', load: 0.5, updatedAt: Date.now(), trafficUsagePercent: 30, bandwidthUsagePercent: 20 },
  { id: 2, name: 'Node 2', ipv4: '192.168.1.2', country: 'JP', region: 'Tokyo', ipv6: '', load: 0.3, updatedAt: Date.now(), trafficUsagePercent: 50, bandwidthUsagePercent: 40 },
];

const MOCK_TEST_RESULT = {
  stdout: 'Hello from test script\n2026-01-15 12:00:00\nLinux node1 5.4.0-generic',
  stderr: '',
  exitCode: 0,
  duration: 1500,
  error: '',
};

async function setupTestMocks(page: Page) {
  await setupScriptMocks(page);

  // Mock nodes list
  await page.route('**/app/nodes?*', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        code: 0,
        data: {
          items: MOCK_NODES,
          pagination: { page: 1, pageSize: 100, total: MOCK_NODES.length },
        },
      }),
    });
  });

  // Mock test endpoint
  await page.route('**/app/batch-scripts/1/test', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        code: 0,
        data: MOCK_TEST_RESULT,
      }),
    });
  });
}

test.describe('Script Testing', () => {
  test.beforeEach(async ({ page }) => {
    await setupTestMocks(page);
  });

  test('should show test button', async ({ page }) => {
    await page.goto('/manager/nodes/batch/scripts');

    // Check test button exists
    const row = page.locator('tr', { has: page.getByRole('cell', { name: 'Health Check', exact: true }) });
    await expect(row.getByRole('button', { name: /测试/ })).toBeVisible();
  });

  test('should open test dialog', async ({ page }) => {
    await page.goto('/manager/nodes/batch/scripts');

    // Click test button
    const row = page.locator('tr', { has: page.getByRole('cell', { name: 'Health Check', exact: true }) });
    await row.getByRole('button', { name: /测试/ }).click();

    // Check dialog is open
    await expect(page.getByText('测试脚本 - Health Check')).toBeVisible();

    // Check node selector is present
    await expect(page.getByText('选择测试节点')).toBeVisible();
  });

  test('should run test and show results', async ({ page }) => {
    await page.goto('/manager/nodes/batch/scripts');

    // Open test dialog
    const row = page.locator('tr', { has: page.getByRole('cell', { name: 'Health Check', exact: true }) });
    await row.getByRole('button', { name: /测试/ }).click();

    // Select node
    await page.getByRole('combobox').click();
    await page.getByRole('option', { name: /Node 1/ }).click();

    // Run test
    await page.getByRole('button', { name: '执行测试' }).click();

    // Check results are displayed
    await expect(page.getByText('执行成功')).toBeVisible();
    await expect(page.getByText('Hello from test script')).toBeVisible();
    await expect(page.getByText('1.50s')).toBeVisible();
  });

  test('should show error for failed test', async ({ page }) => {
    // Mock failed test
    await page.route('**/app/batch-scripts/1/test', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          code: 0,
          data: {
            stdout: '',
            stderr: 'Permission denied',
            exitCode: 1,
            duration: 500,
            error: 'Script execution failed',
          },
        }),
      });
    });

    await page.goto('/manager/nodes/batch/scripts');

    // Open test dialog and run test
    const row = page.locator('tr', { has: page.getByRole('cell', { name: 'Health Check', exact: true }) });
    await row.getByRole('button', { name: /测试/ }).click();
    await page.getByRole('combobox').click();
    await page.getByRole('option', { name: /Node 1/ }).click();
    await page.getByRole('button', { name: '执行测试' }).click();

    // Check error is displayed
    await expect(page.getByText('执行失败')).toBeVisible();
    await expect(page.getByText('Permission denied')).toBeVisible();
  });

  test('should require node selection', async ({ page }) => {
    await page.goto('/manager/nodes/batch/scripts');

    // Open test dialog
    const row = page.locator('tr', { has: page.getByRole('cell', { name: 'Health Check', exact: true }) });
    await row.getByRole('button', { name: /测试/ }).click();

    // Check execute button is disabled without node selection
    await expect(page.getByRole('button', { name: '执行测试' })).toBeDisabled();
  });
});
