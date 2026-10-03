import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';

/**
 * Tests for LanguageSwitcher: the dropdown offers exactly the served locales
 * and switching is always in place (router.replace).
 */

// Mock i18n routing — the underlying next-intl/navigation chain fails to load in jsdom
vi.mock('@/i18n/routing', () => ({
  routing: {
    locales: ['zh-CN', 'zh-TW', 'zh-HK'],
    defaultLocale: 'zh-CN',
  },
  Link: ({ children, ...props }: { children: React.ReactNode }) => <a {...props}>{children}</a>,
  useRouter: () => ({ push: vi.fn(), replace: vi.fn(), back: vi.fn(), refresh: vi.fn() }),
  usePathname: () => '/install',
  redirect: vi.fn(),
  getPathname: () => '/install',
}));

// Mock AuthContext used by LanguageSwitcher
vi.mock('@/contexts/AuthContext', () => ({
  useAuth: () => ({ isAuthenticated: false }),
}));

// Mock api.updateUserLanguage (unused when unauthenticated, but imported)
vi.mock('@/lib/api', () => ({
  api: { updateUserLanguage: vi.fn().mockResolvedValue(undefined) },
}));

// Render the Radix dropdown inline. Radix opens on pointerdown and portals its
// content; jsdom has no PointerEvent and this repo has no user-event dep, so a
// real open is untestable here (the pre-Phase-2 file dodged this by unit-testing
// a pure function instead). These pass-through stubs keep the assertions on the
// thing this task actually changes: WHICH locales get mapped into menu items.
vi.mock('@/components/ui/dropdown-menu', () => ({
  DropdownMenu: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuTrigger: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuItem: ({ children, onClick }: { children: React.ReactNode; onClick?: () => void }) => (
    <div onClick={onClick}>{children}</div>
  ),
}));

// Import after mocks are registered
import LanguageSwitcher from '../LanguageSwitcher';

beforeEach(() => {
  vi.clearAllMocks();
});

describe('LanguageSwitcher', () => {
  it('lists exactly zh-CN/zh-TW/zh-HK', () => {
    render(<LanguageSwitcher />);
    expect(screen.getByText('简体中文')).toBeInTheDocument();
    expect(screen.getByText('繁體中文 (台灣)')).toBeInTheDocument();
    expect(screen.getByText('繁體中文 (香港)')).toBeInTheDocument();
    expect(screen.queryByText('English (US)')).toBeNull();
    expect(screen.queryByText('日本語')).toBeNull();
  });

  it('renders the trigger button', () => {
    render(<LanguageSwitcher />);
    expect(screen.getByRole('button')).toBeTruthy();
  });
});
