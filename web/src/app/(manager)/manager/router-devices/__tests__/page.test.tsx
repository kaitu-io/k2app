/**
 * `/manager/router-devices` 从表页：设备列表、在线状态、跳转台账。
 */
import { render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import type { AdminRouterDeviceItem } from '@/lib/api';

const mockListRouterDevices = vi.fn();

const routerState = vi.hoisted(() => ({ current: { push: vi.fn() } }));
const searchParamsState = vi.hoisted(() => ({ current: new URLSearchParams() }));

vi.mock('next/navigation', () => ({
  useRouter: () => routerState.current,
  useSearchParams: () => searchParamsState.current,
}));

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>();
  return {
    ...actual,
    api: {
      listRouterDevices: (...a: unknown[]) => mockListRouterDevices(...a),
    },
  };
});

import RouterDevicesPage from '../page.kaitu';

function items(): AdminRouterDeviceItem[] {
  return [
    {
      id: 1,
      userId: 11,
      email: 'user1@example.com',
      udid: 'abcdefghijklmno',
      appVersion: '1.2.3',
      appArch: 'arm64',
      lastSeenAt: 0,
      online: false,
    },
    {
      id: 2,
      userId: 22,
      email: 'user2@example.com',
      udid: 'xyz0123456789abcdef',
      appVersion: '1.2.3',
      appArch: 'amd64',
      lastSeenAt: 1_700_000_000,
      online: true,
    },
  ];
}

async function findRow(email: string) {
  const cell = await screen.findByText(email);
  return cell.closest('tr')!;
}

describe('/manager/router-devices', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    routerState.current = { push: vi.fn() };
    searchParamsState.current = new URLSearchParams();
    mockListRouterDevices.mockResolvedValue({
      items: items(),
      pagination: { page: 1, pageSize: 50, total: 2 },
    });
  });

  it('lastSeenAt 为 0 显示「从未连接」；online 为 true 显示「在线」', async () => {
    render(<RouterDevicesPage />);
    const row1 = await findRow('user1@example.com');
    const row2 = await findRow('user2@example.com');

    expect(within(row1).getByText('从未连接')).toBeInTheDocument();
    expect(within(row2).getByText('在线')).toBeInTheDocument();
  });

  it('操作列的查看台账链接带 userId 跳转到路由器订单台账', async () => {
    render(<RouterDevicesPage />);
    const row1 = await findRow('user1@example.com');
    const row2 = await findRow('user2@example.com');

    expect(within(row1).getByRole('link', { name: '查看台账' })).toHaveAttribute(
      'href',
      '/manager/router-fulfillments?userId=11'
    );
    expect(within(row2).getByRole('link', { name: '查看台账' })).toHaveAttribute(
      'href',
      '/manager/router-fulfillments?userId=22'
    );
  });

  it('输入用户 ID 回车后 URL 带 userId 与 page=1', async () => {
    render(<RouterDevicesPage />);
    await screen.findByText('user1@example.com');

    const input = screen.getByPlaceholderText('例如 1234');
    fireEvent.change(input, { target: { value: '99' } });
    fireEvent.keyDown(input, { key: 'Enter' });

    await waitFor(() => expect(routerState.current.push).toHaveBeenCalled());
    const calls = (routerState.current.push as ReturnType<typeof vi.fn>).mock.calls;
    const url = calls[calls.length - 1][0] as string;
    const params = new URLSearchParams(url.split('?')[1] || '');
    expect(params.get('userId')).toBe('99');
    expect(params.get('page')).toBe('1');
  });
});
