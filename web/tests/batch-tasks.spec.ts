import { test, expect, Page } from '@playwright/test';

/**
 * E2E Tests for Batch Tasks Management
 * Tests CRUD and control operations for /manager/nodes/batch/tasks
 */

// Test data
const MOCK_SCRIPTS = [
  { id: 1, name: 'Health Check', description: 'Check server health', createdAt: Date.now(), updatedAt: Date.now() },
  { id: 2, name: 'Disk Usage', description: 'Check disk usage', createdAt: Date.now(), updatedAt: Date.now() },
];

const MOCK_NODES = [
  { id: 1, name: 'Node 1', ipv4: '192.168.1.1' },
  { id: 2, name: 'Node 2', ipv4: '192.168.1.2' },
  { id: 3, name: 'Node 3', ipv4: '192.168.1.3' },
];

const MOCK_TASKS = [
  {
    id: 1,
    asynqTaskId: 'task-001',
    scriptId: 1,
    scriptName: 'Health Check',
    nodeIds: [1, 2],
    scheduleType: 'once',
    executeAt: Date.now(),
    cronExpr: '',
    status: 'running',
    currentIndex: 1,
    totalNodes: 2,
    createdAt: Date.now() - 300000,
    completedAt: null,
  },
  {
    id: 2,
    asynqTaskId: 'task-002',
    scriptId: 2,
    scriptName: 'Disk Usage',
    nodeIds: [1, 2, 3],
    scheduleType: 'once',
    executeAt: Date.now() - 600000,
    cronExpr: '',
    status: 'completed',
    currentIndex: 3,
    totalNodes: 3,
    createdAt: Date.now() - 600000,
    completedAt: Date.now() - 300000,
  },
];

const MOCK_TASK_DETAIL = {
  id: 1,
  asynqTaskId: 'task-001',
  scriptId: 1,
  scriptName: 'Health Check',
  nodeIds: [1, 2],
  scheduleType: 'once',
  executeAt: Date.now(),
  cronExpr: '',
  status: 'running',
  currentIndex: 1,
  totalNodes: 2,
  createdAt: Date.now() - 300000,
  completedAt: null,
  results: [
    {
      nodeId: 1,
      nodeName: 'Node 1',
      nodeIpv4: '192.168.1.1',
      nodeIndex: 0,
      status: 'success',
      stdout: 'Disk usage: 50%\nMemory: 4GB free\nUptime: 10 days',
      stderr: '',
      exitCode: 0,
      error: '',
      startedAt: Date.now() - 290000,
      endedAt: Date.now() - 280000,
      duration: 10000,
    },
    {
      nodeId: 2,
      nodeName: 'Node 2',
      nodeIpv4: '192.168.1.2',
      nodeIndex: 1,
      status: 'running',
      stdout: '',
      stderr: '',
      exitCode: -1,
      error: '',
      startedAt: Date.now() - 5000,
      endedAt: null,
      duration: null,
    },
  ],
};

/**
 * Setup API mocks for batch tasks
 */
async function setupTaskMocks(page: Page) {
  // Mock list scripts
  await page.route('**/app/batch-scripts?*', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        code: 0,
        data: {
          items: MOCK_SCRIPTS,
          pagination: { page: 1, pageSize: 100, total: MOCK_SCRIPTS.length },
        },
      }),
    });
  });

  // Mock list nodes
  await page.route('**/app/nodes?*', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        code: 0,
        data: {
          items: MOCK_NODES,
          pagination: { page: 1, pageSize: 200, total: MOCK_NODES.length },
        },
      }),
    });
  });

  // Mock list tasks
  await page.route('**/app/batch-tasks?*', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        code: 0,
        data: {
          items: MOCK_TASKS,
          pagination: { page: 1, pageSize: 100, total: MOCK_TASKS.length },
        },
      }),
    });
  });

  // Mock get task detail
  await page.route('**/app/batch-tasks/1', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          code: 0,
          data: MOCK_TASK_DETAIL,
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
            id: 3,
            asynqTaskId: 'task-003',
            scriptId: postData.scriptId,
            scriptName: MOCK_SCRIPTS.find(s => s.id === postData.scriptId)?.name || '',
            nodeIds: postData.nodeIds,
            scheduleType: postData.scheduleType,
            executeAt: postData.executeAt,
            cronExpr: postData.cronExpr || '',
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

  // Mock pause task
  await page.route('**/app/batch-tasks/1/pause', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        code: 0,
        data: { success: true },
      }),
    });
  });

  // Mock resume task
  await page.route('**/app/batch-tasks/1/resume', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        code: 0,
        data: { success: true },
      }),
    });
  });
}

