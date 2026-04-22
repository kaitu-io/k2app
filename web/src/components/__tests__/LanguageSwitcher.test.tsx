import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';

/**
 * Tests for LanguageSwitcher — brand-aware cross-domain locale switching.
 *
 * The switching logic is extracted into a pure function `computeSwitchAction`
 * so it can be unit-tested without wrestling with Radix dropdown portals in
 * jsdom. A light integration test verifies the component mounts under both
 * brand providers.
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

// Import after mocks are registered
import LanguageSwitcher, { computeSwitchAction } from '../LanguageSwitcher';
import { BrandProvider } from '@/components/providers/BrandProvider';
import { KAITU, OVERLEAP } from '@/lib/brands';

describe('computeSwitchAction (pure)', () => {
  it('returns {type:"assign"} when switching kaitu→overleap on production kaitu.io', () => {
    const action = computeSwitchAction({
      newLocale: 'en-US',
      pathname: '/install',
      currentBrand: 'kaitu',
      currentHost: 'kaitu.io',
      search: '',
      hash: '',
    });
    expect(action).toEqual({
      type: 'assign',
      url: 'https://overleap.io/en-US/install',
    });
  });

  it('returns {type:"router"} when switching between same-brand locales on production', () => {
    const action = computeSwitchAction({
      newLocale: 'zh-HK',
      pathname: '/install',
      currentBrand: 'kaitu',
      currentHost: 'kaitu.io',
      search: '',
      hash: '',
    });
    expect(action).toEqual({
      type: 'router',
      pathname: '/install',
      locale: 'zh-HK',
    });
  });

  it('returns {type:"router"} when on non-production host, even for cross-brand locale', () => {
    const action = computeSwitchAction({
      newLocale: 'en-US',
      pathname: '/install',
      currentBrand: 'kaitu',
      currentHost: 'localhost:3000',
      search: '',
      hash: '',
    });
    expect(action).toEqual({
      type: 'router',
      pathname: '/install',
      locale: 'en-US',
    });
  });

  it('returns {type:"assign"} when switching overleap→kaitu on production overleap.io', () => {
    const action = computeSwitchAction({
      newLocale: 'zh-CN',
      pathname: '/install',
      currentBrand: 'overleap',
      currentHost: 'overleap.io',
      search: '',
      hash: '',
    });
    expect(action).toEqual({
      type: 'assign',
      url: 'https://kaitu.io/zh-CN/install',
    });
  });

  it('returns {type:"router"} when switching to ja on overleap.io (ja is Overleap-owned)', () => {
    const action = computeSwitchAction({
      newLocale: 'ja',
      pathname: '/install',
      currentBrand: 'overleap',
      currentHost: 'overleap.io',
      search: '',
      hash: '',
    });
    expect(action).toEqual({
      type: 'router',
      pathname: '/install',
      locale: 'ja',
    });
  });

  it('preserves query string and hash on cross-brand assign URL', () => {
    const action = computeSwitchAction({
      newLocale: 'en-US',
      pathname: '/purchase',
      currentBrand: 'kaitu',
      currentHost: 'kaitu.io',
      search: '?ref=abc',
      hash: '#section',
    });
    expect(action).toEqual({
      type: 'assign',
      url: 'https://overleap.io/en-US/purchase?ref=abc#section',
    });
  });

  it('also treats www-prefixed production hosts as production', () => {
    const action = computeSwitchAction({
      newLocale: 'en-US',
      pathname: '/install',
      currentBrand: 'kaitu',
      currentHost: 'www.kaitu.io',
      search: '',
      hash: '',
    });
    expect(action.type).toBe('assign');
  });
});

describe('LanguageSwitcher (render)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders the trigger button inside KAITU brand provider', () => {
    render(
      <BrandProvider brand={KAITU}>
        <LanguageSwitcher />
      </BrandProvider>,
    );
    expect(screen.getByRole('button')).toBeTruthy();
  });

  it('mounts inside OVERLEAP brand provider without crashing', () => {
    render(
      <BrandProvider brand={OVERLEAP}>
        <LanguageSwitcher />
      </BrandProvider>,
    );
    expect(screen.getByRole('button')).toBeTruthy();
  });
});
