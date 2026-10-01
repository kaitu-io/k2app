/**
 * Legal documents render per brand and per language, against the real files.
 *
 * These exist because tests/static-pages-ssr.test.ts mocks `fs/promises` to a
 * one-line stub, so it asserts the privacy/terms pages are Server Components
 * and nothing about what they actually serve. Under that coverage,
 * overleap.io/privacy told visitors they were registering a Kaitu account and
 * gave a kaitu.io address for data-subject requests, and every page rendered a
 * full Chinese copy of the policy above the English one.
 *
 * So read the shipped markdown here, run it through the real renderer for both
 * brands, and assert on the output a reader would see.
 */
import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import { KAITU, OVERLEAP, type Brand } from '../src/lib/brands';
import { renderLegalDoc } from '../src/lib/legal';

const LEGAL = path.resolve(__dirname, '../public/legal');

/** Documents both deployments serve. retailer-rules is kaitu-only (page.kaitu.tsx). */
const SHARED_DOCS = ['privacy-policy', 'terms-of-service', 'delete-account'] as const;

const read = (doc: string) => readFileSync(path.join(LEGAL, `${doc}.md`), 'utf8');

const KAITU_WORDS = /Kaitu|开途|開途|kaitu\.(io|me)/;
const OVERLEAP_WORDS = /[Oo]verleap/;

/** One locale per language master, per brand. */
const CASES: { brand: Brand; locale: string; zh: boolean }[] = [
  { brand: KAITU, locale: 'zh-CN', zh: true },
  { brand: KAITU, locale: 'zh-TW', zh: true },
  { brand: OVERLEAP, locale: 'en-GB', zh: false },
  { brand: OVERLEAP, locale: 'ja', zh: false },
];

