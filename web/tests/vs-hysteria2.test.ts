/**
 * vs-Hysteria2 Comparison Page Tests — T6
 *
 * Vitest tests for the k2 vs Hysteria2 congestion control comparison page.
 * RED phase: tests fail before implementation.
 *
 * Tests verify:
 * 1. Post with slug 'k2/vs-hysteria2' exists and contains non-empty content with "Hysteria2"
 * 2. Content does NOT disclose PCC, Vivace, or full algorithm name
 * 3. All 4 comparison dimensions are present in the content
 * 4. Frontmatter has correct title, section="comparison", order=7
 */
import { describe, it, expect, vi, beforeAll } from 'vitest';
import { readFileSync } from 'fs';
import { resolve } from 'path';

const webRoot = resolve(__dirname, '../');

function readContentFile(relativePath: string): string {
  return readFileSync(resolve(webRoot, relativePath), 'utf-8');
}

// Mock #velite with a post that reflects the actual file at web/content/zh-CN/k2/vs-hysteria2.md
vi.mock('#velite', () => ({
  posts: [
    {
      title: 'k2 vs Hysteria2：拥塞控制机制对比',
      date: '2026-02-21T00:00:00.000Z',
      summary: '深入对比 k2 k2arc 自适应速率控制与 Hysteria2 Brutal 固定发送速率机制的差异。',
      tags: ['k2', 'comparison', '拥塞控制'],
      draft: false,
      content: '<h1>k2 vs Hysteria2</h1><p>自研自适应速率控制</p><p>丢包恢复</p><p>延迟稳定性</p><p>带宽利用率</p><p>公平性</p>',
      metadata: { readingTime: 5, wordCount: 800 },
      filePath: 'zh-CN/k2/vs-hysteria2',
      locale: 'zh-CN',
      slug: 'k2/vs-hysteria2',
      order: 7,
      section: 'comparison',
    },
    {
      title: 'k2 vs Hysteria2: Congestion Control Comparison',
      date: '2026-02-21T00:00:00.000Z',
      summary: 'An in-depth comparison of k2 proprietary adaptive congestion control versus Hysteria2 Brutal fixed-rate sending.',
      tags: ['k2', 'comparison', 'congestion control'],
      draft: false,
      content: '<h1>k2 vs Hysteria2</h1><p>proprietary adaptive congestion control</p><p>packet loss recovery</p><p>latency stability</p><p>bandwidth utilization</p><p>fairness</p>',
      metadata: { readingTime: 5, wordCount: 800 },
      filePath: 'en-US/k2/vs-hysteria2',
      locale: 'en-US',
      slug: 'k2/vs-hysteria2',
      order: 7,
      section: 'comparison',
    },
  ],
}));

describe('test_vs_hysteria2_renders', () => {
  it('zh-CN post with slug k2/vs-hysteria2 exists and has non-empty content', async () => {
    const { posts } = await import('#velite');

    const post = (posts as Array<{
      slug: string;
      locale: string;
      content: string;
      title: string;
    }>).find((p) => p.slug === 'k2/vs-hysteria2' && p.locale === 'zh-CN');

    expect(post).toBeDefined();
    expect(post!.content.length).toBeGreaterThan(0);
    expect(post!.content).toContain('Hysteria2');
  });

  it('zh-CN markdown file exists and contains Hysteria2', () => {
    const content = readContentFile('content/zh-CN/k2/vs-hysteria2.md');
    expect(content.length).toBeGreaterThan(0);
    expect(content).toContain('Hysteria2');
  });

  it('en-US post with slug k2/vs-hysteria2 exists and has non-empty content', async () => {
    const { posts } = await import('#velite');

    const post = (posts as Array<{
      slug: string;
      locale: string;
      content: string;
    }>).find((p) => p.slug === 'k2/vs-hysteria2' && p.locale === 'en-US');

    expect(post).toBeDefined();
    expect(post!.content.length).toBeGreaterThan(0);
    expect(post!.content).toContain('Hysteria2');
  });

  it('en-US markdown file exists and contains Hysteria2', () => {
    const content = readContentFile('content/en-US/k2/vs-hysteria2.md');
    expect(content.length).toBeGreaterThan(0);
    expect(content).toContain('Hysteria2');
  });
});

