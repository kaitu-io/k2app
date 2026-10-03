/**
 * Route-file naming guard.
 *
 * This site used to compile a per-brand page tree (`next.config.ts`
 * `pageExtensions` = [`${brand}.tsx`, 'tsx'], files named `page.kaitu.tsx` /
 * `page.overleap.tsx`). overleap.io moved to `sites/overleap/` and the
 * pageExtensions override is gone, so Next only picks up plain `page.tsx`.
 *
 * A route file still named `page.kaitu.tsx` (e.g. from a branch that predates
 * the switch) would now be silently ignored: the path 404s, every build and
 * every unit test that imports the module directly stays green. This test is
 * the only thing that turns that into a red build.
 */
import { describe, expect, it } from 'vitest';
import { readdirSync, statSync } from 'node:fs';
import path from 'node:path';

const APP = path.resolve(__dirname, '../src/app');
const ROUTE_BASENAMES = ['page', 'layout', 'route', 'loading', 'error', 'not-found', 'template', 'default'];
const BRANDED_ROUTE_FILE_RE = new RegExp(`^(${ROUTE_BASENAMES.join("|")})\\.(?!test\\.|spec\\.)[a-z]+\\.tsx?$`);

function* walk(dir: string): Generator<string> {
  for (const name of readdirSync(dir)) {
    const p = path.join(dir, name);
    if (statSync(p).isDirectory()) yield* walk(p);
    else yield p;
  }
}

describe('route file naming', () => {
  const files = [...walk(APP)].map((f) => path.relative(APP, f));

  it('discovers the route tree (liveness)', () => {
    expect(files).toContain(path.join('[locale]', 'page.tsx'));
    expect(files).toContain(path.join('(manager)', 'manager', 'layout.tsx'));
  });

  it('no route file carries a brand infix (page.<brand>.tsx is no longer compiled)', () => {
    const branded = files.filter((f) => BRANDED_ROUTE_FILE_RE.test(path.basename(f)));
    expect(branded).toEqual([]);
  });
});
