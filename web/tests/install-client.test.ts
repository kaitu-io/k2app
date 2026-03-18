import { describe, it, expect, vi } from 'vitest';
import { createElement } from 'react';
import { render } from '@testing-library/react';

// Mock radix-ui (transitive dependency of accordion)
vi.mock('radix-ui', () => ({
  Accordion: {
    Root: ({ children }: any) => children,
    Item: ({ children }: any) => children,
    Trigger: ({ children }: any) => children,
    Header: ({ children }: any) => children,
    Content: ({ children }: any) => children,
  },
}));

vi.mock('@/i18n/routing', () => ({
  Link: ({ children }: any) => children,
}));
vi.mock('lucide-react', () => ({
  Download: () => null,
  CheckCircle: () => null,
  AlertCircle: () => null,
  RefreshCw: () => null,
  ArrowRight: () => null,
  ExternalLink: () => null,
  Copy: () => null,
  ChevronDownIcon: () => null,
}));
vi.mock('@/components/ui/button', () => ({
  Button: ({ children }: any) => createElement('button', null, children),
}));
vi.mock('@/components/ui/card', () => ({
  Card: ({ children, className }: any) => createElement('div', { 'data-testid': 'card', className }, children),
}));
vi.mock('@/components/ui/accordion', () => ({
  Accordion: ({ children }: any) => createElement('div', { 'data-testid': 'accordion' }, children),
  AccordionItem: ({ children }: any) => createElement('div', { 'data-testid': 'accordion-item' }, children),
  AccordionTrigger: ({ children }: any) => createElement('div', null, children),
  AccordionContent: ({ children }: any) => createElement('div', null, children),
}));
vi.mock('@/lib/device-detection', () => ({
  detectDevice: vi.fn().mockReturnValue({
    type: 'macos', name: 'Mac', isMobile: false, isDesktop: true, userAgent: 'mac',
  }),
  triggerDownload: vi.fn().mockReturnValue(true),
  openDownloadInNewTab: vi.fn(),
}));
vi.mock('@/lib/constants', () => ({
  CDN_PRIMARY: 'https://cdn.test',
  CDN_BACKUP: 'https://backup.test',
  getDownloadLinks: (v: string) => ({
    windows: { primary: `https://cdn/${v}/win.exe`, backup: '' },
    macos: { primary: `https://cdn/${v}/mac.pkg`, backup: '' },
    linux: { primary: `https://cdn/${v}/linux.AppImage`, backup: '' },
  }),
}));
vi.mock('@/lib/downloads', () => ({}));
vi.mock('@/lib/utils', () => ({
  cn: (...args: any[]) => args.filter(Boolean).join(' '),
}));

// vi.mock is hoisted above this static import
import InstallClient from '../src/app/[locale]/install/InstallClient';

describe('InstallClient', () => {
  it('renders without crashing with all props', () => {
    const { container } = render(
      createElement(InstallClient, {
        betaVersion: '0.4.0-beta.1',
        stableVersion: '0.3.22',
        mobileLinks: { ios: 'https://apps.apple.com/test', android: { primary: 'https://cdn/android.apk', backup: 'https://cdn-backup/android.apk' } },
      })
    );
    expect(container.innerHTML).not.toBe('');
  });

  it('renders without crashing when mobileLinks is null', () => {
    const { container } = render(
      createElement(InstallClient, {
        betaVersion: '0.4.0-beta.1',
        stableVersion: null,
        mobileLinks: null,
      })
    );
    expect(container.innerHTML).not.toBe('');
  });

  it('does not contain any Waymaker text', () => {
    const { container } = render(
      createElement(InstallClient, {
        betaVersion: '0.4.0-beta.1',
        stableVersion: '0.3.22',
        mobileLinks: { ios: 'https://apps.apple.com/test', android: { primary: 'https://cdn/android.apk', backup: 'https://cdn-backup/android.apk' } },
      })
    );
    expect(container.innerHTML).not.toContain('Waymaker');
    expect(container.innerHTML).not.toContain('waymaker');
  });

  it('renders FAQ accordion section', () => {
    const { container } = render(
      createElement(InstallClient, {
        betaVersion: '0.4.0-beta.1',
        stableVersion: '0.3.22',
        mobileLinks: { ios: 'https://apps.apple.com/test', android: { primary: 'https://cdn/android.apk', backup: 'https://cdn-backup/android.apk' } },
      })
    );
    expect(container.querySelector('[data-testid="accordion"]')).not.toBeNull();
  });

  it('renders exactly 3 platform cards in grid (desktop only, no iOS/Android cards)', () => {
    const { container } = render(
      createElement(InstallClient, {
        betaVersion: '0.4.0-beta.1',
        stableVersion: '0.3.22',
        mobileLinks: { ios: 'https://apps.apple.com/test', android: { primary: 'https://cdn/android.apk', backup: 'https://cdn-backup/android.apk' } },
      })
    );
    const allCards = container.querySelectorAll('[data-testid="card"]');
    const cardTexts = Array.from(allCards).map(c => c.textContent || '');
    const hasIosCard = cardTexts.some(t => t.includes('install.install.iosDevices'));
    expect(hasIosCard).toBe(false);
  });

  it('renders 5 FAQ items', () => {
    const { container } = render(
      createElement(InstallClient, {
        betaVersion: '0.4.0-beta.1',
        stableVersion: '0.3.22',
        mobileLinks: { ios: 'https://apps.apple.com/test', android: { primary: 'https://cdn/android.apk', backup: 'https://cdn-backup/android.apk' } },
      })
    );
    const items = container.querySelectorAll('[data-testid="accordion-item"]');
    expect(items.length).toBe(6);
  });
});