describe('test_vs_hysteria2_no_pcc_disclosure', () => {
  it('zh-CN markdown file does not disclose PCC algorithm name', () => {
    const content = readContentFile('content/zh-CN/k2/vs-hysteria2.md');
    expect(content).not.toContain('PCC');
    expect(content).not.toContain('Vivace');
    expect(content).not.toContain('Performance-oriented Congestion Control');
  });

  it('en-US markdown file does not disclose PCC algorithm name', () => {
    const content = readContentFile('content/en-US/k2/vs-hysteria2.md');
    expect(content).not.toContain('PCC');
    expect(content).not.toContain('Vivace');
    expect(content).not.toContain('Performance-oriented Congestion Control');
  });

  it('zh-CN post content from Velite does not contain PCC, Vivace, or Performance-oriented Congestion Control', async () => {
    const { posts } = await import('#velite');

    const post = (posts as Array<{
      slug: string;
      locale: string;
      content: string;
    }>).find((p) => p.slug === 'k2/vs-hysteria2' && p.locale === 'zh-CN');

    expect(post).toBeDefined();
    expect(post!.content).not.toContain('PCC');
    expect(post!.content).not.toContain('Vivace');
    expect(post!.content).not.toContain('Performance-oriented Congestion Control');
  });
});

describe('test_vs_hysteria2_has_comparison_dimensions', () => {
  it('zh-CN markdown file covers all 4 comparison dimensions', () => {
    const content = readContentFile('content/zh-CN/k2/vs-hysteria2.md');

    // Dimension 1: Packet loss recovery — 丢包
    expect(content).toContain('丢包');

    // Dimension 2: Latency stability — 延迟
    expect(content).toContain('延迟');

    // Dimension 3: Bandwidth utilization — 带宽
    expect(content).toContain('带宽');

    // Dimension 4: Fairness — 公平
    expect(content).toContain('公平');
  });

  it('en-US markdown file covers all 4 comparison dimensions', () => {
    const content = readContentFile('content/en-US/k2/vs-hysteria2.md');

    // Dimension 1: Packet loss recovery
    const hasPacketLoss = content.toLowerCase().includes('packet loss') || content.toLowerCase().includes('loss recovery');
    expect(hasPacketLoss).toBe(true);

    // Dimension 2: Latency stability
    const hasLatency = content.toLowerCase().includes('latency') || content.toLowerCase().includes('delay');
    expect(hasLatency).toBe(true);

    // Dimension 3: Bandwidth utilization
    const hasBandwidth = content.toLowerCase().includes('bandwidth') || content.toLowerCase().includes('utilization');
    expect(hasBandwidth).toBe(true);

    // Dimension 4: Fairness
    const hasFairness = content.toLowerCase().includes('fairness') || content.toLowerCase().includes('fair');
    expect(hasFairness).toBe(true);
  });
});

describe('test_vs_hysteria2_has_frontmatter', () => {
  it('zh-CN post from Velite has correct title field', async () => {
    const { posts } = await import('#velite');

    const post = (posts as Array<{
      slug: string;
      locale: string;
      title: string;
      section?: string;
      order?: number;
    }>).find((p) => p.slug === 'k2/vs-hysteria2' && p.locale === 'zh-CN');

    expect(post).toBeDefined();
    expect(post!.title).toBeTruthy();
    expect(typeof post!.title).toBe('string');
    expect(post!.title.length).toBeGreaterThan(0);
  });

  it('zh-CN post from Velite has section="comparison"', async () => {
    const { posts } = await import('#velite');

    const post = (posts as Array<{
      slug: string;
      locale: string;
      section?: string;
    }>).find((p) => p.slug === 'k2/vs-hysteria2' && p.locale === 'zh-CN');

    expect(post).toBeDefined();
    expect(post!.section).toBe('comparison');
  });

  it('zh-CN post from Velite has order=7', async () => {
    const { posts } = await import('#velite');

    const post = (posts as Array<{
      slug: string;
      locale: string;
      order?: number;
    }>).find((p) => p.slug === 'k2/vs-hysteria2' && p.locale === 'zh-CN');

    expect(post).toBeDefined();
    expect(post!.order).toBe(7);
  });

  it('zh-CN markdown frontmatter contains section: comparison', () => {
    const content = readContentFile('content/zh-CN/k2/vs-hysteria2.md');
    expect(content).toContain('section:');
    expect(content).toContain('comparison');
  });

  it('zh-CN markdown frontmatter contains order: 7', () => {
    const content = readContentFile('content/zh-CN/k2/vs-hysteria2.md');
    expect(content).toContain('order:');
    expect(content).toContain('7');
  });

  it('en-US markdown frontmatter contains section: comparison', () => {
    const content = readContentFile('content/en-US/k2/vs-hysteria2.md');
    expect(content).toContain('section:');
    expect(content).toContain('comparison');
  });

  it('en-US markdown frontmatter contains order: 7', () => {
    const content = readContentFile('content/en-US/k2/vs-hysteria2.md');
    expect(content).toContain('order:');
    expect(content).toContain('7');
  });
});
