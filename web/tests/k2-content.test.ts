/**
 * K2 Content Files Tests — T4
 *
 * Vitest tests for k2 documentation markdown content files.
 * RED phase: tests fail because the content files don't exist yet.
 *
 * Tests verify:
 * 1. All 12 k2/ content files exist on disk and are non-empty
 * 2. Each file has required frontmatter (title, section, order)
 * 3. Overview page has correct title (contains "k2") and no MPTCP
 * 4. Quickstart page has required commands (k2s run, k2 up)
 * 5. No MPTCP references anywhere
 * 6. No PCC/Vivace disclosure anywhere
 *
 * Uses fs to read actual markdown files — tests fail until files are created.
 */
import { describe, it, expect } from 'vitest';
import fs from 'fs';
import path from 'path';

// Base content directory
const CONTENT_DIR = path.resolve(__dirname, '../content');

/** Slugs for each k2/ doc page */
const EXPECTED_SLUGS = ['index', 'quickstart', 'server', 'client', 'protocol', 'stealth'];

/** Locales that must have all k2/ content */
const EXPECTED_LOCALES = ['zh-CN', 'en-US'];

/**
 * Read a markdown file and return its raw text.
 * Returns empty string if file does not exist.
 */
function readMd(locale: string, slug: string): string {
  const filePath = path.join(CONTENT_DIR, locale, 'k2', `${slug}.md`);
  if (!fs.existsSync(filePath)) return '';
  return fs.readFileSync(filePath, 'utf-8');
}

/**
 * Parse frontmatter from a markdown file.
 * Returns an object with parsed YAML key-value pairs (simple parser for test use).
 */
