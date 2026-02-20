import { test, expect, Page } from '@playwright/test';

/**
 * E2E Tests for Nodes Matrix with Multi-Selection
 * Tests /manager/nodes page with selection and quick actions
 */

const MOCK_SCRIPTS = [
  { id: 1, name: 'Health Check' },
  { id: 2, name: 'Disk Usage' },
  { id: 3, name: 'Memory Check' },
];

const MOCK_NODES = [
  {
    id: 1,
    name: 'Node US-1',
    country: 'US',
    region: 'California',
    ipv4: '192.168.1.1',
    ipv6: '2001:db8::1',
    updatedAt: Math.floor(Date.now() / 1000) - 300,
    tunnelCount: 3,
    tunnels: [
      { id: 1, domain: 'us1.example.com', protocol: 'quic', port: 443 },
      { id: 2, domain: 'us1.example.com', protocol: 'tcp', port: 8443 },
      { id: 3, domain: 'us1.example.com', protocol: 'ws', port: 80 },
    ],
    results: {
      '1': { status: 'success', taskId: 10, executedAt: Date.now() - 86400000, exitCode: 0, stdout: 'OK', stderr: '' },
      '2': { status: 'failed', taskId: 11, executedAt: Date.now() - 172800000, exitCode: 1, stdout: '', stderr: 'Disk full' },
      '3': null,
    },
  },
  {
    id: 2,
    name: 'Node JP-1',
    country: 'JP',
    region: 'Tokyo',
    ipv4: '192.168.1.2',
    ipv6: '',
    updatedAt: Math.floor(Date.now() / 1000) - 600,
    tunnelCount: 1,
    tunnels: [
      { id: 4, domain: 'jp1.example.com', protocol: 'quic', port: 443 },
    ],
    results: {
      '1': { status: 'success', taskId: 10, executedAt: Date.now() - 86400000, exitCode: 0, stdout: 'OK', stderr: '' },
      '2': { status: 'success', taskId: 11, executedAt: Date.now() - 172800000, exitCode: 0, stdout: '50% used', stderr: '' },
      '3': { status: 'success', taskId: 12, executedAt: Date.now() - 259200000, exitCode: 0, stdout: '4GB free', stderr: '' },
    },
  },
  {
    id: 3,
    name: 'Node DE-1',
    country: 'DE',
    region: 'Frankfurt',
    ipv4: '192.168.1.3',
    ipv6: '2001:db8::3',
    updatedAt: Math.floor(Date.now() / 1000) - 3600,
    tunnelCount: 0,
    tunnels: [],
    results: {
      '1': null,
      '2': null,
      '3': null,
    },
  },
];

