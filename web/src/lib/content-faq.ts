/**
 * FAQ extraction for Velite content posts → schema.org FAQPage JSON-LD.
 *
 * Works on the rendered HTML (post.content), so it does not care whether the
 * author wrote `**问题？**`, `<strong>问题？</strong>` or `### Q：问题？`.
 * The FAQ section is the `<h2>` whose text starts with 常见问题 / 常見問題 / FAQ;
 * inside it a question is either an `<h3>` or a paragraph that is entirely
 * bold, and its answer is every following block until the next question or
 * `<h2>`. A section in any other shape (e.g. a bullet list) yields nothing —
 * no FAQPage is better than a wrong one.
 */

export interface FaqItem {
  question: string;
  answer: string;
}

const FAQ_HEADING = /^(常见问题|常見問題|FAQ)/i;

function toText(html: string): string {
  return html
    .replace(/<[^>]+>/g, '')
    .replace(/&nbsp;/g, ' ')
    .replace(/&lt;/g, '<')
    .replace(/&gt;/g, '>')
    .replace(/&quot;/g, '"')
    .replace(/&#39;|&#x27;/g, "'")
    .replace(/&amp;/g, '&')
    .replace(/\s+/g, ' ')
    .trim();
}

/** Extract FAQ Q&A pairs from a post's rendered HTML. */
export function extractFaq(html: string): FaqItem[] {
  const h2s = [...html.matchAll(/<h2\b[^>]*>([\s\S]*?)<\/h2>/g)];
  const idx = h2s.findIndex((m) => FAQ_HEADING.test(toText(m[1])));
  if (idx < 0) return [];

  const start = h2s[idx].index! + h2s[idx][0].length;
  const end = idx + 1 < h2s.length ? h2s[idx + 1].index! : html.length;
  const section = html.slice(start, end);

  const items: FaqItem[] = [];
  let current: { question: string; answer: string[] } | null = null;
  const flush = () => {
    if (current && current.answer.length > 0) {
      items.push({ question: current.question, answer: current.answer.join('\n') });
    }
  };

  for (const block of section.matchAll(/<(h3|p|ul|ol|blockquote|table)\b[^>]*>([\s\S]*?)<\/\1>/g)) {
    const [, tag, inner] = block;
    const boldOnly = tag === 'p' && /^\s*<strong>([\s\S]*?)<\/strong>\s*$/.exec(inner);
    if (tag === 'h3' || boldOnly) {
      flush();
      const raw = toText(boldOnly ? boldOnly[1] : inner).replace(/^Q[：:]\s*/, '');
      current = { question: raw, answer: [] };
      continue;
    }
    const text = toText(inner);
    if (current && text) current.answer.push(text);
  }
  flush();
  return items;
}

/** FAQPage JSON-LD for a post, or null when it has no recognisable FAQ. */
export function faqPageJsonLd(html: string): Record<string, unknown> | null {
  const items = extractFaq(html);
  if (items.length === 0) return null;
  return {
    '@context': 'https://schema.org',
    '@type': 'FAQPage',
    mainEntity: items.map((item) => ({
      '@type': 'Question',
      name: item.question,
      acceptedAnswer: { '@type': 'Answer', text: item.answer },
    })),
  };
}
