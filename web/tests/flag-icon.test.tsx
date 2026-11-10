/**
 * Every language the site serves has a real icon in the language menu — not the
 * two-letter fallback badge, which is only for a locale we forgot to draw.
 */
import fs from 'node:fs';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import FlagIcon from '../src/components/FlagIcon';
import { KAITU } from '../src/lib/brands';

describe('FlagIcon', () => {
  it.each([...KAITU.allowedLocales])('%s renders an svg flag', (locale) => {
    const html = renderToStaticMarkup(<FlagIcon locale={locale} />);
    expect(html).toContain('<svg');
    expect(html).not.toContain('bg-muted'); // the fallback badge
  });

  it('an undrawn locale still gets the code badge instead of nothing', () => {
    const html = renderToStaticMarkup(<FlagIcon locale="vi" />);
    expect(html).toContain('bg-muted');
    expect(html).toContain('VI');
  });

  // Dev aid: FLAG_DUMP=/abs/path.html vitest run tests/flag-icon.test.tsx
  it.runIf(process.env.FLAG_DUMP)('dump a contact sheet', () => {
    const cells = KAITU.allowedLocales
      .map((l) => {
        const svg = renderToStaticMarkup(<FlagIcon locale={l} className="w-24 h-16" />);
        const small = renderToStaticMarkup(<FlagIcon locale={l} className="w-4 h-4" />);
        return `<div style="display:inline-block;margin:12px;text-align:center;font:14px sans-serif"><div class="big">${svg}</div>${l}<div class="small">${small}</div></div>`;
      })
      .join('');
    fs.writeFileSync(process.env.FLAG_DUMP as string, `<body style="background:#f4f4f5"><style>.big svg,.big div{width:96px;height:64px;display:block}.small svg,.small div{width:16px;height:16px;display:block;margin:4px auto}</style>${cells}</body>`);
  });
});