test.describe('Batch Tasks Management', () => {
  test.beforeEach(async ({ page }) => {
    await setupTaskMocks(page);
  });

  test('should display tasks list', async ({ page }) => {
    await page.goto('/manager/nodes/batch/tasks');

    // Wait for page to load
    await expect(page.getByRole('heading', { name: '批量任务管理' })).toBeVisible();

    // Check if tasks are displayed
    await expect(page.getByText('Health Check')).toBeVisible();
    await expect(page.getByText('Disk Usage')).toBeVisible();

    // Check task status badges
    await expect(page.getByText('执行中')).toBeVisible();
    await expect(page.getByText('已完成')).toBeVisible();
  });

  test('should display empty state when no tasks', async ({ page }) => {
    // Override mock to return empty list
    await page.route('**/app/batch-tasks?*', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          code: 0,
          data: {
            items: [],
            pagination: { page: 1, pageSize: 100, total: 0 },
          },
        }),
      });
    });

    await page.goto('/manager/nodes/batch/tasks');

    // Check empty state
    await expect(page.getByText('暂无任务')).toBeVisible();
    await expect(page.getByText('您还没有创建任何批量任务')).toBeVisible();
  });

  test('should create a new task', async ({ page }) => {
    await page.goto('/manager/nodes/batch/tasks');

    // Click create button
    await page.getByRole('button', { name: '创建任务' }).click();

    // Wait for dialog to open
    await expect(page.getByText('创建批量任务')).toBeVisible();

    // Select script
    await page.getByRole('combobox').click();
    await page.getByRole('option', { name: 'Health Check' }).click();

    // Select nodes
    await page.getByRole('checkbox', { name: /Node 1/ }).check();
    await page.getByRole('checkbox', { name: /Node 2/ }).check();

    // Submit form
    await page.getByRole('button', { name: /^创建$/ }).click();

    // Check success message
    await expect(page.getByText('任务创建成功')).toBeVisible();
  });

  test('should validate required fields', async ({ page }) => {
    await page.goto('/manager/nodes/batch/tasks');

    // Click create button
    await page.getByRole('button', { name: '创建任务' }).click();

    // Try to submit without selecting script
    await page.getByRole('button', { name: /^创建$/ }).click();

    // Check validation message
    await expect(page.getByText('请选择脚本')).toBeVisible();
  });

  test('should select all nodes', async ({ page }) => {
    await page.goto('/manager/nodes/batch/tasks');

    // Click create button
    await page.getByRole('button', { name: '创建任务' }).click();

    // Click "select all" button
    await page.getByRole('button', { name: '全选' }).click();

    // All checkboxes should be checked
    await expect(page.getByRole('checkbox', { name: /Node 1/ })).toBeChecked();
    await expect(page.getByRole('checkbox', { name: /Node 2/ })).toBeChecked();
    await expect(page.getByRole('checkbox', { name: /Node 3/ })).toBeChecked();

    // Check counter
    await expect(page.getByText('3 个已选')).toBeVisible();
  });

  test('should view task details', async ({ page }) => {
    await page.goto('/manager/nodes/batch/tasks');

    // Click view button for first task
    await page.getByRole('button', { name: '查看' }).first().click();

    // Wait for navigation to detail page
    await expect(page).toHaveURL(/\/manager\/nodes\/batch\/tasks\/1/);

    // Check if detail page is shown
    await expect(page.getByRole('heading', { name: '任务详情' })).toBeVisible();
    await expect(page.getByText('Health Check')).toBeVisible();

    // Check results table
    await expect(page.getByText('Node 1')).toBeVisible();
    await expect(page.getByText('192.168.1.1')).toBeVisible();
    await expect(page.getByText('成功')).toBeVisible();
  });

  test('should pause a running task', async ({ page }) => {
    await page.goto('/manager/nodes/batch/tasks');

    // Click pause button for running task
    await page.getByRole('button', { name: '暂停' }).click();

    // Confirm pause
    await page.getByRole('button', { name: /^暂停$/ }).click();

    // Check success message
    await expect(page.getByText('任务已暂停')).toBeVisible();
  });

  test('should delete a completed task', async ({ page }) => {
    await page.goto('/manager/nodes/batch/tasks');

    // Find completed task row and click delete
    const completedRow = page.locator('tr', { has: page.getByText('Disk Usage') });
    await completedRow.getByRole('button', { name: '删除' }).click();

    // Confirm deletion
    await page.getByRole('button', { name: /^删除$/ }).click();

    // Check success message
    await expect(page.getByText('任务删除成功')).toBeVisible();
  });

  test('should filter results by status in detail page', async ({ page }) => {
    await page.goto('/manager/nodes/batch/tasks/1');

    // Wait for page to load
    await expect(page.getByRole('heading', { name: '任务详情' })).toBeVisible();

    // Click success tab
    await page.getByRole('tab', { name: /成功/ }).click();

    // Should show only success results
    await expect(page.getByText('Node 1')).toBeVisible();
    await expect(page.getByText('Node 2')).not.toBeVisible();

    // Click all tab
    await page.getByRole('tab', { name: /全部/ }).click();

    // Should show all results
    await expect(page.getByText('Node 1')).toBeVisible();
    await expect(page.getByText('Node 2')).toBeVisible();
  });

  test('should handle API errors gracefully', async ({ page }) => {
    // Mock API error
    await page.route('**/app/batch-tasks', async (route) => {
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

    await page.goto('/manager/nodes/batch/tasks');

    // Try to create task
    await page.getByRole('button', { name: '创建任务' }).click();
    await page.getByRole('combobox').click();
    await page.getByRole('option', { name: 'Health Check' }).click();
    await page.getByRole('checkbox', { name: /Node 1/ }).check();
    await page.getByRole('button', { name: /^创建$/ }).click();

    // Check error message
    await expect(page.getByText('任务创建失败')).toBeVisible();
  });

  test('should navigate back from detail page', async ({ page }) => {
    await page.goto('/manager/nodes/batch/tasks/1');

    // Click back button
    await page.getByRole('button', { name: '返回' }).click();

    // Should return to task list
    await expect(page).toHaveURL(/\/manager\/nodes\/batch\/tasks$/);
  });
});

