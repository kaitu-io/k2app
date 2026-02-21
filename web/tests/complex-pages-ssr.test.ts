/**
 * Complex Pages SSR Tests — T4
 *
 * Vitest tests for SSR conversion of the install and s/[code] pages.
 * RED phase: tests fail before implementation.
 *
 * Tests verify:
 * 1. install/page.tsx is an async Server Component with generateMetadata
 * 2. s/[code]/page.tsx is an async Server Component with generateMetadata
 * 3. Both pages accept params as Promise (Next.js 15 async params pattern)
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
  useRouter: () => ({
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
  }),
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
    windows: 'https://example.com/kaitu_windows.exe',
    macos: 'https://example.com/kaitu_macos.pkg',
    ios: 'https://apps.apple.com/app/id12345',
    android: 'https://example.com/kaitu.apk',
  },
  DESKTOP_VERSION: '0.3.22',
}));

// Mock @/lib/device-detection
vi.mock('@/lib/device-detection', () => ({
  detectDevice: vi.fn().mockReturnValue({
    type: 'windows',
    name: 'Windows PC',
    isMobile: false,
    isDesktop: true,
    userAgent: 'Mozilla/5.0 (Windows NT 10.0)',
  }),
  getPrimaryDownloadLink: vi.fn().mockReturnValue('https://example.com/kaitu_windows.exe'),
  hasAvailableDownload: vi.fn().mockReturnValue(true),
  triggerDownload: vi.fn().mockReturnValue(true),
  openDownloadInNewTab: vi.fn(),
}));

// Mock @/lib/api
vi.mock('@/lib/api', () => ({
  api: {
    getInviteCodeInfo: vi.fn().mockResolvedValue({
      code: 'TESTCODE',
      inviterName: 'Test User',
    }),
    getAppConfig: vi.fn().mockResolvedValue({
      inviteReward: {
        purchaseRewardDays: 7,
      },
    }),
  },
  ApiError: class ApiError extends Error {
    constructor(message: string) {
      super(message);
      this.name = 'ApiError';
    }
  },
}));

// Mock next/image
vi.mock('next/image', () => ({
  default: () => null,
}));

// Mock next/link
vi.mock('next/link', () => ({
  default: ({ children }: { children: React.ReactNode }) => children,
}));

// Mock lucide-react icons
vi.mock('lucide-react', () => ({
  Download: () => null,
  CheckCircle: () => null,
  AlertCircle: () => null,
  Smartphone: () => null,
  Monitor: () => null,
  ExternalLink: () => null,
  RefreshCw: () => null,
  ArrowRight: () => null,
  Gift: () => null,
  Loader2: () => null,
  QrCode: () => null,
  Sparkles: () => null,
}));

// Mock shadcn/ui components
vi.mock('@/components/ui/button', () => ({
  Button: ({ children }: { children: React.ReactNode }) => children,
}));

vi.mock('@/components/ui/card', () => ({
  Card: ({ children }: { children: React.ReactNode }) => children,
}));

// Mock InstallClient child component (to isolate server shell tests)
vi.mock('@/app/[locale]/install/InstallClient', () => ({
  default: () => null,
}));

// Mock InviteClient child component (to isolate server shell tests)
vi.mock('@/app/[locale]/s/[code]/InviteClient', () => ({
  default: () => null,
}));

// ============================================================================
// install/page.tsx tests
// ============================================================================

describe('test_install_ssr_renders_content', () => {
  it('page component is an async function (Server Component pattern)', async () => {
    const { default: InstallPage } = await import('../src/app/[locale]/install/page');

    expect(InstallPage).toBeTypeOf('function');
    const result = InstallPage({ params: Promise.resolve({ locale: 'zh-CN' }) });
    expect(result).toBeInstanceOf(Promise);
  });

  it('page accepts params as a Promise<{ locale: string }> (Next.js 15 pattern)', async () => {
    const { default: InstallPage } = await import('../src/app/[locale]/install/page');

    const element = await InstallPage({ params: Promise.resolve({ locale: 'zh-CN' }) });
    expect(element).not.toBeNull();
  });

  it('page renders JSX content (not null or empty)', async () => {
    const { default: InstallPage } = await import('../src/app/[locale]/install/page');

    const element = await InstallPage({ params: Promise.resolve({ locale: 'zh-CN' }) });

    expect(element).toBeDefined();
    expect(element).not.toBeNull();
  });
});

describe('test_install_generates_metadata', () => {
  it('generateMetadata is exported from the install page module', async () => {
    const pageModule = await import('../src/app/[locale]/install/page');

    expect(pageModule.generateMetadata).toBeDefined();
    expect(pageModule.generateMetadata).toBeTypeOf('function');
  });

  it('generateMetadata returns an object with title field', async () => {
    const { generateMetadata } = await import('../src/app/[locale]/install/page');

    const metadata = await generateMetadata({
      params: Promise.resolve({ locale: 'zh-CN' }),
    });

    expect(metadata).toHaveProperty('title');
    expect(metadata.title).toBeTruthy();
  });

  it('generateMetadata returns an object with description field', async () => {
    const { generateMetadata } = await import('../src/app/[locale]/install/page');

    const metadata = await generateMetadata({
      params: Promise.resolve({ locale: 'en-US' }),
    });

    expect(metadata).toHaveProperty('description');
    expect(metadata.description).toBeTruthy();
  });
});

// ============================================================================
// s/[code]/page.tsx tests
// ============================================================================

describe('test_invite_ssr_renders_content', () => {
  it('page component is an async function (Server Component pattern)', async () => {
    const { default: InvitePage } = await import('../src/app/[locale]/s/[code]/page');

    expect(InvitePage).toBeTypeOf('function');
    const result = InvitePage({ params: Promise.resolve({ locale: 'zh-CN', code: 'TESTCODE' }) });
    expect(result).toBeInstanceOf(Promise);
  });

  it('page accepts params as a Promise<{ locale: string; code: string }> (Next.js 15 pattern)', async () => {
    const { default: InvitePage } = await import('../src/app/[locale]/s/[code]/page');

    const element = await InvitePage({ params: Promise.resolve({ locale: 'zh-CN', code: 'TESTCODE' }) });
    expect(element).not.toBeNull();
  });

  it('page renders JSX content (not null or empty)', async () => {
    const { default: InvitePage } = await import('../src/app/[locale]/s/[code]/page');

    const element = await InvitePage({ params: Promise.resolve({ locale: 'zh-CN', code: 'INVITE123' }) });

    expect(element).toBeDefined();
    expect(element).not.toBeNull();
  });
});

describe('test_invite_generates_metadata', () => {
  it('generateMetadata is exported from the invite page module', async () => {
    const pageModule = await import('../src/app/[locale]/s/[code]/page');

    expect(pageModule.generateMetadata).toBeDefined();
    expect(pageModule.generateMetadata).toBeTypeOf('function');
  });

  it('generateMetadata returns an object with title field', async () => {
    const { generateMetadata } = await import('../src/app/[locale]/s/[code]/page');

    const metadata = await generateMetadata({
      params: Promise.resolve({ locale: 'zh-CN', code: 'TESTCODE' }),
    });

    expect(metadata).toHaveProperty('title');
    expect(metadata.title).toBeTruthy();
  });

  it('generateMetadata returns an object with description field', async () => {
    const { generateMetadata } = await import('../src/app/[locale]/s/[code]/page');

    const metadata = await generateMetadata({
      params: Promise.resolve({ locale: 'en-US', code: 'TESTCODE' }),
    });

    expect(metadata).toHaveProperty('description');
    expect(metadata.description).toBeTruthy();
  });
});
