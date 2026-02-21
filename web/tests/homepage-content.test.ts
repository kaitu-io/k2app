/**
 * Homepage Content Tests — T5
 *
 * Vitest tests for the homepage k2v5 technology rewrite.
 * RED phase: tests fail before implementation.
 *
 * Tests verify:
 * 1. zh-CN hero.json contains k2v5 keywords (ECH, 隐身, k2, 隧道)
 * 2. No locale hero.json contains incorrect MPTCP/CA cert references
 * 3. Comparison table covers all 5 protocols
 * 4. Quickstart section includes k2s and k2 up commands
 * 5. page.tsx contains SoftwareApplication JSON-LD
 * 6. metadata.ts does not contain incorrect descriptions
 */
import { describe, it, expect } from 'vitest';
import { readFileSync } from 'fs';
import { resolve } from 'path';

// __dirname = web/tests/  →  ../ = web/
const webRoot = resolve(__dirname, '../');

function readWebFile(relativePath: string): string {
  return readFileSync(resolve(webRoot, relativePath), 'utf-8');
}

function readHeroJson(locale: string): string {
  return readWebFile(`messages/${locale}/hero.json`);
}

const ALL_LOCALES = ['zh-CN', 'en-US', 'en-GB', 'en-AU', 'zh-TW', 'zh-HK', 'ja'];

describe('test_homepage_hero_k2_content', () => {
  it('zh-CN hero.json contains k2v5 technology keywords', () => {
    const content = readHeroJson('zh-CN');
    const hasECH = content.includes('ECH');
    const hasHidden = content.includes('隐身');
    const hasK2 = content.includes('k2');
    const hasTunnel = content.includes('隧道');
    expect(hasECH || hasHidden || hasK2 || hasTunnel).toBe(true);
  });

  it('zh-CN hero.json title contains k2 or 隧道', () => {
    const parsed = JSON.parse(readHeroJson('zh-CN'));
    const titleContent = JSON.stringify(parsed.hero?.title || '');
    const hasK2OrTunnel = titleContent.includes('k2') || titleContent.includes('隧道') || titleContent.includes('隐身');
    expect(hasK2OrTunnel).toBe(true);
  });

  it('zh-CN hero.json subtitle contains ECH', () => {
    const parsed = JSON.parse(readHeroJson('zh-CN'));
    const subtitleContent = JSON.stringify(parsed.hero?.subtitle || '');
    expect(subtitleContent.includes('ECH')).toBe(true);
  });
});

describe('test_homepage_no_mptcp_references', () => {
  it('zh-CN hero.json does not contain MPTCP', () => {
    expect(readHeroJson('zh-CN')).not.toContain('MPTCP');
  });

  it('zh-CN hero.json does not contain CA证书', () => {
    expect(readHeroJson('zh-CN')).not.toContain('CA证书');
  });

  it('zh-CN hero.json does not contain PBKDF2', () => {
    expect(readHeroJson('zh-CN')).not.toContain('PBKDF2');
  });

  it('zh-CN hero.json does not contain smux', () => {
    expect(readHeroJson('zh-CN')).not.toContain('smux');
  });

  it('zh-CN hero.json does not contain 5000', () => {
    expect(readHeroJson('zh-CN')).not.toContain('5000');
  });

  it('en-US hero.json does not contain MPTCP', () => {
    expect(readHeroJson('en-US')).not.toContain('MPTCP');
  });

  it('en-US hero.json does not contain CA certificate simulation', () => {
    expect(readHeroJson('en-US')).not.toContain('CA certificate simulation');
  });

  it('en-US hero.json does not contain PBKDF2', () => {
    expect(readHeroJson('en-US')).not.toContain('PBKDF2');
  });

  it('en-US hero.json does not contain smux', () => {
    expect(readHeroJson('en-US')).not.toContain('smux');
  });

  it('en-US hero.json does not contain 5000', () => {
    expect(readHeroJson('en-US')).not.toContain('5000');
  });

  it.each(ALL_LOCALES)('%s hero.json does not contain MPTCP', (locale) => {
    expect(readHeroJson(locale)).not.toContain('MPTCP');
  });

  it.each(ALL_LOCALES)('%s hero.json does not contain PBKDF2', (locale) => {
    expect(readHeroJson(locale)).not.toContain('PBKDF2');
  });

  it.each(ALL_LOCALES)('%s hero.json does not contain smux', (locale) => {
    expect(readHeroJson(locale)).not.toContain('smux');
  });

  it.each(ALL_LOCALES)('%s hero.json does not contain 5000 (concurrent)', (locale) => {
    // 5000 as a standalone concurrent reference (not part of port/year etc.)
    const content = readHeroJson(locale);
    expect(content).not.toContain('5000');
  });
});

