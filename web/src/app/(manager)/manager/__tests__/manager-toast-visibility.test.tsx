/**
 * Regression guard: manager 后台的 toast 反馈必须真正渲染出来。
 *
 * (manager) 路由组有自己的 <html> 根 layout，不共享 [locale]/layout.tsx。
 * 在 2026-08-22 之前它从未挂载 <Toaster />，于是 manager 树里 190 处
 * `toast.*(...)` 全部静默 —— 用户看到的症状是「点了按钮完全没反应」
 * （例：管理员重置用户密码，客户端校验失败但没有任何提示）。
 *
 * 这个测试渲染真实的 ManagerLayout 包住真实的 ResetPasswordDialog，
 * 走完整点击路径，断言错误文案出现在 DOM 中。移除 <Toaster /> 会让它变红。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';

vi.mock('@/contexts/AuthContext', () => ({
  useAuth: () => ({
    user: { uuid: 'user-admin', isAdmin: true, roles: 0xffffffff },
    isAuthenticated: true,
    isAuthLoading: false,
  }),
  AuthProvider: ({ children }: { children: React.ReactNode }) => children,
}));

vi.mock('@/components/manager-sidebar', () => ({
  default: () => <nav data-testid="manager-sidebar" />,
}));

const requestMock = vi.fn();
vi.mock('@/lib/api', async () => {
  const actual = await vi.importActual<typeof import('@/lib/api')>('@/lib/api');
  return { ...actual, api: { request: (...args: unknown[]) => requestMock(...args) } };
});

import ManagerLayout from '../layout.kaitu';
import { ResetPasswordDialog } from '../users/detail/components/ResetPasswordDialog';

function renderInManager() {
  return render(
    <ManagerLayout>
      <ResetPasswordDialog
        open
        onOpenChange={() => {}}
        userUUID="user-d9blednrc3n2cihn2ns0"
        userEmail="someone@example.com"
      />
    </ManagerLayout>,
  );
}

function passwordFields() {
  const inputs = document.querySelectorAll<HTMLInputElement>('input[type="password"]');
  return { password: inputs[0], confirm: inputs[1] };
}

describe('manager 后台 toast 可见性', () => {
  beforeEach(() => {
    requestMock.mockReset();
    requestMock.mockResolvedValue({});
  });

  it('客户端校验失败时，错误提示必须可见（而不是被静默吞掉）', async () => {
    renderInManager();

    fireEvent.click(screen.getByRole('button', { name: '确认重置' }));

    expect(requestMock).not.toHaveBeenCalled();
    await waitFor(() => {
      expect(screen.getByText('密码至少 10 位')).toBeInTheDocument();
    });
  });

  it('后端拒绝时，错误提示必须可见且说明具体原因', async () => {
    const { ApiError, ErrorCode } =
      await vi.importActual<typeof import('@/lib/api')>('@/lib/api');
    requestMock.mockRejectedValue(new ApiError(ErrorCode.InvalidArgument, 'password_too_weak'));

    renderInManager();

    const { password, confirm } = passwordFields();
    fireEvent.change(password, { target: { value: 'Correct-Horse-9' } });
    fireEvent.change(confirm, { target: { value: 'Correct-Horse-9' } });
    fireEvent.change(screen.getByRole('textbox'), { target: { value: '工单 #1234' } });
    fireEvent.click(screen.getByRole('button', { name: '确认重置' }));

    await waitFor(() => expect(requestMock).toHaveBeenCalledTimes(1));
    await waitFor(() => {
      expect(screen.getByText(/密码过弱/)).toBeInTheDocument();
    });
  });
});
