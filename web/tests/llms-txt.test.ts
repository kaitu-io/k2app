import { describe, it, expect } from 'vitest';
import { GET } from '@/app/llms.txt/route';
import { siteBrand } from '@/lib/brands';

describe('llms.txt', () => {
  it('lists published guides as absolute URLs on the served brand host, and nothing off-host', async () => {
    const res = GET();
    expect(res.headers.get('content-type')).toContain('text/plain');
    const body = await res.text();
    const brand = siteBrand();
    const links = [...body.matchAll(/\]\((https?:\/\/[^)]+)\)/g)].map((m) => m[1]);
    expect(links.length).toBeGreaterThan(5);
    for (const l of links) expect(l.startsWith(`${brand.baseUrl}/${brand.defaultLocale}/`)).toBe(true);
    expect(body).not.toMatch(/\bKaitu\b/);
    expect(links.some((l) => l.includes('/guides/'))).toBe(true);
  });
});