describe('test_homepage_comparison_table', () => {
  it('zh-CN hero.json contains comparison data for k2', () => {
    const content = readHeroJson('zh-CN');
    // Should reference k2 protocol in comparison section
    expect(content.toLowerCase()).toContain('k2');
  });

  it('zh-CN hero.json contains comparison data for WireGuard', () => {
    expect(readHeroJson('zh-CN')).toContain('WireGuard');
  });

  it('zh-CN hero.json contains comparison data for VLESS', () => {
    expect(readHeroJson('zh-CN')).toContain('VLESS');
  });

  it('zh-CN hero.json contains comparison data for Hysteria2', () => {
    expect(readHeroJson('zh-CN')).toContain('Hysteria2');
  });

  it('zh-CN hero.json contains comparison data for Shadowsocks', () => {
    expect(readHeroJson('zh-CN')).toContain('Shadowsocks');
  });

  it('zh-CN hero.json comparison section has at least 5 protocols', () => {
    const parsed = JSON.parse(readHeroJson('zh-CN'));
    const comparison = parsed.hero?.comparison || parsed.comparison || {};
    const protocols = comparison.protocols || [];
    expect(protocols.length).toBeGreaterThanOrEqual(5);
  });
});

describe('test_homepage_quickstart_section', () => {
  it('zh-CN hero.json contains k2s server command reference', () => {
    const content = readHeroJson('zh-CN');
    expect(content).toContain('k2s');
  });

  it('zh-CN hero.json contains k2 up client command reference', () => {
    const content = readHeroJson('zh-CN');
    expect(content).toContain('k2 up');
  });

  it('zh-CN hero.json quickstart section exists', () => {
    const parsed = JSON.parse(readHeroJson('zh-CN'));
    const quickstart = parsed.hero?.quickstart || parsed.quickstart;
    expect(quickstart).toBeDefined();
  });
});

describe('test_homepage_json_ld', () => {
  it('page.tsx contains SoftwareApplication JSON-LD type', () => {
    const content = readWebFile('src/app/[locale]/page.tsx');
    expect(content).toContain('SoftwareApplication');
  });

  it('page.tsx contains application/ld+json script tag', () => {
    const content = readWebFile('src/app/[locale]/page.tsx');
    expect(content).toContain('application/ld+json');
  });
});

describe('test_metadata_no_incorrect_descriptions', () => {
  it('metadata.ts does not contain CA证书模拟', () => {
    const content = readWebFile('src/app/[locale]/metadata.ts');
    expect(content).not.toContain('CA证书模拟');
  });

  it('metadata.ts does not contain 网络代理服务', () => {
    const content = readWebFile('src/app/[locale]/metadata.ts');
    expect(content).not.toContain('网络代理服务');
  });

  it('metadata.ts does not contain 网络加速', () => {
    const content = readWebFile('src/app/[locale]/metadata.ts');
    expect(content).not.toContain('网络加速');
  });

  it('metadata.ts does not contain CA certificate simulation', () => {
    const content = readWebFile('src/app/[locale]/metadata.ts');
    expect(content).not.toContain('CA certificate simulation');
  });
});
