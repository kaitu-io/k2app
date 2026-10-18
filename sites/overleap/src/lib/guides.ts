import fs from 'fs';
import path from 'path';

/**
 * Guides: `content/guides/<slug>.md`, English only (served under the indexed
 * English locales). Front matter is a flat `key: value` block:
 *
 *   ---
 *   title: How to …
 *   description: One sentence for the meta description and the guide list.
 *   updated: 2026-10-15
 *   draft: true
 *   ---
 *
 * FAQ structured data is derived from the body, not duplicated in front matter:
 * every `### Question?` under a `## Frequently asked questions` heading becomes one
 * FAQPage entry whose answer is the text up to the next heading.
 *
 * A draft is served by `next dev` (for review) and is 404 + absent from the sitemap
 * in a production build.
 */
export interface Guide {
  slug: string;
  title: string;
  description: string;
  /** ISO date (YYYY-MM-DD) of the last substantive edit. */
  updated: string;
  draft: boolean;
  body: string;
  faq: { question: string; answer: string }[];
}

const DIR = path.join(process.cwd(), 'content/guides');
const SLUG = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;
export const FAQ_HEADING = 'Frequently asked questions';

export function parseGuide(slug: string, raw: string): Guide {
  const m = /^---\n([\s\S]*?)\n---\n?([\s\S]*)$/.exec(raw);
  if (!m) throw new Error(`guide ${slug}: missing front matter`);
  const fm: Record<string, string> = {};
  for (const line of m[1].split('\n')) {
    const kv = /^([a-z]+):\s*(.*)$/.exec(line.trim());
    if (kv) fm[kv[1]] = kv[2].replace(/^"(.*)"$/, '$1');
  }
  for (const key of ['title', 'description', 'updated']) {
    if (!fm[key]) throw new Error(`guide ${slug}: front matter needs ${key}`);
  }
  if (!/^\d{4}-\d{2}-\d{2}$/.test(fm.updated)) throw new Error(`guide ${slug}: updated must be YYYY-MM-DD`);
  const body = m[2].trim();
  return {
    slug,
    title: fm.title,
    description: fm.description,
    updated: fm.updated,
    draft: fm.draft === 'true',
    body,
    faq: extractFaq(body),
  };
}

export function extractFaq(body: string): Guide['faq'] {
  const lines = body.split('\n');
  const start = lines.findIndex((l) => l.trim() === `## ${FAQ_HEADING}`);
  if (start < 0) return [];
  const faq: Guide['faq'] = [];
  let current: { question: string; answer: string[] } | null = null;
  const flush = () => {
    if (current && current.answer.join(' ').trim()) {
      faq.push({ question: current.question, answer: current.answer.join(' ').replace(/\s+/g, ' ').trim() });
    }
  };
  for (const line of lines.slice(start + 1)) {
    if (/^## /.test(line)) break;
    const q = /^### (.+)$/.exec(line);
    if (q) {
      flush();
      current = { question: q[1].trim(), answer: [] };
    } else if (current) {
      current.answer.push(line.trim());
    }
  }
  flush();
  return faq;
}

function showDrafts(): boolean {
  return process.env.NODE_ENV !== 'production';
}

/** All guides visible in this build (drafts only outside production), newest first. */
export function listGuides(): Guide[] {
  if (!fs.existsSync(DIR)) return [];
  return fs
    .readdirSync(DIR)
    .filter((f) => f.endsWith('.md') && SLUG.test(f.slice(0, -3)))
    .map((f) => parseGuide(f.slice(0, -3), fs.readFileSync(path.join(DIR, f), 'utf8')))
    .filter((g) => !g.draft || showDrafts())
    .sort((a, b) => b.updated.localeCompare(a.updated));
}

export function findGuide(slug: string): Guide | undefined {
  return SLUG.test(slug) ? listGuides().find((g) => g.slug === slug) : undefined;
}