async function setupMatrixMocks(page: Page) {
  // Mock batch matrix API
  await page.route('**/app/nodes/batch-matrix', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        code: 0,
        data: {
          scripts: MOCK_SCRIPTS,
          nodes: MOCK_NODES,
        },
      }),
    });
  });

  // Mock scripts list
  await page.route('**/app/batch-scripts?*', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        code: 0,
        data: {
          items: MOCK_SCRIPTS.map((s, i) => ({
            ...s,
            description: `Script ${i + 1}`,
            executeWithSudo: i === 0,
            createdAt: Date.now() - 86400000 * i,
            updatedAt: Date.now() - 86400000 * i,
          })),
          pagination: { page: 1, pageSize: 100, total: MOCK_SCRIPTS.length },
        },
      }),
    });
  });

  // Mock create task
  await page.route('**/app/batch-tasks', async (route) => {
    if (route.request().method() === 'POST') {
      const postData = route.request().postDataJSON();
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          code: 0,
          data: {
            id: 100,
            asynqTaskId: 'task-100',
            scriptId: postData.scriptId,
            scriptName: MOCK_SCRIPTS.find(s => s.id === postData.scriptId)?.name || '',
            nodeIds: postData.nodeIds,
            scheduleType: 'once',
            executeAt: postData.executeAt,
            cronExpr: '',
            status: 'pending',
            currentIndex: 0,
            totalNodes: postData.nodeIds.length,
            createdAt: Date.now(),
            completedAt: null,
          },
        }),
      });
    }
  });

  // Mock delete node
  await page.route('**/app/nodes/*', async (route) => {
    if (route.request().method() === 'DELETE') {
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
}

test.describe('Nodes Matrix Display', () => {
  test.beforeEach(async ({ page }) => {
    await setupMatrixMocks(page);
  });

  test('should display nodes matrix table', async ({ page }) => {
    await page.goto('/manager/nodes');

    // Check page title
    await expect(page.getByRole('heading', { name: '节点运维' })).toBeVisible();

    // Check nodes are displayed
    await expect(page.getByText('Node US-1')).toBeVisible();
    await expect(page.getByText('Node JP-1')).toBeVisible();
    await expect(page.getByText('Node DE-1')).toBeVisible();

    // Check script columns are displayed
    await expect(page.getByRole('columnheader', { name: 'Health Check' })).toBeVisible();
    await expect(page.getByRole('columnheader', { name: 'Disk Usage' })).toBeVisible();
    await expect(page.getByRole('columnheader', { name: 'Memory Check' })).toBeVisible();
  });

  test('should display task result icons', async ({ page }) => {
    await page.goto('/manager/nodes');

    // Check success icons (green checkmarks)
    const successIcons = page.locator('svg.text-green-500');
    await expect(successIcons.first()).toBeVisible();

    // Check failed icons (red X)
    const failedIcons = page.locator('svg.text-red-500');
    await expect(failedIcons.first()).toBeVisible();

    // Check empty state icons (gray dash)
    const emptyIcons = page.locator('svg.text-gray-300');
    await expect(emptyIcons.first()).toBeVisible();
  });

  test('should show node details on hover', async ({ page }) => {
    await page.goto('/manager/nodes');

    // Hover over a result icon
    const successIcon = page.locator('svg.text-green-500').first();
    await successIcon.hover();

    // Check tooltip content
    await expect(page.getByText('状态: 成功')).toBeVisible();
    await expect(page.getByText('退出码: 0')).toBeVisible();
  });

  test('should display tunnel information', async ({ page }) => {
    await page.goto('/manager/nodes');

    // Click on tunnel count to expand
    await page.getByText('3 个隧道').click();

    // Check tunnel details are shown
    await expect(page.getByText('us1.example.com (quic:443)')).toBeVisible();
    await expect(page.getByText('us1.example.com (tcp:8443)')).toBeVisible();
  });
});

test.describe('Nodes Multi-Selection', () => {
  test.beforeEach(async ({ page }) => {
    await setupMatrixMocks(page);
  });

  test('should show checkbox column', async ({ page }) => {
    await page.goto('/manager/nodes');

    // Check header checkbox exists
    await expect(page.getByRole('checkbox').first()).toBeVisible();

    // Check each row has a checkbox
    const checkboxes = page.getByRole('checkbox');
    await expect(checkboxes).toHaveCount(4); // 1 header + 3 rows
  });

  test('should select individual nodes', async ({ page }) => {
    await page.goto('/manager/nodes');

    // Select first node
    const firstRowCheckbox = page.getByRole('checkbox').nth(1);
    await firstRowCheckbox.check();

    // Check selection bar appears
    await expect(page.getByText('已选择 1 个节点')).toBeVisible();

    // Select second node
    const secondRowCheckbox = page.getByRole('checkbox').nth(2);
    await secondRowCheckbox.check();

    // Check count updates
    await expect(page.getByText('已选择 2 个节点')).toBeVisible();
  });

  test('should select all nodes', async ({ page }) => {
    await page.goto('/manager/nodes');

    // Click header checkbox to select all
    const headerCheckbox = page.getByRole('checkbox').first();
    await headerCheckbox.check();

    // Check all nodes are selected
    await expect(page.getByText('已选择 3 个节点')).toBeVisible();

    // All row checkboxes should be checked
    const rowCheckboxes = page.getByRole('checkbox').nth(1);
    await expect(rowCheckboxes).toBeChecked();
  });

  test('should deselect all when clicking header again', async ({ page }) => {
    await page.goto('/manager/nodes');

    // Select all
    const headerCheckbox = page.getByRole('checkbox').first();
    await headerCheckbox.check();
    await expect(page.getByText('已选择 3 个节点')).toBeVisible();

    // Deselect all
    await headerCheckbox.uncheck();
    await expect(page.getByText('已选择')).not.toBeVisible();
  });

  test('should clear selection with cancel button', async ({ page }) => {
    await page.goto('/manager/nodes');

    // Select a node
    const firstRowCheckbox = page.getByRole('checkbox').nth(1);
    await firstRowCheckbox.check();
    await expect(page.getByText('已选择 1 个节点')).toBeVisible();

    // Click cancel
    await page.getByRole('button', { name: '取消选择' }).click();

    // Selection should be cleared
    await expect(page.getByText('已选择')).not.toBeVisible();
    await expect(firstRowCheckbox).not.toBeChecked();
  });
});

test.describe('Nodes Quick Actions', () => {
  test.beforeEach(async ({ page }) => {
    await setupMatrixMocks(page);
  });

  test('should show execute script button when nodes selected', async ({ page }) => {
    await page.goto('/manager/nodes');

    // Select nodes
    const headerCheckbox = page.getByRole('checkbox').first();
    await headerCheckbox.check();

    // Check execute button is visible
    await expect(page.getByRole('button', { name: '执行脚本' })).toBeVisible();
  });

  test('should open quick action dialog', async ({ page }) => {
    await page.goto('/manager/nodes');

    // Select nodes
    const firstRowCheckbox = page.getByRole('checkbox').nth(1);
    await firstRowCheckbox.check();

    // Click execute button
    await page.getByRole('button', { name: '执行脚本' }).click();

    // Check dialog is open
    await expect(page.getByText('批量执行脚本')).toBeVisible();
    await expect(page.getByText('选择要在 1 个节点上执行的脚本')).toBeVisible();
  });

  test('should execute script on selected nodes', async ({ page }) => {
    await page.goto('/manager/nodes');

    // Select two nodes
    await page.getByRole('checkbox').nth(1).check();
    await page.getByRole('checkbox').nth(2).check();

    // Open dialog
    await page.getByRole('button', { name: '执行脚本' }).click();

    // Select script
    await page.getByRole('combobox').click();
    await page.getByRole('option', { name: 'Health Check' }).click();

    // Execute
    await page.getByRole('button', { name: '立即执行' }).click();

    // Check success and navigation
    await expect(page.getByText('任务创建成功')).toBeVisible();
  });

  test('should require script selection', async ({ page }) => {
    await page.goto('/manager/nodes');

    // Select node
    await page.getByRole('checkbox').nth(1).check();

    // Open dialog
    await page.getByRole('button', { name: '执行脚本' }).click();

    // Execute button should be disabled without script selection
    await expect(page.getByRole('button', { name: '立即执行' })).toBeDisabled();
  });
});

test.describe('Nodes Task Result Detail', () => {
  test.beforeEach(async ({ page }) => {
    await setupMatrixMocks(page);
  });

  test('should open result detail dialog', async ({ page }) => {
    await page.goto('/manager/nodes');

    // Click on a result icon
    const successIcon = page.locator('svg.text-green-500').first();
    await successIcon.click();

    // Check dialog content
    await expect(page.getByText('Health Check - Node US-1')).toBeVisible();
    await expect(page.getByText('状态:')).toBeVisible();
    await expect(page.getByText('成功')).toBeVisible();
  });

  test('should show task link in detail dialog', async ({ page }) => {
    await page.goto('/manager/nodes');

    // Click on a result icon
    const successIcon = page.locator('svg.text-green-500').first();
    await successIcon.click();

    // Check link to task exists
    await expect(page.getByRole('link', { name: '查看任务详情' })).toBeVisible();
  });
});

test.describe('Nodes Row Actions', () => {
  test.beforeEach(async ({ page }) => {
    await setupMatrixMocks(page);
  });

  test('should show dropdown menu', async ({ page }) => {
    await page.goto('/manager/nodes');

    // Click action menu
    const actionButton = page.getByRole('row').nth(1).getByRole('button').last();
    await actionButton.click();

    // Check menu items
    await expect(page.getByText('SSH Terminal')).toBeVisible();
    await expect(page.getByText('查看所有任务')).toBeVisible();
    await expect(page.getByText('删除节点')).toBeVisible();
  });

  test('should confirm before deleting node', async ({ page }) => {
    await page.goto('/manager/nodes');

    // Open action menu
    const actionButton = page.getByRole('row').nth(1).getByRole('button').last();
    await actionButton.click();

    // Click delete
    await page.getByText('删除节点').click();

    // Check confirmation dialog
    await expect(page.getByText('确认删除节点？')).toBeVisible();
    await expect(page.getByText('192.168.1.1')).toBeVisible();
  });

  test('should delete node after confirmation', async ({ page }) => {
    await page.goto('/manager/nodes');

    // Open action menu and click delete
    const actionButton = page.getByRole('row').nth(1).getByRole('button').last();
    await actionButton.click();
    await page.getByText('删除节点').click();

    // Confirm
    await page.getByRole('button', { name: '确认删除' }).click();

    // Check success
    await expect(page.getByText('节点删除成功')).toBeVisible();
  });
});
