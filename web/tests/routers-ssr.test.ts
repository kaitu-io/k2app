/**
 * Routers Page SSR Tests — F1
 *
 * Vitest tests for SSR conversion of the routers page.
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
  ROUTER_PRODUCTS: {
    k2Mini: {
      name: 'K2 Mini',
      englishName: 'K2 Mini Router',
      tagline: 'Compact smart router',
      features: ['Feature 1', 'Feature 2'],
    },
    k2001: {
      name: 'K2-001',
      englishName: 'K2-001 Router',
      tagline: 'Enterprise router',
      features: ['Feature A', 'Feature B'],
    },
  },
}));

// Mock next/image
vi.mock('next/image', () => ({
  default: () => null,
}));

// Mock lucide-react icons
vi.mock('lucide-react', () => ({
  Router: () => null,
  Wifi: () => null,
  Home: () => null,
  Clock: () => null,
  HeartHandshake: () => null,
  DollarSign: () => null,
  Smartphone: () => null,
  CheckCircle: () => null,
  Star: () => null,
  Users: () => null,
  Mail: () => null,
}));

// Mock shadcn/ui components
vi.mock('@/components/ui/button', () => ({
  Button: ({ children }: { children: React.ReactNode }) => children,
}));

vi.mock('@/components/ui/card', () => ({
  Card: ({ children }: { children: React.ReactNode }) => children,
}));

describe('test_routers_ssr_renders_content', () => {
  it('page component is an async function (Server Component pattern)', async () => {
    const { default: RoutersPage } = await import('../src/app/[locale]/routers/page');

    // Must be an async function to qualify as a Server Component that awaits params
    expect(RoutersPage).toBeTypeOf('function');
    const result = RoutersPage({ params: Promise.resolve({ locale: 'zh-CN' }) });
    expect(result).toBeInstanceOf(Promise);
  });

  it('page accepts params as a Promise<{ locale: string }> (Next.js 15 pattern)', async () => {
    const { default: RoutersPage } = await import('../src/app/[locale]/routers/page');

    // Should resolve without throwing — async params are awaited inside
    const element = await RoutersPage({ params: Promise.resolve({ locale: 'zh-CN' }) });
    expect(element).not.toBeNull();
  });

  it('page renders JSX content (not null or empty)', async () => {
    const { default: RoutersPage } = await import('../src/app/[locale]/routers/page');

    const element = await RoutersPage({ params: Promise.resolve({ locale: 'zh-CN' }) });

    // Must return a React element (object with $$typeof or type), not null/undefined
    expect(element).toBeDefined();
    expect(element).not.toBeNull();
  });
});

describe('test_routers_generates_metadata', () => {
  it('generateMetadata is exported from the page module', async () => {
    const pageModule = await import('../src/app/[locale]/routers/page');

    expect(pageModule.generateMetadata).toBeDefined();
    expect(pageModule.generateMetadata).toBeTypeOf('function');
  });

  it('generateMetadata returns an object with title field', async () => {
    const { generateMetadata } = await import('../src/app/[locale]/routers/page');

    const metadata = await generateMetadata({
      params: Promise.resolve({ locale: 'zh-CN' }),
    });

    expect(metadata).toHaveProperty('title');
    expect(metadata.title).toBeTruthy();
  });

  it('generateMetadata returns an object with description field', async () => {
    const { generateMetadata } = await import('../src/app/[locale]/routers/page');

    const metadata = await generateMetadata({
      params: Promise.resolve({ locale: 'en-US' }),
    });

    expect(metadata).toHaveProperty('description');
    expect(metadata.description).toBeTruthy();
  });
});
