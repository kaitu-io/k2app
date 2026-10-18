/**
 * Indexing policy: only the human-reviewed English locales are indexable.
 * Machine-translated locales stay served but are `noindex, follow`, absent from
 * the sitemap and from every hreflang set. Guides are English-only and their FAQ
 * section becomes FAQPage structured data.
 */
import { describe, it, expect, vi } from 'vitest';
import { pageMetadata } from '@/lib/metadata';
import { INDEXED_LOCALES, LOCALES, STATIC_ROUTES } from '@/lib/site';
import sitemap from '@/app/sitemap';
import { extractFaq, listGuides, parseGuide } from '@/lib/guides';

const meta = (locale: string, index?: boolean) =>
  pageMetadata(locale, '/pricing', { title: 't', description: 'd', ...(index === false ? { index } : {}) });

describe('locale indexing', () => {
  it('indexed locales carry no robots directive', () => {
    for (const l of INDEXED_LOCALES) expect(meta(l).robots).toBeUndefined();
  });

  it('every other served locale is noindex, follow', () => {
    const others = LOCALES.filter((l) => !(INDEXED_LOCALES as readonly string[]).includes(l));
    expect(others.length).toBeGreaterThan(10);
    for (const l of others) expect(meta(l).robots).toEqual({ index: false, follow: true });
  });

  it('an explicitly unindexed page stays noindex, nofollow in any locale', () => {
    expect(meta('en-GB', false).robots).toEqual({ index: false, follow: false });
    expect(meta('fr', false).robots).toEqual({ index: false, follow: false });
  });

  it('hreflang lists only the indexed locales plus x-default → en-GB, on every page', () => {
    for (const l of ['en-GB', 'fr', 'ja']) {
      const langs = meta(l).alternates?.languages as Record<string, string>;
      expect(Object.keys(langs).sort()).toEqual(['en-au', 'en-gb', 'en-us', 'x-default']);
      expect(langs['x-default']).toBe('https://overleap.io/en-GB/pricing');
    }
  });

  it('canonical stays self-referencing (a noindexed translation is not a duplicate of en-GB)', () => {
    expect(meta('fr').alternates?.canonical).toBe('https://overleap.io/fr/pricing');
  });
});

describe('sitemap', () => {
  const entries = sitemap();
  const urls = entries.map((e) => e.url);

  it('lists every static route in exactly the indexed locales', () => {
    for (const path of STATIC_ROUTES) {
      for (const l of INDEXED_LOCALES) expect(urls).toContain(`https://overleap.io/${l}${path}`);
    }
    for (const url of urls) {
      const locale = url.replace('https://overleap.io/', '').split('/')[0];
      expect(INDEXED_LOCALES as readonly string[]).toContain(locale);
    }
  });

  it('alternates never point at a noindexed locale', () => {
    for (const e of entries) {
      for (const href of Object.values(e.alternates?.languages ?? {})) {
        const locale = String(href).replace('https://overleap.io/', '').split('/')[0];
        expect(INDEXED_LOCALES as readonly string[]).toContain(locale);
      }
    }
  });
});

describe('guides', () => {
  const raw = `---
title: How to test
description: A test guide.
updated: 2026-10-15
draft: true
---

Intro.

## Section

Text.

## Frequently asked questions

### First question?

First answer,
on two lines.

### Empty question?

### Second question?

Second answer.

## After

Not part of the FAQ.
`;

  it('parses front matter, body and the FAQ section', () => {
    const g = parseGuide('how-to-test', raw);
    expect(g).toMatchObject({ slug: 'how-to-test', title: 'How to test', updated: '2026-10-15', draft: true });
    expect(g.body.startsWith('Intro.')).toBe(true);
    expect(g.faq).toEqual([
      { question: 'First question?', answer: 'First answer, on two lines.' },
      { question: 'Second question?', answer: 'Second answer.' },
    ]);
  });

  it('a guide without the FAQ heading has no FAQ entries', () => {
    expect(extractFaq('## Section\n\n### Not a question\n\nText.')).toEqual([]);
  });

  it('rejects a guide without the required front matter', () => {
    expect(() => parseGuide('x', '---\ntitle: T\n---\nbody')).toThrow(/description/);
    expect(() => parseGuide('x', '---\ntitle: T\ndescription: D\nupdated: 15/10/2026\n---\nbody')).toThrow(/YYYY-MM-DD/);
  });

  it('every guide in content/ parses (drafts are visible outside production)', () => {
    const guides = listGuides();
    expect(guides.map((g) => g.slug)).toContain('guide-template');
    for (const g of guides) expect(g.faq.length).toBeGreaterThan(0);
  });

  it('a production build neither serves nor lists drafts', () => {
    vi.stubEnv('NODE_ENV', 'production');
    try {
      expect(listGuides().some((g) => g.draft)).toBe(false);
      expect(sitemap().some((e) => e.url.includes('/guides/guide-template'))).toBe(false);
    } finally {
      vi.unstubAllEnvs();
    }
  });
});