// ==================== NEW TESTS FOR RETRY FUNCTIONALITY ====================

const MOCK_FAILED_TASK_DETAIL = {
  id: 3,
  asynqTaskId: 'task-003',
  scriptId: 1,
  scriptName: 'Health Check',
  nodeIds: [1, 2, 3],
  scheduleType: 'once',
  executeAt: Date.now() - 600000,
  cronExpr: '',
  status: 'completed',
  currentIndex: 3,
  totalNodes: 3,
  createdAt: Date.now() - 600000,
  completedAt: Date.now() - 300000,
  successCount: 1,
  failedCount: 2,
  parentTaskId: null,
  isEnabled: true,
  results: [
    {
      nodeId: 1,
      nodeName: 'Node 1',
      nodeIpv4: '192.168.1.1',
      nodeIndex: 0,
      status: 'success',
      stdout: 'OK',
      stderr: '',
      exitCode: 0,
      error: '',
      startedAt: Date.now() - 550000,
      endedAt: Date.now() - 540000,
      duration: 10000,
    },
    {
      nodeId: 2,
      nodeName: 'Node 2',
      nodeIpv4: '192.168.1.2',
      nodeIndex: 1,
      status: 'failed',
      stdout: '',
      stderr: 'Connection refused',
      exitCode: 1,
      error: 'SSH connection failed',
      startedAt: Date.now() - 530000,
      endedAt: Date.now() - 520000,
      duration: 10000,
    },
    {
      nodeId: 3,
      nodeName: 'Node 3',
      nodeIpv4: '192.168.1.3',
      nodeIndex: 2,
      status: 'failed',
      stdout: '',
      stderr: 'Command timeout',
      exitCode: 124,
      error: '',
      startedAt: Date.now() - 510000,
      endedAt: Date.now() - 400000,
      duration: 110000,
    },
  ],
};

