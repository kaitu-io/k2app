/**
 * X 广告着陆页 /[locale]/go/[code]。
 *
 * 用真实 zh-CN message 文件渲染（key 回显的替身会让"错层级 key"漏网）。
 * 设备分流在挂载后才定形：SSR 一律给 App Store 按钮，桌面挂载后换成二维码卡。
 */
import React from 'react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, act } from '@testing-library/react';
import fs from 'fs';
import path from 'path';

function loadMessages(locale: string): Record<string, unknown> {
  const dir = path.resolve(__dirname, '../messages', locale);
  return Object.fromEntries(
    fs.readdirSync(dir).filter((f) => f.endsWith('.json')).map((f) => [
      f.replace('.json', ''),
      JSON.parse(fs.readFileSync(path.join(dir, f), 'utf8')),
    ]),
  );
}
function lookup(root: unknown, key: string): unknown {
  return key.split('.').reduce<unknown>(
    (acc, part) => (acc && typeof acc === 'object' ? (acc as Record<string, unknown>)[part] : undefined),
    root,
  );
}
function makeTranslator(locale: string, namespace?: string) {
  const messages = loadMessages(locale);
  const root = namespace ? lookup(messages, namespace) : messages;
  return (key: string) => {
    const v = lookup(root, key);
    return typeof v === 'string' ? v : key;
  };
}

vi.mock('next-intl', () => ({
  useTranslations: (ns?: string) => makeTranslator('zh-CN', ns),
  useLocale: () => 'zh-CN',
}));
vi.mock('next-intl/server', () => ({
  getTranslations: async (opts: { locale: string; namespace?: string } | string) =>
    typeof opts === 'string' ? makeTranslator('zh-CN', opts) : makeTranslator(opts.locale, opts.namespace),
  setRequestLocale: vi.fn(),
}));
vi.mock('server-only', () => ({}));
vi.mock('@/i18n/routing', () => ({
  routing: { locales: ['zh-CN', 'zh-TW', 'zh-HK'], defaultLocale: 'zh-CN' },
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  Link: ({ children, ...p }: any) => <a {...p}>{children}</a>,
}));
vi.mock('next/image', () => ({
  // eslint-disable-next-line @typescript-eslint/no-explicit-any, @next/next/no-img-element, jsx-a11y/alt-text, @typescript-eslint/no-unused-vars
  default: ({ priority, fill, ...p }: any) => <img {...p} />,
}));
vi.mock('@/lib/funnel', () => ({ track: vi.fn() }));

import { track } from '@/lib/funnel';
import AdLandingClient from '../src/app/[locale]/go/[code]/AdLandingClient';

const M = (loadMessages('zh-CN').adLanding as { adLanding: Record<string, any> }).adLanding; // eslint-disable-line @typescript-eslint/no-explicit-any
const APP_STORE = 'https://apps.apple.com/app/id6448744655';
const IPHONE_UA = 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 Mobile/15E148';
const MAC_UA = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 Version/18.0 Safari/605.1.15';
const ANDROID_UA = 'Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 Chrome/129 Mobile Safari/537.36';

function setDevice(ua: string, touch: number) {
  vi.spyOn(window.navigator, 'userAgent', 'get').mockReturnValue(ua);
  // jsdom 没有 maxTouchPoints，直接定义在实例上
  Object.defineProperty(window.navigator, 'maxTouchPoints', { value: touch, configurable: true });
}

function renderClient(variant: 'stable' | 'compare' = 'stable') {
  return render(
    <AdLandingClient code="xa1" variant={variant} appStoreHref={APP_STORE} qrSvg='<svg data-testid="qr"></svg>' />,
  );
}

beforeEach(() => {
  vi.mocked(track).mockClear();
});
afterEach(() => {
  vi.restoreAllMocks();
});

describe('AdLandingClient', () => {
  it('SSR defaults to the App Store CTA (no QR flash) for every variant', async () => {
    const { renderToString } = await import('react-dom/server');
    for (const variant of ['stable', 'compare'] as const) {
      const html = renderToString(
        <AdLandingClient code="xa1" variant={variant} appStoreHref={APP_STORE} qrSvg="<svg></svg>" />,
      );
      expect(html).toContain(M.ctaAppStore);
      expect(html).toContain(`href="${APP_STORE}"`);
      expect(html).not.toContain(M.qrTitle);
    }
    expect(track).not.toHaveBeenCalled();
  });

  it('desktop UA after mount shows the QR card and reports qr_view once', async () => {
    setDevice(MAC_UA, 0);
    const { rerender } = renderClient();
    await act(async () => {});
    expect(screen.getByText(M.qrTitle)).toBeInTheDocument();
    expect(document.querySelector('[data-testid="qr"]')).not.toBeNull();
    rerender(<AdLandingClient code="xa1" variant="stable" appStoreHref={APP_STORE} qrSvg='<svg data-testid="qr"></svg>' />);
    const qrCalls = vi.mocked(track).mock.calls.filter(([e]) => e === 'qr_view');
    expect(qrCalls).toHaveLength(1);
    expect(qrCalls[0][1]).toMatchObject({ source: 'ad' });
  });

  it('iPhone tap on CTA reports install_click with plan ios', async () => {
    setDevice(IPHONE_UA, 5);
    renderClient();
    await act(async () => {});
    expect(screen.queryByText(M.qrTitle)).toBeNull();
    const ctas = screen.getAllByText(M.ctaAppStore);
    expect(ctas[0].closest('a')).toHaveAttribute('href', APP_STORE);
    fireEvent.click(ctas[0]);
    expect(track).toHaveBeenCalledWith('install_click', { plan: 'ios', source: 'ad' });
  });

  it('android shows the download CTA to /install and reports install_click android', async () => {
    setDevice(ANDROID_UA, 5);
    renderClient();
    await act(async () => {});
    expect(screen.queryByText(M.ctaAppStore)).toBeNull();
    const ctas = screen.getAllByText(M.ctaAndroid);
    expect(ctas[0].closest('a')).toHaveAttribute('href', '/install');
    fireEvent.click(ctas[0]);
    expect(track).toHaveBeenCalledWith('install_click', { plan: 'android', source: 'ad' });
  });

  it('compare variant renders the compare subtitle', () => {
    renderClient('compare');
    expect(screen.getByText(M.subtitle.compare)).toBeInTheDocument();
    expect(screen.queryByText(M.subtitle.stable)).toBeNull();
  });
});

describe('/[locale]/go/[code] page', () => {
  it('generateMetadata marks the page noindex', async () => {
    const { generateMetadata } = await import('../src/app/[locale]/go/[code]/page');
    const meta = await generateMetadata({ params: Promise.resolve({ locale: 'zh-CN', code: 'xa1' }) });
    expect(meta.robots).toEqual({ index: false, follow: false });
    expect(meta.title).toBe(M.metaTitle);
  });

  it('unknown code renders the stable variant instead of 404', async () => {
    const { default: Page } = await import('../src/app/[locale]/go/[code]/page');
    const element = await Page({
      params: Promise.resolve({ locale: 'zh-CN', code: 'zz9' }),
      searchParams: Promise.resolve({ utm_source: 'x', twclid: 'abc' }),
    });
    render(element);
    expect(screen.getByText(M.subtitle.stable)).toBeInTheDocument();
    expect(screen.getByText(M.heroTitle)).toBeInTheDocument();
    expect(screen.getByAltText(M.heroAlt)).toHaveAttribute('src', '/ads/x-hero.jpg');
  });
});
