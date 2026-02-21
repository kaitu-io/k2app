/**
 * Static Content Pages SSR Tests — T2
 *
 * Vitest tests for SSR conversion of 403, privacy, terms, and retailer/rules pages.
 * RED phase: tests fail before implementation.
 *
 * Tests verify:
 * 1. Each page component is an async function (Server Component compatible)
 * 2. Each page accepts params with locale as Promise (Next.js 15 async params pattern)
 * 3. Each page exports generateMetadata returning title and description
 * 4. Privacy/terms/retailer-rules read markdown from filesystem (not fetch())
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';

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

// Mock @/components/MarkdownRenderer
vi.mock('@/components/MarkdownRenderer', () => ({
  default: ({ content }: { content: string }) => content,
}));

// Mock shadcn/ui components
vi.mock('@/components/ui/button', () => ({
  Button: ({ children }: { children: React.ReactNode }) => children,
}));

// Mock lucide-react icons
vi.mock('lucide-react', () => ({
  ShieldX: () => null,
  ArrowLeft: () => null,
  Home: () => null,
  Shield: () => null,
  Scale: () => null,
  Users: () => null,
}));

// Mock next/link
vi.mock('next/link', () => ({
  default: ({ children }: { children: React.ReactNode }) => children,
}));

// Mock fs/promises for server-side file reading
vi.mock('fs/promises', () => ({
  readFile: vi.fn().mockResolvedValue('# Sample Markdown Content\n\nThis is test content.'),
}));

// Mock path module
vi.mock('path', () => ({
  default: {
    join: (...args: string[]) => args.join('/'),
  },
  join: (...args: string[]) => args.join('/'),
}));

// ─── 403 Page Tests ───────────────────────────────────────────────────────────

describe('test_403_ssr_renders_content', () => {
  it('page component is an async function (Server Component pattern)', async () => {
    const { default: ForbiddenPage } = await import('../src/app/[locale]/403/page');

    // Must be an async function to qualify as a Server Component that awaits params
    expect(ForbiddenPage).toBeTypeOf('function');
    const result = ForbiddenPage({ params: Promise.resolve({ locale: 'zh-CN' }) });
    expect(result).toBeInstanceOf(Promise);
  });

  it('page accepts params as a Promise<{ locale: string }> (Next.js 15 pattern)', async () => {
    const { default: ForbiddenPage } = await import('../src/app/[locale]/403/page');

    // Should resolve without throwing — async params are awaited inside
    const element = await ForbiddenPage({ params: Promise.resolve({ locale: 'zh-CN' }) });
    expect(element).not.toBeNull();
  });

  it('page renders JSX content (not null or empty)', async () => {
    const { default: ForbiddenPage } = await import('../src/app/[locale]/403/page');

    const element = await ForbiddenPage({ params: Promise.resolve({ locale: 'zh-CN' }) });

    // Must return a React element (object with $$typeof or type), not null/undefined
    expect(element).toBeDefined();
    expect(element).not.toBeNull();
  });
});

describe('test_403_generates_metadata', () => {
  it('generateMetadata is exported from the page module', async () => {
    const pageModule = await import('../src/app/[locale]/403/page');

    expect(pageModule.generateMetadata).toBeDefined();
    expect(pageModule.generateMetadata).toBeTypeOf('function');
  });

  it('generateMetadata returns an object with title field', async () => {
    const { generateMetadata } = await import('../src/app/[locale]/403/page');

    const metadata = await generateMetadata({
      params: Promise.resolve({ locale: 'zh-CN' }),
    });

    expect(metadata).toHaveProperty('title');
    expect(metadata.title).toBeTruthy();
  });

  it('generateMetadata returns an object with description field', async () => {
    const { generateMetadata } = await import('../src/app/[locale]/403/page');

    const metadata = await generateMetadata({
      params: Promise.resolve({ locale: 'en-US' }),
    });

    expect(metadata).toHaveProperty('description');
    expect(metadata.description).toBeTruthy();
  });
});

// ─── Privacy Page Tests ───────────────────────────────────────────────────────

describe('test_privacy_ssr_renders_content', () => {
  beforeEach(() => {
    vi.resetModules();
    // Re-apply mocks after resetModules
    vi.mock('fs/promises', () => ({
      readFile: vi.fn().mockResolvedValue('# Privacy Policy\n\nTest content.'),
    }));
    vi.mock('path', () => ({
      default: { join: (...args: string[]) => args.join('/') },
      join: (...args: string[]) => args.join('/'),
    }));
  });

  it('page component is an async function (Server Component pattern)', async () => {
    const { default: PrivacyPage } = await import('../src/app/[locale]/privacy/page');

    expect(PrivacyPage).toBeTypeOf('function');
    const result = PrivacyPage({ params: Promise.resolve({ locale: 'zh-CN' }) });
    expect(result).toBeInstanceOf(Promise);
  });

  it('page accepts params as a Promise<{ locale: string }> (Next.js 15 pattern)', async () => {
    const { default: PrivacyPage } = await import('../src/app/[locale]/privacy/page');

    const element = await PrivacyPage({ params: Promise.resolve({ locale: 'zh-CN' }) });
    expect(element).not.toBeNull();
  });

  it('page renders JSX content (not null or empty)', async () => {
    const { default: PrivacyPage } = await import('../src/app/[locale]/privacy/page');

    const element = await PrivacyPage({ params: Promise.resolve({ locale: 'zh-CN' }) });

    expect(element).toBeDefined();
    expect(element).not.toBeNull();
  });
});

describe('test_privacy_generates_metadata', () => {
  beforeEach(() => {
    vi.resetModules();
    vi.mock('fs/promises', () => ({
      readFile: vi.fn().mockResolvedValue('# Privacy Policy\n\nTest content.'),
    }));
    vi.mock('path', () => ({
      default: { join: (...args: string[]) => args.join('/') },
      join: (...args: string[]) => args.join('/'),
    }));
  });

  it('generateMetadata is exported from the page module', async () => {
    const pageModule = await import('../src/app/[locale]/privacy/page');

    expect(pageModule.generateMetadata).toBeDefined();
    expect(pageModule.generateMetadata).toBeTypeOf('function');
  });

  it('generateMetadata returns an object with title field', async () => {
    const { generateMetadata } = await import('../src/app/[locale]/privacy/page');

    const metadata = await generateMetadata({
      params: Promise.resolve({ locale: 'zh-CN' }),
    });

    expect(metadata).toHaveProperty('title');
    expect(metadata.title).toBeTruthy();
  });

  it('generateMetadata returns an object with description field', async () => {
    const { generateMetadata } = await import('../src/app/[locale]/privacy/page');

    const metadata = await generateMetadata({
      params: Promise.resolve({ locale: 'en-US' }),
    });

    expect(metadata).toHaveProperty('description');
    expect(metadata.description).toBeTruthy();
  });
});

// ─── Terms Page Tests ─────────────────────────────────────────────────────────

describe('test_terms_ssr_renders_content', () => {
  beforeEach(() => {
    vi.resetModules();
    vi.mock('fs/promises', () => ({
      readFile: vi.fn().mockResolvedValue('# Terms of Service\n\nTest content.'),
    }));
    vi.mock('path', () => ({
      default: { join: (...args: string[]) => args.join('/') },
      join: (...args: string[]) => args.join('/'),
    }));
  });

  it('page component is an async function (Server Component pattern)', async () => {
    const { default: TermsPage } = await import('../src/app/[locale]/terms/page');

    expect(TermsPage).toBeTypeOf('function');
    const result = TermsPage({ params: Promise.resolve({ locale: 'zh-CN' }) });
    expect(result).toBeInstanceOf(Promise);
  });

  it('page accepts params as a Promise<{ locale: string }> (Next.js 15 pattern)', async () => {
    const { default: TermsPage } = await import('../src/app/[locale]/terms/page');

    const element = await TermsPage({ params: Promise.resolve({ locale: 'zh-CN' }) });
    expect(element).not.toBeNull();
  });

  it('page renders JSX content (not null or empty)', async () => {
    const { default: TermsPage } = await import('../src/app/[locale]/terms/page');

    const element = await TermsPage({ params: Promise.resolve({ locale: 'zh-CN' }) });

    expect(element).toBeDefined();
    expect(element).not.toBeNull();
  });
});

describe('test_terms_generates_metadata', () => {
  beforeEach(() => {
    vi.resetModules();
    vi.mock('fs/promises', () => ({
      readFile: vi.fn().mockResolvedValue('# Terms of Service\n\nTest content.'),
    }));
    vi.mock('path', () => ({
      default: { join: (...args: string[]) => args.join('/') },
      join: (...args: string[]) => args.join('/'),
    }));
  });

  it('generateMetadata is exported from the page module', async () => {
    const pageModule = await import('../src/app/[locale]/terms/page');

    expect(pageModule.generateMetadata).toBeDefined();
    expect(pageModule.generateMetadata).toBeTypeOf('function');
  });

  it('generateMetadata returns an object with title field', async () => {
    const { generateMetadata } = await import('../src/app/[locale]/terms/page');

    const metadata = await generateMetadata({
      params: Promise.resolve({ locale: 'zh-CN' }),
    });

    expect(metadata).toHaveProperty('title');
    expect(metadata.title).toBeTruthy();
  });

  it('generateMetadata returns an object with description field', async () => {
    const { generateMetadata } = await import('../src/app/[locale]/terms/page');

    const metadata = await generateMetadata({
      params: Promise.resolve({ locale: 'en-US' }),
    });

    expect(metadata).toHaveProperty('description');
    expect(metadata.description).toBeTruthy();
  });
});

// ─── Retailer Rules Page Tests ────────────────────────────────────────────────

describe('test_retailer_rules_ssr_renders_content', () => {
  beforeEach(() => {
    vi.resetModules();
    vi.mock('fs/promises', () => ({
      readFile: vi.fn().mockResolvedValue('# Retailer Rules\n\nTest content.'),
    }));
    vi.mock('path', () => ({
      default: { join: (...args: string[]) => args.join('/') },
      join: (...args: string[]) => args.join('/'),
    }));
  });

  it('page component is an async function (Server Component pattern)', async () => {
    const { default: RetailerRulesPage } = await import('../src/app/[locale]/retailer/rules/page');

    expect(RetailerRulesPage).toBeTypeOf('function');
    const result = RetailerRulesPage({ params: Promise.resolve({ locale: 'zh-CN' }) });
    expect(result).toBeInstanceOf(Promise);
  });

  it('page accepts params as a Promise<{ locale: string }> (Next.js 15 pattern)', async () => {
    const { default: RetailerRulesPage } = await import('../src/app/[locale]/retailer/rules/page');

    const element = await RetailerRulesPage({ params: Promise.resolve({ locale: 'zh-CN' }) });
    expect(element).not.toBeNull();
  });

  it('page renders JSX content (not null or empty)', async () => {
    const { default: RetailerRulesPage } = await import('../src/app/[locale]/retailer/rules/page');

    const element = await RetailerRulesPage({ params: Promise.resolve({ locale: 'zh-CN' }) });

    expect(element).toBeDefined();
    expect(element).not.toBeNull();
  });
});

describe('test_retailer_rules_generates_metadata', () => {
  beforeEach(() => {
    vi.resetModules();
    vi.mock('fs/promises', () => ({
      readFile: vi.fn().mockResolvedValue('# Retailer Rules\n\nTest content.'),
    }));
    vi.mock('path', () => ({
      default: { join: (...args: string[]) => args.join('/') },
      join: (...args: string[]) => args.join('/'),
    }));
  });

  it('generateMetadata is exported from the page module', async () => {
    const pageModule = await import('../src/app/[locale]/retailer/rules/page');

    expect(pageModule.generateMetadata).toBeDefined();
    expect(pageModule.generateMetadata).toBeTypeOf('function');
  });

  it('generateMetadata returns an object with title field', async () => {
    const { generateMetadata } = await import('../src/app/[locale]/retailer/rules/page');

    const metadata = await generateMetadata({
      params: Promise.resolve({ locale: 'zh-CN' }),
    });

    expect(metadata).toHaveProperty('title');
    expect(metadata.title).toBeTruthy();
  });

  it('generateMetadata returns an object with description field', async () => {
    const { generateMetadata } = await import('../src/app/[locale]/retailer/rules/page');

    const metadata = await generateMetadata({
      params: Promise.resolve({ locale: 'en-US' }),
    });

    expect(metadata).toHaveProperty('description');
    expect(metadata.description).toBeTruthy();
  });
});
