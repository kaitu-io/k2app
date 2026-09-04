/**
 * content-posts.ts — category registry + Velite lookup for the [...slug] catch-all.
 *
 * Mirrors the mock pattern of tests/content-pages.test.ts: #velite is mocked
 * with synthetic posts, brand comes from the registry in src/lib/brands.
 */
import { describe, it, expect, vi } from 'vitest';

vi.mock('#velite', () => ({
  posts: [
    {
      title: '快速入门',
      date: '2026-02-20',
      summary: '入门指南',
      draft: false,
      content: '<p>zh-CN body</p>',
      metadata: { readingTime: 1, wordCount: 10 },
      filePath: 'zh-CN/guides/getting-started',
      locale: 'zh-CN',
      slug: 'guides/getting-started',
      brand: 'kaitu',
    },
    {
      title: '快速入門',
      date: '2026-02-20',
      summary: '入門指南',
      draft: false,
      content: '<p>zh-TW body</p>',
      metadata: { readingTime: 1, wordCount: 10 },
      filePath: 'zh-TW/guides/getting-started',
      locale: 'zh-TW',
      slug: 'guides/getting-started',
      brand: 'kaitu',
    },
    {
      title: 'zh-CN only',
      date: '2026-03-01',
      summary: 'no zh-TW copy',
      draft: false,
      content: '<p>only zh-CN</p>',
      metadata: { readingTime: 1, wordCount: 10 },
      filePath: 'zh-CN/guides/zh-only',
      locale: 'zh-CN',
      slug: 'guides/zh-only',
      brand: 'kaitu',
    },
    {
      title: 'draft post',
      date: '2026-03-02',
      draft: true,
      content: '<p>draft</p>',
      metadata: { readingTime: 1, wordCount: 10 },
      filePath: 'zh-CN/guides/draft-post',
      locale: 'zh-CN',
      slug: 'guides/draft-post',
      brand: 'kaitu',
    },
  ],
}));

import { KAITU, OVERLEAP } from '../brands';
import {
  categorySlugs,
  findCategory,
  findContentPost,
  listCategoryPosts,
} from '../content-posts';

describe('findCategory', () => {
  it('resolves a registered category with a localized name', () => {
    const cat = findCategory('zh-CN', 'guides', KAITU);
    expect(cat).not.toBeNull();
    expect(cat?.name).toBe('使用指南');
  });

  it('returns null for an unregistered category (a markdown dir alone is not a page)', () => {
    expect(findCategory('zh-CN', 'blog', KAITU)).toBeNull();
    expect(findCategory('zh-CN', 'nonexistent', KAITU)).toBeNull();
  });

  it('guides is registered', () => {
    expect(categorySlugs()).toContain('guides');
  });
});

describe('findContentPost', () => {
  it('returns the exact locale match', () => {
    const post = findContentPost('zh-TW', 'guides', 'getting-started', KAITU);
    expect(post?.locale).toBe('zh-TW');
    expect(post?.content).toBe('<p>zh-TW body</p>');
  });

  it('falls back to the brand default locale when the requested locale is missing', () => {
    const post = findContentPost('zh-TW', 'guides', 'zh-only', KAITU);
    expect(post?.locale).toBe('zh-CN');
  });

  it('never serves a draft', () => {
    expect(findContentPost('zh-CN', 'guides', 'draft-post', KAITU)).toBeUndefined();
  });

  it('404s kaitu-only posts on the overleap brand', () => {
    expect(findContentPost('en-US', 'guides', 'getting-started', OVERLEAP)).toBeUndefined();
  });

  it('returns undefined for a missing slug', () => {
    expect(findContentPost('zh-CN', 'guides', 'nope', KAITU)).toBeUndefined();
  });
});

describe('listCategoryPosts', () => {
  it('lists exact-locale posts plus default-locale fallbacks, newest first, no drafts', () => {
    const list = listCategoryPosts('zh-TW', 'guides', KAITU);
    expect(list.map((p) => p.slug)).toEqual(['guides/zh-only', 'guides/getting-started']);
    // getting-started has a real zh-TW copy; zh-only falls back to zh-CN.
    expect(list.find((p) => p.slug === 'guides/getting-started')?.locale).toBe('zh-TW');
    expect(list.find((p) => p.slug === 'guides/zh-only')?.locale).toBe('zh-CN');
  });

  it('is empty on the overleap brand (all fixture posts are kaitu-only)', () => {
    expect(listCategoryPosts('en-US', 'guides', OVERLEAP)).toEqual([]);
  });
});
