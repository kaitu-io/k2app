/**
 * /{category} and /{category}/{post} — Velite content pages.
 *
 * Serves markdown from `content/{locale}/{category}/{slug}.md` via the
 * category registry in `src/lib/content-posts.ts`. One segment renders a
 * category listing, two segments render a post. Static routes (and the
 * dedicated /k2 section) win over this catch-all.
 *
 * Content is Velite-processed markdown — a trusted build-time source, not
 * user input, hence dangerouslySetInnerHTML (same as the /k2 pages).
 */
import { notFound } from 'next/navigation';
import { setRequestLocale } from 'next-intl/server';
import type { Metadata } from 'next';
import { posts } from '#velite';
import { Link } from '@/i18n/routing';
import { routing } from '@/i18n/routing';
import { getBrand } from '@/lib/brand-server';
import { siteBrand } from '@/lib/brands';
import { isPostVisibleToBrand } from '@/lib/k2-posts';
import {
  categorySlugs,
  findCategory,
  findContentPost,
  listCategoryPosts,
  type ContentCategory,
  type ContentPost,
} from '@/lib/content-posts';
import { generateMetadata as generatePageMetadata } from '../metadata';
import Header from '@/components/Header';
import Footer from '@/components/Footer';

type Props = {
  params: Promise<{ locale: string; slug: string[] }>;
};

type Locale = (typeof routing.locales)[number];

export const dynamic = 'force-static';

export function generateStaticParams(): { locale: string; slug: string[] }[] {
  const brand = siteBrand();
  const seen = new Set<string>();
  const params: { locale: string; slug: string[] }[] = [];

  const push = (locale: string, slug: string[]) => {
    const key = `${locale}::${slug.join('/')}`;
    if (seen.has(key)) return;
    seen.add(key);
    params.push({ locale, slug });
  };

  for (const category of categorySlugs()) {
    // Harvest slugs across ALL locales (a post may exist only in one), but
    // prerender only this brand's locales — same discipline as the /k2 pages:
    // an off-brand or off-locale prerender would hand the doc a real URL.
    const categoryPosts = (posts as ContentPost[]).filter(
      (p) =>
        p.slug.startsWith(`${category}/`) &&
        !p.draft &&
        isPostVisibleToBrand(p, brand.id)
    );
    if (categoryPosts.length === 0) continue;

    for (const locale of brand.allowedLocales) {
      push(locale, [category]);
      for (const post of categoryPosts) {
        push(locale, post.slug.split('/'));
      }
    }
  }

  return params;
}

export default async function CatchAll({ params }: Props) {
  const { locale, slug } = await params;
  setRequestLocale(locale as Locale);

  const brand = await getBrand();

  if (slug.length === 1) {
    const category = findCategory(locale, slug[0], brand);
    if (!category) notFound();
    const categoryPosts = listCategoryPosts(locale, category.slug, brand);
    return <CategoryListPage category={category} posts={categoryPosts} locale={locale} />;
  }

  if (slug.length === 2) {
    const [catSlug, postSlug] = slug;
    const category = findCategory(locale, catSlug, brand);
    if (!category) notFound();
    const post = findContentPost(locale, catSlug, postSlug, brand);
    if (!post) notFound();

    const article = {
      '@context': 'https://schema.org',
      '@type': 'Article',
      headline: post.title,
      description: post.summary || '',
      url: `${brand.baseUrl}/${locale}/${post.slug}`,
      datePublished: post.date,
      dateModified: post.date,
      inLanguage: locale,
      wordCount: post.metadata?.wordCount,
      articleSection: category.name,
      author: { '@type': 'Organization', name: brand.displayName, url: brand.baseUrl },
      publisher: { '@type': 'Organization', name: brand.displayName, url: brand.baseUrl },
      mainEntityOfPage: { '@type': 'WebPage', '@id': `${brand.baseUrl}/${locale}/${post.slug}` },
    };

    return (
      <>
        <script
          type="application/ld+json"
          dangerouslySetInnerHTML={{ __html: JSON.stringify(article) }}
        />
        <PostDetailPage post={post} locale={locale} />
      </>
    );
  }

  notFound();
}

function CategoryListPage({
  category,
  posts,
  locale,
}: {
  category: ContentCategory;
  posts: ContentPost[];
  locale: string;
}) {
  return (
    <>
      <Header />
      <main className="mx-auto max-w-3xl px-4 py-12">
        <h1 className="mb-8 text-3xl font-bold">{category.name}</h1>
        {posts.length === 0 ? (
          <p className="text-muted-foreground">
            {locale.startsWith('zh') ? '即将上线，敬请期待。' : 'Coming soon.'}
          </p>
        ) : (
          <ul className="space-y-6">
            {posts.map((post) => (
              <li key={post.slug}>
                <Link href={`/${post.slug}`} className="block hover:underline">
                  <h2 className="text-xl font-semibold">{post.title}</h2>
                  {post.summary && (
                    <p className="mt-2 text-muted-foreground">{post.summary}</p>
                  )}
                </Link>
              </li>
            ))}
          </ul>
        )}
      </main>
      <Footer />
    </>
  );
}

function PostDetailPage({ post, locale }: { post: ContentPost; locale: string }) {
  return (
    <>
      <Header />
      <main>
        <article className="prose dark:prose-invert mx-auto max-w-3xl px-4 py-12">
          <h1>{post.title}</h1>
          <time dateTime={post.date}>
            {new Date(post.date).toLocaleDateString(locale)}
          </time>
          <div dangerouslySetInnerHTML={{ __html: post.content }} />
        </article>
      </main>
      <Footer />
    </>
  );
}

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { locale, slug } = await params;
  const brand = await getBrand();
  // Path without the locale prefix — the shared helper re-adds `/{locale}`.
  const pathname = `/${slug.join('/')}`;

  if (slug.length === 1) {
    const category = findCategory(locale, slug[0], brand);
    if (!category) return {};
    // Route through the shared helper so category pages get their own
    // openGraph/twitter/hreflang instead of inheriting the homepage defaults
    // via Next.js shallow metadata merge.
    return generatePageMetadata(
      locale,
      pathname,
      {
        title: `${category.name} | ${brand.displayName}`,
        description: category.description,
      },
      brand
    );
  }

  if (slug.length === 2) {
    const [catSlug, postSlug] = slug;
    const category = findCategory(locale, catSlug, brand);
    if (!category) return {};
    const post = findContentPost(locale, catSlug, postSlug, brand);
    if (!post) return {};

    const canonicalUrl = `${brand.baseUrl}/${locale}/${category.slug}/${postSlug}`;

    const meta = generatePageMetadata(
      locale,
      pathname,
      {
        title: `${post.title} | ${brand.displayName}`,
        description: post.summary,
        ogType: 'article',
        ogImage: post.coverImage,
        article: {
          publishedTime: post.date,
          section: category.name,
          tags: post.tags,
        },
      },
      brand
    );

    // Own-brand canonical while keeping the helper's language alternates.
    return {
      ...meta,
      alternates: {
        ...meta.alternates,
        canonical: canonicalUrl,
      },
    };
  }

  return {};
}
