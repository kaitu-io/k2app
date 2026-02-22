/**
 * /k2/[[...path]] — K2 Documentation Page
 *
 * Catches all paths under /k2/. The optional catch-all [[...path]] means:
 *   /k2/          → params.path = undefined  (renders k2/index)
 *   /k2/index     → params.path = ['index']
 *   /k2/architecture → params.path = ['architecture']
 *
 * Velite slugs for k2 content follow the pattern k2/{name} (e.g. k2/index).
 * Content is Velite-processed markdown (trusted build-time source, not user input).
 */
import { notFound } from 'next/navigation';
import { setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import { posts } from '#velite';
import { routing } from '@/i18n/routing';
import type { K2Post } from '@/lib/k2-posts';

/** Resolve a slug from the optional catch-all path param. */
function resolveSlug(path: string[] | undefined): string {
  if (!path || path.length === 0) {
    return 'k2';
  }
  return `k2/${path.join('/')}`;
}

/** Find a published k2 post by locale + slug, with zh-CN fallback. */
function findK2Post(locale: string, slug: string): K2Post | undefined {
  const exactMatch = (posts as K2Post[]).find(
    (p) => p.locale === locale && p.slug === slug && !p.draft
  );
  if (exactMatch) return exactMatch;

  // Fall back to zh-CN if not found in the requested locale
  if (locale !== 'zh-CN') {
    return (posts as K2Post[]).find(
      (p) => p.locale === 'zh-CN' && p.slug === slug && !p.draft
    );
  }

  return undefined;
}

interface PageParams {
  locale: string;
  path?: string[];
}

export async function generateMetadata({
  params,
}: {
  params: Promise<PageParams>;
}): Promise<Metadata> {
  const { locale, path } = await params;
  const slug = resolveSlug(path);
  const post = findK2Post(locale, slug);

  if (post) {
    return {
      title: post.title,
      description: post.summary,
      openGraph: {
        title: post.title,
        description: post.summary,
      },
    };
  }

  return {
    title: 'k2 | Kaitu',
  };
}

export function generateStaticParams(): { locale: string; path: string[] | undefined }[] {
  const params: { locale: string; path: string[] | undefined }[] = [];

  const k2Posts = (posts as K2Post[]).filter(
    (p) => (p.slug === 'k2' || p.slug.startsWith('k2/')) && !p.draft
  );

  for (const post of k2Posts) {
    // Skip the index post (slug "k2") — handled below as path: undefined
    if (post.slug === 'k2') continue;
    // Strip "k2/" prefix to get the path segments
    const pathSegments = post.slug.slice('k2/'.length).split('/');

    params.push({ locale: post.locale, path: pathSegments });

    // Also generate routes for all locales from the routing config
    for (const locale of routing.locales) {
      if (locale !== post.locale) {
        params.push({ locale, path: pathSegments });
      }
    }
  }

  // Add index route (path: undefined) for each locale
  for (const locale of routing.locales) {
    params.push({ locale, path: undefined });
  }

  return params;
}

export const dynamic = 'force-static';

export default async function K2Page({
  params,
}: {
  params: Promise<PageParams>;
}): Promise<React.ReactElement> {
  const { locale, path } = await params;

  setRequestLocale(locale as (typeof routing.locales)[number]);

  const slug = resolveSlug(path);
  const post = findK2Post(locale, slug);

  if (!post) {
    notFound();
  }

  // Content is Velite-processed Markdown (trusted build-time source)
  /* eslint-disable-next-line react/no-danger */
  return (
    <article className="prose max-w-none min-w-0 flex-1">
      <h1 className="text-3xl font-bold text-foreground mb-4">{post.title}</h1>
      {post.summary && (
        <p className="text-lg text-muted-foreground mb-8">{post.summary}</p>
      )}
      <div dangerouslySetInnerHTML={{ __html: post.content }} />
    </article>
  );
}
