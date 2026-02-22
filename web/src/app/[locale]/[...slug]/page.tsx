import { notFound } from 'next/navigation';
import { setRequestLocale } from 'next-intl/server';
import { Metadata } from 'next';
import { format } from 'date-fns';
import NextLink from 'next/link';
import Header from '@/components/Header';
import Footer from '@/components/Footer';
import { posts } from '#velite';
import { routing } from '@/i18n/routing';

interface Post {
  title: string;
  date: string;
  summary?: string;
  tags?: string[];
  coverImage?: string;
  draft: boolean;
  content: string;
  metadata: { readingTime: number; wordCount: number };
  filePath: string;
  locale: string;
  slug: string;
}

/**
 * Find a single published post matching locale + slug.
 * Returns undefined if not found or if draft.
 */
function findPost(locale: string, slug: string): Post | undefined {
  return (posts as Post[]).find(
    (post) => post.locale === locale && post.slug === slug && !post.draft
  );
}

/**
 * Find all published posts whose slug starts with the given directory prefix.
 * Returns them sorted by date descending.
 */
function findPostsInDirectory(locale: string, prefix: string): Post[] {
  return (posts as Post[])
    .filter(
      (post) =>
        post.locale === locale &&
        post.slug.startsWith(prefix + '/') &&
        !post.draft
    )
    .sort((a, b) => new Date(b.date).getTime() - new Date(a.date).getTime());
}

interface PageParams {
  locale: string;
  slug: string[];
}

export function generateStaticParams(): { locale: string; slug: string[] }[] {
  const params: { locale: string; slug: string[] }[] = [];
  const directories = new Set<string>();

  const publishedPosts = (posts as Post[]).filter((p) => !p.draft);

  for (const post of publishedPosts) {
    params.push({ locale: post.locale, slug: post.slug.split('/') });

    // Collect directory paths
    const parts = post.slug.split('/');
    for (let i = 1; i < parts.length; i++) {
      directories.add(`${post.locale}/${parts.slice(0, i).join('/')}`);
    }
  }

  for (const dir of directories) {
    const [locale, ...rest] = dir.split('/');
    params.push({ locale, slug: rest });
  }

  return params;
}

export async function generateMetadata({
  params,
}: {
  params: Promise<PageParams>;
}): Promise<Metadata> {
  const { locale, slug } = await params;
  const slugPath = slug.join('/');

  // Check for exact post match
  const post = findPost(locale, slugPath);
  if (post) {
    return {
      title: post.title,
      description: post.summary,
      openGraph: {
        title: post.title,
        description: post.summary,
        ...(post.coverImage ? { images: [post.coverImage] } : {}),
      },
    };
  }

  // Check for zh-CN fallback
  const fallbackPost =
    locale !== 'zh-CN' ? findPost('zh-CN', slugPath) : undefined;
  if (fallbackPost) {
    return {
      title: fallbackPost.title,
      description: fallbackPost.summary,
      openGraph: {
        title: fallbackPost.title,
        description: fallbackPost.summary,
        ...(fallbackPost.coverImage ? { images: [fallbackPost.coverImage] } : {}),
      },
    };
  }

  // Directory listing
  const dirName = slug[slug.length - 1] ?? '';
  const capitalizedDirName =
    dirName.charAt(0).toUpperCase() + dirName.slice(1);
  return {
    title: `${capitalizedDirName} | Kaitu`,
  };
}

export default async function ContentPage({
  params,
}: {
  params: Promise<PageParams>;
}) {
  const { locale, slug } = await params;

  // Enable static rendering for this locale
  setRequestLocale(locale as (typeof routing.locales)[number]);

  const slugPath = slug.join('/');

  // 1. Try exact post match in requested locale
  const post = findPost(locale, slugPath);
  if (post) {
    return <ArticleDetail post={post} locale={locale} />;
  }

  // 2. Try directory listing for requested locale
  const directoryPosts = findPostsInDirectory(locale, slugPath);
  if (directoryPosts.length > 0) {
    const dirName = slug[slug.length - 1] ?? '';
    return (
      <DirectoryListing
        posts={directoryPosts}
        locale={locale}
        dirName={dirName}
      />
    );
  }

  // 3. Try zh-CN fallback for the article
  if (locale !== 'zh-CN') {
    const fallbackPost = findPost('zh-CN', slugPath);
    if (fallbackPost) {
      return <ArticleDetail post={fallbackPost} locale={locale} />;
    }
  }

  // 4. Nothing found
  notFound();
}

interface ArticleDetailProps {
  post: Post;
  locale: string;
}

function ArticleDetail({ post }: ArticleDetailProps) {
  const formattedDate = format(new Date(post.date), 'yyyy-MM-dd');

  return (
    <div className="min-h-screen bg-background">
      <Header />
      <main className="max-w-4xl mx-auto px-4 sm:px-6 lg:px-8 py-12">
        <article className="prose max-w-none">
          <h1 className="text-3xl font-bold text-foreground mb-4">
            {post.title}
          </h1>
          <div className="flex flex-wrap items-center gap-4 mb-8 text-sm text-muted-foreground">
            <time dateTime={post.date}>{formattedDate}</time>
            {post.tags && post.tags.length > 0 && (
              <div className="flex flex-wrap gap-2">
                {post.tags.map((tag) => (
                  <span
                    key={tag}
                    className="px-2 py-0.5 rounded-full bg-muted text-muted-foreground text-xs"
                  >
                    {tag}
                  </span>
                ))}
              </div>
            )}
          </div>
          <div dangerouslySetInnerHTML={{ __html: post.content }} />
        </article>
      </main>
      <Footer />
    </div>
  );
}

interface DirectoryListingProps {
  posts: Post[];
  locale: string;
  dirName: string;
}

function DirectoryListing({ posts, locale, dirName }: DirectoryListingProps) {
  const capitalizedDirName =
    dirName.charAt(0).toUpperCase() + dirName.slice(1);

  return (
    <div className="min-h-screen bg-background">
      <Header />
      <main className="max-w-4xl mx-auto px-4 sm:px-6 lg:px-8 py-12">
        <h1 className="text-3xl font-bold text-foreground mb-8">
          {capitalizedDirName}
        </h1>
        <ul className="space-y-8">
          {posts.map((post) => {
            const formattedDate = format(new Date(post.date), 'yyyy-MM-dd');
            return (
              <li key={post.slug} className="border-b pb-8 last:border-0">
                <NextLink
                  href={`/${locale}/${post.slug}`}
                  className="group"
                >
                  <h2 className="text-xl font-semibold text-foreground group-hover:text-primary transition-colors mb-2">
                    {post.title}
                  </h2>
                </NextLink>
                <time
                  dateTime={post.date}
                  className="text-sm text-muted-foreground block mb-2"
                >
                  {formattedDate}
                </time>
                {post.summary && (
                  <p className="text-muted-foreground">{post.summary}</p>
                )}
              </li>
            );
          })}
        </ul>
      </main>
      <Footer />
    </div>
  );
}
