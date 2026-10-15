import { describe, it, expect } from 'vitest';
import { extractFaq, faqPageJsonLd } from '../src/lib/content-faq';

describe('extractFaq', () => {
  it('reads bold-paragraph questions and multi-paragraph answers', () => {
    const html = `
      <h2 id="a">开途是什么</h2><p><strong>不是问答</strong></p><p>正文</p>
      <h2 id="faq">常见问题</h2>
      <p><strong>快连还能用吗？</strong></p>
      <p>不能保证。</p>
      <p>第二段 <a href="/x">链接</a> &amp; 符号。</p>
      <p><strong>退款从哪天算？</strong></p>
      <ul><li>4 月 8 日</li></ul>
      <h2>延伸阅读</h2><p><strong>不算</strong></p><p>不算</p>`;
    expect(extractFaq(html)).toEqual([
      { question: '快连还能用吗？', answer: '不能保证。\n第二段 链接 & 符号。' },
      { question: '退款从哪天算？', answer: '4 月 8 日' },
    ]);
  });

  it('reads h3 questions and strips the Q： prefix', () => {
    const html = `<h2>常见问题（FAQ）</h2><h3 id="q1">Q：iPhone 为什么不能分流？</h3><p>系统限制。</p>`;
    expect(extractFaq(html)).toEqual([{ question: 'iPhone 为什么不能分流？', answer: '系统限制。' }]);
  });

  it('matches traditional-Chinese and English headings', () => {
    expect(extractFaq('<h2>常見問題</h2><h3>問？</h3><p>答。</p>')).toHaveLength(1);
    expect(extractFaq('<h2>FAQ</h2><h3>Q?</h3><p>A.</p>')).toHaveLength(1);
  });

  it('ignores a paragraph that is only partly bold', () => {
    const html = `<h2>常见问题</h2><p><strong>注意</strong>：这不是问题。</p>`;
    expect(extractFaq(html)).toEqual([]);
  });

  it('drops a question with no answer and yields nothing without a FAQ heading', () => {
    expect(extractFaq('<h2>常见问题</h2><p><strong>孤立？</strong></p>')).toEqual([]);
    expect(extractFaq('<h2>介绍</h2><p><strong>问？</strong></p><p>答</p>')).toEqual([]);
  });

  it('yields nothing for a bullet-list troubleshooting section', () => {
    const html = `<h2>常见问题排查</h2><ul><li><strong>注册失败</strong>：换节点</li></ul>`;
    expect(faqPageJsonLd(html)).toBeNull();
  });
});

describe('faqPageJsonLd', () => {
  it('builds a schema.org FAQPage', () => {
    const ld = faqPageJsonLd('<h2>常见问题</h2><h3>问？</h3><p>答。</p>');
    expect(ld).toEqual({
      '@context': 'https://schema.org',
      '@type': 'FAQPage',
      mainEntity: [{ '@type': 'Question', name: '问？', acceptedAnswer: { '@type': 'Answer', text: '答。' } }],
    });
  });
});
