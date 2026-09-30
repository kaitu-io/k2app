/**
 * 未登录访问账户页 → 跳登录页时必须带 `?next=`：登录页只读 `next`（lib/auth.ts
 * redirectToLogin 同一约定）。曾经这里写成 `?redirect=`，登录后永远落回 /account。
 */
import { render } from '@testing-library/react';
import { describe, it, expect, vi } from 'vitest';
import AccountLayout from '../layout';

const push = vi.fn();

vi.mock('@/contexts/AuthContext', () => ({
  useAuth: () => ({ isAuthenticated: false, isAuthLoading: false, logout: vi.fn() }),
}));
vi.mock('@/components/Header', () => ({ default: () => null }));
vi.mock('@/components/Footer', () => ({ default: () => null }));
vi.mock('@/i18n/routing', () => ({
  useRouter: () => ({ push }),
  usePathname: () => '/account/security',
  Link: ({ children }: { children: React.ReactNode }) => children,
}));
vi.mock('next-intl', () => ({ useTranslations: () => (key: string) => key }));
vi.mock('@/lib/api', () => ({ api: { getUserRouter: vi.fn() } }));

describe('AccountLayout 未登录跳转', () => {
  it('带 ?next=<当前路径> 跳登录页', () => {
    render(<AccountLayout><div /></AccountLayout>);
    expect(push).toHaveBeenCalledWith('/login?next=%2Faccount%2Fsecurity');
  });
});
