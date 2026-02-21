/**
 * Interactive Island Pages SSR Tests — T3
 *
 * Vitest tests for SSR conversion of discovery, opensource, and changelog pages.
 * RED phase: tests fail before implementation.
 *
 * Tests verify:
 * 1. Each page component is an async function (Server Component compatible)
 * 2. Each page accepts params as Promise<{ locale: string }> (Next.js 15 pattern)
 * 3. Each page exports generateMetadata with title + description
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

// Mock @/components/Header
vi.mock('@/components/Header', () => ({
  default: () => null,
}));

// Mock @/components/Footer
vi.mock('@/components/Footer', () => ({
  default: () => null,
}));

// Mock next/image
vi.mock('next/image', () => ({
  default: () => null,
}));

// Mock next/link
vi.mock('next/link', () => ({
  default: ({ children }: { children: React.ReactNode }) => children,
}));

// Mock next/navigation
vi.mock('next/navigation', () => ({
  useSearchParams: () => new URLSearchParams(),
  usePathname: () => '/',
}));

// Mock @/hooks/useEmbedMode
vi.mock('@/hooks/useEmbedMode', () => ({
  useEmbedMode: () => ({
    isEmbedded: false,
    showNavigation: true,
    showFooter: true,
    compactLayout: false,
    authToken: null,
    embedTheme: null,
  }),
}));

// Mock lucide-react icons
vi.mock('lucide-react', () => ({
  Globe: () => null,
  Smartphone: () => null,
  Video: () => null,
  ExternalLink: () => null,
  Users: () => null,
  BookOpen: () => null,
  ShoppingBag: () => null,
  Tv: () => null,
  Newspaper: () => null,
  Github: () => null,
  Calendar: () => null,
  Clock: () => null,
  Heart: () => null,
  FileText: () => null,
  ChevronDown: () => null,
  ChevronUp: () => null,
  Package: () => null,
}));

// Mock shadcn/ui components
vi.mock('@/components/ui/button', () => ({
  Button: ({ children }: { children: React.ReactNode }) => children,
}));

vi.mock('@/components/ui/card', () => ({
  Card: ({ children }: { children: React.ReactNode }) => children,
}));

vi.mock('@/components/ui/badge', () => ({
  Badge: ({ children }: { children: React.ReactNode }) => children,
}));

// ============================================================================
// Discovery Page Tests
// ============================================================================

describe('test_discovery_ssr_renders_content', () => {
  it('page component is an async function (Server Component pattern)', async () => {
    const { default: DiscoveryPage } = await import('../src/app/[locale]/discovery/page');

    // Must be an async function to qualify as a Server Component that awaits params
    expect(DiscoveryPage).toBeTypeOf('function');
    const result = DiscoveryPage({ params: Promise.resolve({ locale: 'zh-CN' }) });
    expect(result).toBeInstanceOf(Promise);
  });

  it('page accepts params as a Promise<{ locale: string }> (Next.js 15 pattern)', async () => {
    const { default: DiscoveryPage } = await import('../src/app/[locale]/discovery/page');

    // Should resolve without throwing — async params are awaited inside
    const element = await DiscoveryPage({ params: Promise.resolve({ locale: 'zh-CN' }) });
    expect(element).not.toBeNull();
  });

  it('page renders JSX content (not null or empty)', async () => {
    const { default: DiscoveryPage } = await import('../src/app/[locale]/discovery/page');

    const element = await DiscoveryPage({ params: Promise.resolve({ locale: 'zh-CN' }) });

    // Must return a React element (object with $$typeof or type), not null/undefined
    expect(element).toBeDefined();
    expect(element).not.toBeNull();
  });
});

describe('test_discovery_generates_metadata', () => {
  it('generateMetadata is exported from the discovery page module', async () => {
    const pageModule = await import('../src/app/[locale]/discovery/page');

    expect(pageModule.generateMetadata).toBeDefined();
    expect(pageModule.generateMetadata).toBeTypeOf('function');
  });

  it('generateMetadata returns an object with title field', async () => {
    const { generateMetadata } = await import('../src/app/[locale]/discovery/page');

    const metadata = await generateMetadata({
      params: Promise.resolve({ locale: 'zh-CN' }),
    });

    expect(metadata).toHaveProperty('title');
    expect(metadata.title).toBeTruthy();
  });

  it('generateMetadata returns an object with description field', async () => {
    const { generateMetadata } = await import('../src/app/[locale]/discovery/page');

    const metadata = await generateMetadata({
      params: Promise.resolve({ locale: 'en-US' }),
    });

    expect(metadata).toHaveProperty('description');
    expect(metadata.description).toBeTruthy();
  });
});

// ============================================================================
// Opensource Page Tests
// ============================================================================

describe('test_opensource_ssr_renders_content', () => {
  it('page component is an async function (Server Component pattern)', async () => {
    const { default: OpenSourcePage } = await import('../src/app/[locale]/opensource/page');

    // Must be an async function to qualify as a Server Component that awaits params
    expect(OpenSourcePage).toBeTypeOf('function');
    const result = OpenSourcePage({ params: Promise.resolve({ locale: 'zh-CN' }) });
    expect(result).toBeInstanceOf(Promise);
  });

  it('page accepts params as a Promise<{ locale: string }> (Next.js 15 pattern)', async () => {
    const { default: OpenSourcePage } = await import('../src/app/[locale]/opensource/page');

    // Should resolve without throwing — async params are awaited inside
    const element = await OpenSourcePage({ params: Promise.resolve({ locale: 'zh-CN' }) });
    expect(element).not.toBeNull();
  });

  it('page renders JSX content (not null or empty)', async () => {
    const { default: OpenSourcePage } = await import('../src/app/[locale]/opensource/page');

    const element = await OpenSourcePage({ params: Promise.resolve({ locale: 'zh-CN' }) });

    // Must return a React element (object with $$typeof or type), not null/undefined
    expect(element).toBeDefined();
    expect(element).not.toBeNull();
  });
});

describe('test_opensource_generates_metadata', () => {
  it('generateMetadata is exported from the opensource page module', async () => {
    const pageModule = await import('../src/app/[locale]/opensource/page');

    expect(pageModule.generateMetadata).toBeDefined();
    expect(pageModule.generateMetadata).toBeTypeOf('function');
  });

  it('generateMetadata returns an object with title field', async () => {
    const { generateMetadata } = await import('../src/app/[locale]/opensource/page');

    const metadata = await generateMetadata({
      params: Promise.resolve({ locale: 'zh-CN' }),
    });

    expect(metadata).toHaveProperty('title');
    expect(metadata.title).toBeTruthy();
  });

  it('generateMetadata returns an object with description field', async () => {
    const { generateMetadata } = await import('../src/app/[locale]/opensource/page');

    const metadata = await generateMetadata({
      params: Promise.resolve({ locale: 'en-US' }),
    });

    expect(metadata).toHaveProperty('description');
    expect(metadata.description).toBeTruthy();
  });
});

// ============================================================================
// Changelog Page Tests
// ============================================================================

describe('test_changelog_ssr_renders_content', () => {
  it('page component is an async function (Server Component pattern)', async () => {
    const { default: ChangelogPage } = await import('../src/app/[locale]/changelog/page');

    // Must be an async function to qualify as a Server Component that awaits params
    expect(ChangelogPage).toBeTypeOf('function');
    const result = ChangelogPage({ params: Promise.resolve({ locale: 'zh-CN' }) });
    expect(result).toBeInstanceOf(Promise);
  });

  it('page accepts params as a Promise<{ locale: string }> (Next.js 15 pattern)', async () => {
    const { default: ChangelogPage } = await import('../src/app/[locale]/changelog/page');

    // Should resolve without throwing — async params are awaited inside
    const element = await ChangelogPage({ params: Promise.resolve({ locale: 'zh-CN' }) });
    expect(element).not.toBeNull();
  });

  it('page renders JSX content (not null or empty)', async () => {
    const { default: ChangelogPage } = await import('../src/app/[locale]/changelog/page');

    const element = await ChangelogPage({ params: Promise.resolve({ locale: 'zh-CN' }) });

    // Must return a React element (object with $$typeof or type), not null/undefined
    expect(element).toBeDefined();
    expect(element).not.toBeNull();
  });
});

describe('test_changelog_generates_metadata', () => {
  it('generateMetadata is exported from the changelog page module', async () => {
    const pageModule = await import('../src/app/[locale]/changelog/page');

    expect(pageModule.generateMetadata).toBeDefined();
    expect(pageModule.generateMetadata).toBeTypeOf('function');
  });

  it('generateMetadata returns an object with title field', async () => {
    const { generateMetadata } = await import('../src/app/[locale]/changelog/page');

    const metadata = await generateMetadata({
      params: Promise.resolve({ locale: 'zh-CN' }),
    });

    expect(metadata).toHaveProperty('title');
    expect(metadata.title).toBeTruthy();
  });

  it('generateMetadata returns an object with description field', async () => {
    const { generateMetadata } = await import('../src/app/[locale]/changelog/page');

    const metadata = await generateMetadata({
      params: Promise.resolve({ locale: 'en-US' }),
    });

    expect(metadata).toHaveProperty('description');
    expect(metadata.description).toBeTruthy();
  });
});