async function setupRetryMocks(page: Page) {
  // Mock get task detail with failures
  await page.route('**/app/batch-tasks/3', async (route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          code: 0,
          data: MOCK_FAILED_TASK_DETAIL,
        }),
      });
    }
  });

  // Mock retry endpoint
  await page.route('**/app/batch-tasks/3/retry', async (route) => {
    const postData = route.request().postDataJSON();
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        code: 0,
        data: {
          taskId: 4, // New task ID for retry
        },
      }),
    });
  });
}

test.describe('Batch Tasks Retry Functionality', () => {
  test.beforeEach(async ({ page }) => {
    await setupTaskMocks(page);
    await setupRetryMocks(page);
  });

  test('should display retry button for tasks with failures', async ({ page }) => {
    await page.goto('/manager/nodes/batch/tasks/3');

    // Wait for page to load
    await expect(page.getByRole('heading', { name: '任务详情' })).toBeVisible();

    // Check retry button is visible
    await expect(page.getByRole('button', { name: '重试失败节点' })).toBeVisible();

    // Check failed count badge
    await expect(page.getByText('2 失败')).toBeVisible();
  });

  test('should show retry dialog with failed nodes selection', async ({ page }) => {
    await page.goto('/manager/nodes/batch/tasks/3');

    // Click retry button
    await page.getByRole('button', { name: '重试失败节点' }).click();

    // Check dialog is open
    await expect(page.getByText('重试失败节点')).toBeVisible();

    // Check failed nodes are listed
    await expect(page.getByText('Node 2')).toBeVisible();
    await expect(page.getByText('Node 3')).toBeVisible();

    // Check successful node is not listed
    await expect(page.getByText('Node 1')).not.toBeVisible();
  });

  test('should retry all failed nodes', async ({ page }) => {
    await page.goto('/manager/nodes/batch/tasks/3');

    // Click retry button
    await page.getByRole('button', { name: '重试失败节点' }).click();

    // Click retry all button
    await page.getByRole('button', { name: '重试全部' }).click();

    // Check success message
    await expect(page.getByText('重试任务已创建')).toBeVisible();
  });

  test('should retry selected nodes only', async ({ page }) => {
    await page.goto('/manager/nodes/batch/tasks/3');

    // Click retry button
    await page.getByRole('button', { name: '重试失败节点' }).click();

    // Uncheck one node
    await page.getByRole('checkbox', { name: /Node 3/ }).uncheck();

    // Click retry button
    await page.getByRole('button', { name: '重试选中' }).click();

    // Check success message
    await expect(page.getByText('重试任务已创建')).toBeVisible();
  });

  test('should show parent task link for retry tasks', async ({ page }) => {
    // Mock task with parent
    await page.route('**/app/batch-tasks/4', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          code: 0,
          data: {
            ...MOCK_FAILED_TASK_DETAIL,
            id: 4,
            parentTaskId: 3,
          },
        }),
      });
    });

    await page.goto('/manager/nodes/batch/tasks/4');

    // Check parent task link
    await expect(page.getByText('重试自任务 #3')).toBeVisible();
  });
});

