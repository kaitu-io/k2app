import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import WeChatBrowserGuide from '../WeChatBrowserGuide';

/**
 * Component test for WeChatBrowserGuide.
 *
 * The test/setup.ts mock of `useTranslations` returns the key itself, so we
 * assert on i18n keys rather than localized strings. Asserting on the KEYS is
 * actually stronger: it catches typos in both the component and any *.json
 * namespace file whose key the component expects.
 */
describe('WeChatBrowserGuide', () => {
  it('renders with the maximum 32-bit-int zIndex to guarantee top stacking', () => {
    const { container } = render(<WeChatBrowserGuide />);
    const overlay = container.firstElementChild as HTMLElement;
    expect(overlay).toBeTruthy();
    // Inline style is the contract — Tailwind arbitrary z-* got silently dropped
    // by the compiler in our CSS v4 setup, so we lock this via style attribute.
    expect(overlay.style.zIndex).toBe('2147483647');
  });

  it('covers the full viewport (fixed inset-0)', () => {
    const { container } = render(<WeChatBrowserGuide />);
    const overlay = container.firstElementChild as HTMLElement;
    expect(overlay.className).toMatch(/\bfixed\b/);
    expect(overlay.className).toMatch(/\binset-0\b/);
  });

  it('renders all 5 i18n keys (arrow label, title, 2 steps, reason)', () => {
    render(<WeChatBrowserGuide />);
    expect(screen.getByText('purchase.wechatGuide.tapHere')).toBeInTheDocument();
    expect(screen.getByText('purchase.wechatGuide.title')).toBeInTheDocument();
    expect(screen.getByText('purchase.wechatGuide.step1')).toBeInTheDocument();
    expect(screen.getByText('purchase.wechatGuide.step2')).toBeInTheDocument();
    expect(screen.getByText('purchase.wechatGuide.reason')).toBeInTheDocument();
  });

  it('renders numbered step badges (1 and 2)', () => {
    render(<WeChatBrowserGuide />);
    expect(screen.getByText('1')).toBeInTheDocument();
    expect(screen.getByText('2')).toBeInTheDocument();
  });

  it('is scrollable so cramped viewports (iPhone SE, etc.) do not clip content', () => {
    const { container } = render(<WeChatBrowserGuide />);
    const overlay = container.firstElementChild as HTMLElement;
    expect(overlay.className).toMatch(/overflow-y-auto/);
  });
});
