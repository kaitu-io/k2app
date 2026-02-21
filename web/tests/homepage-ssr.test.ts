/**
 * Homepage SSR Tests — T1
 *
 * Vitest tests for SSR conversion of the homepage.
 * RED phase: tests fail before implementation.
 *
 * Tests verify:
 * 1. The page component is an async function (Server Component compatible)
 * 2. The page accepts params with locale (Next.js 15 async params pattern)
 * 3. The exported generateMetadata function returns title and description
 */
import { describe, it, expect, vi } from 'vitest';

// Mock next-intl/server (Server Component API)
vi.mock('next-intl/server', () => ({
  getTranslations: vi.fn().mockResolvedValue((key: string) => key),
  setRequestLocale: vi.fn(),
}));

// Mock next-intl (Client Component API — used by child components)
vi.mock('next-intl', () => ({
  useTranslations: () => (key: string) => key,
  useLocale: () => 'zh-CN',
}));

// Mock @/i18n/routing
vi.mock('@/i18n/routing', () => ({
  routing: {
    locales: ['zh-CN', 'en-US', 'en-GB', 'en-AU', 'zh-TW', 'zh-HK', 'ja'],
  },
  Link: ({ children }: { children: React.ReactNode }) => children,
}));

// Mock next/dynamic (used for MPTCPVisualization)
vi.mock('next/dynamic', () => ({
  default: () => () => null,
}));

// Mock @/components/MPTCPVisualization
vi.mock('@/components/MPTCPVisualization', () => ({
  default: () => null,
}));

// Mock @/components/Header
vi.mock('@/components/Header', () => ({
  default: () => null,
}));

// Mock @/components/Footer
vi.mock('@/components/Footer', () => ({
  default: () => null,
}));

// Mock @/lib/constants
vi.mock('@/lib/constants', () => ({
  DOWNLOAD_LINKS: {
    ios: null,
    android: null,
    windows: 'https://example.com/windows',
    macos: 'https://example.com/macos',
  },
}));

// Mock lucide-react icons
vi.mock('lucide-react', () => ({
  Download: () => null,
  ExternalLink: () => null,
  Smartphone: () => null,
  Monitor: () => null,
  Router: () => null,
}));

// Mock shadcn/ui components
vi.mock('@/components/ui/button', () => ({
  Button: ({ children }: { children: React.ReactNode }) => children,
}));

vi.mock('@/components/ui/card', () => ({
  Card: ({ children }: { children: React.ReactNode }) => children,
}));

describe('test_homepage_ssr_renders_content', () => {
  it('page component is an async function (Server Component pattern)', async () => {
    const { default: Home } = await import('../src/app/[locale]/page');

    // Must be an async function to qualify as a Server Component that awaits params
    expect(Home).toBeTypeOf('function');
    const result = Home({ params: Promise.resolve({ locale: 'zh-CN' }) });
    expect(result).toBeInstanceOf(Promise);
  });

  it('page accepts params as a Promise<{ locale: string }> (Next.js 15 pattern)', async () => {
    const { default: Home } = await import('../src/app/[locale]/page');

    // Should resolve without throwing — async params are awaited inside
    const element = await Home({ params: Promise.resolve({ locale: 'zh-CN' }) });
    expect(element).not.toBeNull();
  });

  it('page renders JSX content (not null or empty)', async () => {
    const { default: Home } = await import('../src/app/[locale]/page');

    const element = await Home({ params: Promise.resolve({ locale: 'zh-CN' }) });

    // Must return a React element (object with $$typeof or type), not null/undefined
    expect(element).toBeDefined();
    expect(element).not.toBeNull();
  });
});

describe('test_homepage_generates_metadata', () => {
  it('generateMetadata is exported from the page module', async () => {
    const pageModule = await import('../src/app/[locale]/page');

    expect(pageModule.generateMetadata).toBeDefined();
    expect(pageModule.generateMetadata).toBeTypeOf('function');
  });

  it('generateMetadata returns an object with title field', async () => {
    const { generateMetadata } = await import('../src/app/[locale]/page');

    const metadata = await generateMetadata({
      params: Promise.resolve({ locale: 'zh-CN' }),
    });

    expect(metadata).toHaveProperty('title');
    expect(metadata.title).toBeTruthy();
  });

  it('generateMetadata returns an object with description field', async () => {
    const { generateMetadata } = await import('../src/app/[locale]/page');

    const metadata = await generateMetadata({
      params: Promise.resolve({ locale: 'en-US' }),
    });

    expect(metadata).toHaveProperty('description');
    expect(metadata.description).toBeTruthy();
  });
});
