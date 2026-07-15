import React from 'react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';

/**
 * Tests for LanguageSwitcher — Brand Split Phase 2.
 *
 * Each deployment is single-brand, so the dropdown offers only the baked
 * brand's own locales and switching is always in place (router.replace).
 * The old cross-domain `computeSwitchAction` is gone with the dual-host model.
 */

// Mock i18n routing — the underlying next-intl/navigation chain fails to load in jsdom
vi.mock('@/i18n/routing', () => ({
  routing: {
    locales: ['en-US', 'en-GB', 'en-AU', 'zh-CN', 'zh-TW', 'zh-HK', 'ja'],
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

afterEach(() => vi.unstubAllEnvs());

describe('LanguageSwitcher — brand-local locales only', () => {
  it('kaitu build lists exactly zh-CN/zh-TW/zh-HK', () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'kaitu');
    render(<LanguageSwitcher />);
    expect(screen.getByText('简体中文')).toBeInTheDocument();
    expect(screen.getByText('繁體中文 (台灣)')).toBeInTheDocument();
    expect(screen.getByText('繁體中文 (香港)')).toBeInTheDocument();
    expect(screen.queryByText('English (US)')).toBeNull();
    expect(screen.queryByText('日本語')).toBeNull();
  });

  it('overleap build lists exactly en-US/en-GB/en-AU/ja', () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'overleap');
    render(<LanguageSwitcher />);
    expect(screen.getByText('English (US)')).toBeInTheDocument();
    expect(screen.getByText('English (UK)')).toBeInTheDocument();
    expect(screen.getByText('English (AU)')).toBeInTheDocument();
    expect(screen.getByText('日本語')).toBeInTheDocument();
    expect(screen.queryByText('简体中文')).toBeNull();
    expect(screen.queryByText('繁體中文 (台灣)')).toBeNull();
  });
});

describe('LanguageSwitcher (render)', () => {
  it('renders the trigger button on the kaitu build', () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'kaitu');
    render(<LanguageSwitcher />);
    expect(screen.getByRole('button')).toBeTruthy();
  });

  it('mounts on the overleap build without crashing', () => {
    vi.stubEnv('NEXT_PUBLIC_BRAND', 'overleap');
    render(<LanguageSwitcher />);
    expect(screen.getByRole('button')).toBeTruthy();
  });
});