// ==================== NEW TESTS FOR SCHEDULED TASKS ====================

const MOCK_SCHEDULED_TASKS = [
  {
    id: 5,
    scriptId: 1,
    scriptName: 'Health Check',
    cronExpr: '0 2 * * *',
    isEnabled: true,
    nodeIds: [1, 2, 3],
    totalNodes: 3,
    nextRunAt: Date.now() + 3600000,
    lastRunAt: Date.now() - 86400000,
    lastStatus: 'completed',
    createdAt: Date.now() - 604800000,
  },
  {
    id: 6,
    scriptId: 2,
    scriptName: 'Disk Usage',
    cronExpr: '0 * * * *',
    isEnabled: false,
    nodeIds: [1, 2],
    totalNodes: 2,
    nextRunAt: null,
    lastRunAt: Date.now() - 172800000,
    lastStatus: 'failed',
    createdAt: Date.now() - 1209600000,
  },
];

async function setupScheduledMocks(page: Page) {
  await page.route('**/app/batch-tasks/scheduled', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        code: 0,
        data: {
          items: MOCK_SCHEDULED_TASKS,
          pagination: { page: 1, pageSize: 100, total: MOCK_SCHEDULED_TASKS.length },
        },
      }),
    });
  });

  await page.route('**/app/batch-tasks/5/schedule', async (route) => {
    if (route.request().method() === 'PUT') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          code: 0,
          data: { success: true },
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
}

test.describe('Scheduled Tasks Management', () => {
  test.beforeEach(async ({ page }) => {
    await setupTaskMocks(page);
    await setupScheduledMocks(page);
  });

  test('should show scheduled tasks tab', async ({ page }) => {
    await page.goto('/manager/nodes/batch/tasks');

    // Click scheduled tab
    await page.getByRole('tab', { name: '定时任务' }).click();

    // Check scheduled tasks are shown
    await expect(page.getByText('每天 02:00 执行')).toBeVisible();
    await expect(page.getByText('每小时整点执行')).toBeVisible();
  });

  test('should show enabled/disabled status', async ({ page }) => {
    await page.goto('/manager/nodes/batch/tasks');
    await page.getByRole('tab', { name: '定时任务' }).click();

    // Check enabled badge
    await expect(page.getByText('启用')).toBeVisible();
    await expect(page.getByText('已禁用')).toBeVisible();
  });

  test('should disable scheduled task', async ({ page }) => {
    await page.goto('/manager/nodes/batch/tasks');
    await page.getByRole('tab', { name: '定时任务' }).click();

    // Click disable on enabled task
    const enabledRow = page.locator('tr', { has: page.getByText('每天 02:00 执行') });
    await enabledRow.getByRole('button', { name: '禁用' }).click();

    // Confirm
    await page.getByRole('button', { name: /^禁用$/ }).click();

    // Check success message
    await expect(page.getByText('定时任务已禁用')).toBeVisible();
  });

  test('should create cron task with schedule picker', async ({ page }) => {
    await page.goto('/manager/nodes/batch/tasks');

    // Click create button
    await page.getByRole('button', { name: '创建任务' }).click();

    // Select cron schedule type
    await page.getByLabel('定时执行').check();

    // Select frequency
    await page.getByRole('combobox', { name: /执行频率/ }).click();
    await page.getByRole('option', { name: '每天' }).click();

    // Set time
    await page.locator('input[type="time"]').fill('03:00');

    // Select script and nodes
    await page.getByRole('combobox').first().click();
    await page.getByRole('option', { name: 'Health Check' }).click();
    await page.getByRole('checkbox', { name: /Node 1/ }).check();

    // Submit
    await page.getByRole('button', { name: /^创建$/ }).click();

    // Check success
    await expect(page.getByText('任务创建成功')).toBeVisible();
  });
});
