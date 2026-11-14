import { posts } from '#velite';
import { siteBrand } from '@/lib/brands';
import { isPostVisibleToBrand } from '@/lib/k2-posts';
import { categorySlugs } from '@/lib/content-posts';

// llms.txt: a plain-text index of the site's published articles for AI search
// engines (https://llmstxt.org). Generated from the same Velite posts the
// sitemap uses, in the brand's default locale only, so it can never list a
// page the site does not serve and needs no upkeep when an article is added.
export const dynamic = 'force-static';

export function GET(): Response {
  const brand = siteBrand();
  const served = categorySlugs();
  const items = posts
    .filter(
      (p) =>
        !p.draft &&
        p.locale === brand.defaultLocale &&
        isPostVisibleToBrand(p, brand.id) &&
        served.includes(p.slug.split('/')[0]),
    )
    .sort((a, b) => +new Date(b.date) - +new Date(a.date));

  const lines = [
    `# ${brand.displayName}`,
    '',
    `> ${brand.taglineZh ?? brand.displayName}`,
    '',
    '## Articles',
    '',
    ...items.map((p) => {
      const note = p.summary ? `: ${p.summary.replace(/\s+/g, ' ').trim()}` : '';
      return `- [${p.title}](${brand.baseUrl}/${brand.defaultLocale}/${p.slug})${note}`;
    }),
    '',
  ];

  return new Response(lines.join('\n'), {
    headers: { 'content-type': 'text/plain; charset=utf-8' },
  });
}