function parseFrontmatter(content: string): Record<string, string | number | boolean> {
  const match = content.match(/^---\n([\s\S]*?)\n---/);
  if (!match) return {};
  const fm: Record<string, string | number | boolean> = {};
  for (const line of match[1].split('\n')) {
    const colonIdx = line.indexOf(':');
    if (colonIdx === -1) continue;
    const key = line.slice(0, colonIdx).trim();
    const raw = line.slice(colonIdx + 1).trim();
    if (raw === 'true') fm[key] = true;
    else if (raw === 'false') fm[key] = false;
    else if (/^\d+$/.test(raw)) fm[key] = parseInt(raw, 10);
    else fm[key] = raw.replace(/^["']|["']$/g, '');
  }
  return fm;
}

/** Extract body (content after frontmatter block) */
function parseBody(content: string): string {
  return content.replace(/^---\n[\s\S]*?\n---\n?/, '');
}

describe('test_k2_content_files_parse', () => {
  it('all 12 k2/ markdown files exist on disk', () => {
    const missing: string[] = [];
    for (const locale of EXPECTED_LOCALES) {
      for (const slug of EXPECTED_SLUGS) {
        const filePath = path.join(CONTENT_DIR, locale, 'k2', `${slug}.md`);
        if (!fs.existsSync(filePath)) {
          missing.push(`${locale}/k2/${slug}.md`);
        }
      }
    }
    expect(missing, `Missing files: ${missing.join(', ')}`).toHaveLength(0);
  });

  it('each k2/ markdown file has non-empty content', () => {
    for (const locale of EXPECTED_LOCALES) {
      for (const slug of EXPECTED_SLUGS) {
        const raw = readMd(locale, slug);
        expect(
          raw.trim().length,
          `${locale}/k2/${slug}.md is empty or does not exist`
        ).toBeGreaterThan(0);
      }
    }
  });

  it('each k2/ file has a non-empty body after frontmatter', () => {
    for (const locale of EXPECTED_LOCALES) {
      for (const slug of EXPECTED_SLUGS) {
        const raw = readMd(locale, slug);
        const body = parseBody(raw);
        expect(
          body.trim().length,
          `${locale}/k2/${slug}.md has no body content after frontmatter`
        ).toBeGreaterThan(0);
      }
    }
  });
});

describe('test_k2_content_has_required_frontmatter', () => {
  it('every k2/ file has a title in frontmatter', () => {
    for (const locale of EXPECTED_LOCALES) {
      for (const slug of EXPECTED_SLUGS) {
        const raw = readMd(locale, slug);
        const fm = parseFrontmatter(raw);
        expect(
          typeof fm['title'] === 'string' && String(fm['title']).trim().length > 0,
          `${locale}/k2/${slug}.md missing title in frontmatter`
        ).toBe(true);
      }
    }
  });

  it('every k2/ file has a section in frontmatter', () => {
    for (const locale of EXPECTED_LOCALES) {
      for (const slug of EXPECTED_SLUGS) {
        const raw = readMd(locale, slug);
        const fm = parseFrontmatter(raw);
        expect(
          typeof fm['section'] === 'string' && String(fm['section']).trim().length > 0,
          `${locale}/k2/${slug}.md missing section in frontmatter`
        ).toBe(true);
      }
    }
  });

  it('every k2/ file has a numeric order in frontmatter', () => {
    for (const locale of EXPECTED_LOCALES) {
      for (const slug of EXPECTED_SLUGS) {
        const raw = readMd(locale, slug);
        const fm = parseFrontmatter(raw);
        expect(
          typeof fm['order'] === 'number',
          `${locale}/k2/${slug}.md missing numeric order in frontmatter`
        ).toBe(true);
      }
    }
  });

  it('section values are one of the allowed sections', () => {
    const allowedSections = new Set(['getting-started', 'technical']);
    for (const locale of EXPECTED_LOCALES) {
      for (const slug of EXPECTED_SLUGS) {
        const raw = readMd(locale, slug);
        const fm = parseFrontmatter(raw);
        expect(
          allowedSections.has(String(fm['section'] ?? '')),
          `${locale}/k2/${slug}.md has invalid section "${fm['section']}"`
        ).toBe(true);
      }
    }
  });

  it('order values match spec: index=1, quickstart=2, server=3, client=4, protocol=5, stealth=6', () => {
    const expectedOrders: Record<string, number> = {
      index: 1,
      quickstart: 2,
      server: 3,
      client: 4,
      protocol: 5,
      stealth: 6,
    };
    for (const locale of EXPECTED_LOCALES) {
      for (const slug of EXPECTED_SLUGS) {
        const raw = readMd(locale, slug);
        const fm = parseFrontmatter(raw);
        expect(
          fm['order'],
          `${locale}/k2/${slug}.md has wrong order (expected ${expectedOrders[slug]})`
        ).toBe(expectedOrders[slug]);
      }
    }
  });
});

describe('test_k2_overview_page_renders', () => {
  it('zh-CN k2/index.md exists and title contains "k2"', () => {
    const raw = readMd('zh-CN', 'index');
    expect(raw.trim().length, 'zh-CN/k2/index.md does not exist').toBeGreaterThan(0);
    const fm = parseFrontmatter(raw);
    expect(
      String(fm['title'] ?? '').toLowerCase().includes('k2'),
      `zh-CN/k2/index.md title "${fm['title']}" does not contain "k2"`
    ).toBe(true);
  });

  it('en-US k2/index.md exists and title contains "k2"', () => {
    const raw = readMd('en-US', 'index');
    expect(raw.trim().length, 'en-US/k2/index.md does not exist').toBeGreaterThan(0);
    const fm = parseFrontmatter(raw);
    expect(
      String(fm['title'] ?? '').toLowerCase().includes('k2'),
      `en-US/k2/index.md title "${fm['title']}" does not contain "k2"`
    ).toBe(true);
  });
});

describe('test_k2_quickstart_has_commands', () => {
  it('zh-CN quickstart body contains "k2s run"', () => {
    const raw = readMd('zh-CN', 'quickstart');
    const body = parseBody(raw);
    expect(body, 'zh-CN/k2/quickstart.md body does not contain "k2s run"').toContain('k2s run');
  });

  it('zh-CN quickstart body contains "k2 up"', () => {
    const raw = readMd('zh-CN', 'quickstart');
    const body = parseBody(raw);
    expect(body, 'zh-CN/k2/quickstart.md body does not contain "k2 up"').toContain('k2 up');
  });

  it('en-US quickstart body contains "k2s run"', () => {
    const raw = readMd('en-US', 'quickstart');
    const body = parseBody(raw);
    expect(body, 'en-US/k2/quickstart.md body does not contain "k2s run"').toContain('k2s run');
  });

  it('en-US quickstart body contains "k2 up"', () => {
    const raw = readMd('en-US', 'quickstart');
    const body = parseBody(raw);
    expect(body, 'en-US/k2/quickstart.md body does not contain "k2 up"').toContain('k2 up');
  });
});

describe('test_k2_no_mptcp_references', () => {
  it('no k2/ file body mentions MPTCP', () => {
    for (const locale of EXPECTED_LOCALES) {
      for (const slug of EXPECTED_SLUGS) {
        const raw = readMd(locale, slug);
        const body = parseBody(raw);
        expect(
          body.toUpperCase().includes('MPTCP'),
          `MPTCP found in body of ${locale}/k2/${slug}.md`
        ).toBe(false);
      }
    }
  });

  it('no k2/ file frontmatter mentions MPTCP', () => {
    for (const locale of EXPECTED_LOCALES) {
      for (const slug of EXPECTED_SLUGS) {
        const raw = readMd(locale, slug);
        const fm = parseFrontmatter(raw);
        const fmText = Object.values(fm).join(' ').toUpperCase();
        expect(
          fmText.includes('MPTCP'),
          `MPTCP found in frontmatter of ${locale}/k2/${slug}.md`
        ).toBe(false);
      }
    }
  });
});

describe('test_k2_no_pcc_disclosure', () => {
  it('no k2/ file body mentions PCC as a standalone algorithm name', () => {
    for (const locale of EXPECTED_LOCALES) {
      for (const slug of EXPECTED_SLUGS) {
        const raw = readMd(locale, slug);
        const body = parseBody(raw);
        expect(
          /\bPCC\b/.test(body),
          `PCC algorithm name found in body of ${locale}/k2/${slug}.md`
        ).toBe(false);
      }
    }
  });

  it('no k2/ file body mentions Vivace', () => {
    for (const locale of EXPECTED_LOCALES) {
      for (const slug of EXPECTED_SLUGS) {
        const raw = readMd(locale, slug);
        const body = parseBody(raw);
        expect(
          body.includes('Vivace'),
          `Vivace found in body of ${locale}/k2/${slug}.md`
        ).toBe(false);
      }
    }
  });

  it('no k2/ file summary or title mentions PCC or Vivace', () => {
    for (const locale of EXPECTED_LOCALES) {
      for (const slug of EXPECTED_SLUGS) {
        const raw = readMd(locale, slug);
        const fm = parseFrontmatter(raw);
        const summaryAndTitle = `${fm['summary'] ?? ''} ${fm['title'] ?? ''}`;
        expect(
          /\bPCC\b/.test(summaryAndTitle) || summaryAndTitle.includes('Vivace'),
          `PCC or Vivace found in frontmatter of ${locale}/k2/${slug}.md`
        ).toBe(false);
      }
    }
  });
});