describe.each(SHARED_DOCS)('%s', (doc) => {
  const raw = read(doc);

  it('holds no brand literal on disk — the words come from the registry', () => {
    expect(raw.match(KAITU_WORDS)).toBeNull();
    expect(raw.match(OVERLEAP_WORDS)).toBeNull();
  });

  it.each(CASES)('$brand.id / $locale renders one language, fully substituted', ({ brand, locale, zh }) => {
    const out = renderLegalDoc(raw, locale, brand);

    // Substitution is complete. An unresolved {{token}} on a legal page is the
    // kind of thing that ships: it reads as a typo, not as a missing value.
    expect(out).not.toMatch(/\{\{/);

    // The split happened: exactly one language master survives. Section
    // numbering is the discriminator — the zh master numbers 一、二、三, the
    // en master 1. 2. 3. — because both masters share every other marker.
    expect(out).not.toContain('## 中文版本');
    expect(out).not.toContain('## English Version');
    if (zh) {
      expect(out).toContain('### 一、');
      expect(out).not.toContain('### 1. ');
    } else {
      expect(out).toContain('### 1. ');
      expect(out).not.toContain('### 一、');
    }

    // Each master carries its own date; the bilingual preamble that used to
    // hold it is dropped with the other language.
    expect(out).toMatch(zh ? /\*\*最后更新：/ : /\*\*Last updated: /);

    // The reader sees this deployment's brand and only this deployment's brand.
    // Legal-signature exception (root CLAUDE.md, 法务文书署名): both deployments
    // sign as Overleap LLC, so strip exactly that string first — the same
    // scoping tests/brand-leak-ssr.test.tsx uses — and assert separately that it
    // is present, so the exception can't widen into a general free pass.
    expect(out).toContain(brand.legalName);
    const body = out.replaceAll(brand.legalName, '');
    const own = brand.id === 'kaitu' ? KAITU_WORDS : OVERLEAP_WORDS;
    const other = brand.id === 'kaitu' ? OVERLEAP_WORDS : KAITU_WORDS;
    expect(body).toMatch(own);
    expect(body.match(other)).toBeNull();

    // Root CLAUDE.md: the latin form is banned in Chinese user-facing copy
    // (开途, never "Kaitu"). Without this the brand-word assertion above passes
    // on either form, since both are "this brand's words".
    if (zh) expect(body).not.toMatch(/\bKaitu\b/);
  });

  it.each(CASES)('$brand.id / $locale names only its own contact addresses', ({ brand, locale }) => {
    const out = renderLegalDoc(raw, locale, brand);
    const foreign = (brand.id === 'kaitu' ? OVERLEAP : KAITU);
    for (const addr of [foreign.privacyEmail, foreign.legalEmail, foreign.contactEmail]) {
      expect(out).not.toContain(addr);
    }
  });
});

describe('delete-account satisfies the Play Data safety URL requirements', () => {
  // Play requires the linked page to (a) name the app or developer shown in the
  // listing, (b) show the steps prominently, and (c) state which data is deleted
  // and which is retained, with any extra retention period. A reviewer checks
  // all three by eye; these assert the substance survives an edit.
  const raw = read('delete-account');

  it.each(CASES)('$brand.id / $locale names the app and the developer', ({ brand, locale }) => {
    const out = renderLegalDoc(raw, locale, brand);
    expect(out).toContain(brand.legalName);
  });

  it.each(CASES)('$brand.id / $locale gives numbered in-app steps', ({ brand, locale, zh }) => {
    const out = renderLegalDoc(raw, locale, brand);
    expect(out).toMatch(/^1\. /m);
    expect(out).toMatch(/^4\. /m);
    // The step list must name the control the app actually shows, or the
    // reviewer cannot follow it.
    expect(out).toContain(zh ? '注销账号' : 'Delete Account');
  });

  it.each(CASES)('$brand.id / $locale states retention periods, not just deletion', ({ brand, locale, zh }) => {
    const out = renderLegalDoc(raw, locale, brand);
    expect(out).toMatch(zh ? /保留 7 年/ : /kept for 7 years/);
    expect(out).toMatch(zh ? /30 天内/ : /within 30 days/);
  });

  it.each(CASES)('$brand.id / $locale offers a route for uninstalled users', ({ brand, locale }) => {
    const out = renderLegalDoc(raw, locale, brand);
    expect(out).toContain(brand.privacyEmail);
  });
});

describe('renderLegalDoc edge cases', () => {
  it('returns a document with no language markers whole, still substituted', () => {
    const out = renderLegalDoc('Operated by {{legalName}}.', 'en-GB', OVERLEAP);
    expect(out).toBe('Operated by Overleap LLC.');
  });

  it('leaves an unknown placeholder visible rather than blanking it', () => {
    // A blank where a company name belongs reads as finished prose, and nothing
    // downstream would flag it.
    expect(renderLegalDoc('Contact {{nope}}.', 'en-GB', OVERLEAP)).toBe('Contact {{nope}}.');
  });

  it('uses the wordmark in Chinese and the latin name elsewhere', () => {
    // Root CLAUDE.md bans the latin form in Chinese user-facing copy, so the
    // token has to be locale-sensitive rather than one fixed string.
    expect(renderLegalDoc('{{brand}}', 'zh-CN', KAITU)).toBe(KAITU.wordmark);
    expect(renderLegalDoc('{{brand}}', 'en-GB', KAITU)).toBe(KAITU.displayName);
  });
});

describe('privacy-policy: the statistics section matches what the site actually does', () => {
  // The policy is one file served to both deployments, while the shared layout
  // loads a third-party analytics script only for a brand whose registry entry
  // has a measurement id. A sentence written once on disk is therefore false
  // for one of them; it is derived from the registry, and this locks the two.
  const raw = read('privacy-policy');
  const THIRD_PARTY = { zh: /第三方网站分析服务（Google Analytics）/, en: /third-party web-analytics service \(Google Analytics\)/ };
  const NONE = { zh: /不使用任何第三方分析工具/, en: /use no third-party analytics tools/ };

  const MATRIX = [KAITU, OVERLEAP].flatMap((brand) =>
    ['zh-CN', 'en-GB'].map((locale) => ({ brand, locale, lang: locale.startsWith('zh') ? 'zh' as const : 'en' as const })),
  );

  it('covers both registry states, so neither branch goes untested', () => {
    const states = new Set([KAITU, OVERLEAP].map((b) => Boolean(b.gaMeasurementId)));
    expect(states.size).toBe(2);
  });

  it.each(MATRIX)('$brand.id / $locale: third-party sentence ⇔ gaMeasurementId is set', ({ brand, locale, lang }) => {
    const out = renderLegalDoc(raw, locale, brand);
    const hasGa = Boolean(brand.gaMeasurementId);
    expect(THIRD_PARTY[lang].test(out)).toBe(hasGa);
    expect(NONE[lang].test(out)).toBe(!hasGa);
    expect(out).not.toMatch(/\{\{/);
  });

  it.each(MATRIX)('$brand.id / $locale: cookie lifetime and record retention are stated separately', ({ brand, locale, lang }) => {
    const out = renderLegalDoc(raw, locale, brand);
    if (lang === 'zh') {
      expect(out).toMatch(/Cookie 最长保留 400 天/);
      expect(out).toMatch(/统计事件记录在 120 天后删除/);
      expect(out).not.toMatch(/Cookie 保留 120 天/);
      expect(out).not.toMatch(/13 个月/);
      // The 120 days cover the event records only; the identifier ⇔ account
      // link lives until the account is deleted — on the site and in the app.
      expect(out).toMatch(/Cookie 标识符与您账户之间的关联会保留到您删除账户为止，届时该关联以及与您关联的事件记录会一并删除/);
      expect(out).toMatch(/设备标识符与您账户之间的关联会保留到您删除账户为止，届时该关联以及与您关联的事件记录会一并删除/);
      expect(out).not.toMatch(/保留 120 天/);
    } else {
      expect(out).toMatch(/cookie is kept for up to 400 days/);
      expect(out).toMatch(/statistics event records are deleted after 120 days/);
      expect(out).not.toMatch(/cookie is kept for 120 days/);
      expect(out).not.toMatch(/13 months/);
      expect(out).toMatch(/link between the cookie identifier and your account is kept until you delete your account, at which point the link and your linked event records are deleted/);
      expect(out).toMatch(/link between the device identifier and your account is kept until you delete your account, at which point the link and your linked event records are deleted/);
      expect(out).not.toMatch(/kept for 120 days/);
    }
  });

  it.each(MATRIX)('$brand.id / $locale: says what is recorded, what is not, and the account link', ({ brand, locale, lang }) => {
    const out = renderLegalDoc(raw, locale, brand);
    const must = lang === 'zh'
      ? [/页面（路径）/, /来源网站的域名/, /推广活动标记/, /国家\/地区/, /设备类型/, /操作系统/, /选择的套餐/, /点击下载及其对应的平台/, /在购买页请求和完成登录验证码/, /不保存 IP 地址/, /完整的浏览器标识/,
         /登录或付款后，这些记录会与您的账户关联/, /第一方/, /聚合/, /Global Privacy Control/,
         /打开 App/, /登录（查看登录界面、请求验证码、完成登录）/, /首次尝试连接/, /首次连接成功/, /连接成功（每天至多记录一次）/, /查看购买页/, /选择套餐/, /发起支付/, /打开订阅管理/,
         /仅适用于网站/, /不包含任何连接、流量或访问目的地信息/]
      : [/pages of this site you visit \(the path\)/, /referring site's domain/, /campaign tags/, /country\/region/, /device type/, /operating system/, /the plan you select/, /a download you click and its platform/,
         /requesting and completing a sign-in code on the purchase page/,
         /IP address and the full browser identification are not stored/,
         /After you sign in or pay, these records are linked to your account/, /first-party/, /aggregate/, /Global Privacy Control/,
         /opening the app/, /signing in \(viewing the sign-in screen, requesting a code, completing sign-in\)/, /first connection attempt/,
         /first successful connection/, /a successful connection at most once per day/,
         /viewing the purchase page/, /selecting a plan/, /starting a payment/, /opening subscription management/,
         /applies to the website only/, /no connection, traffic or destination information/];
    for (const re of must) expect(out, String(re)).toMatch(re);
  });
});

describe('privacy-policy: one "last updated" date throughout', () => {
  it('the file header agrees with both language sections', () => {
    const raw = read('privacy-policy');
    const dates = [...raw.matchAll(/(?:最后更新|Last [Uu]pdated)[^\d\n]*(\d{4}-\d{2})/g)].map((m) => m[1]);
    expect(dates).toHaveLength(3);
    expect(new Set(dates).size).toBe(1);
  });
});
