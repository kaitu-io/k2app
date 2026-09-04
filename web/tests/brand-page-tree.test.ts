/**
 * 页面树按品牌编译的结构守卫（spec 2026-09-04-overleap-site-decoupling §1）。
 *
 * next.config 的 `pageExtensions` 按构建期品牌取 [`${brand}.tsx`, 'tsx']：
 *   - `page.tsx` / `layout.tsx`     两品牌共用
 *   - `page.kaitu.tsx`              只进开途构建；Overleap 构建里该路径不存在（原生 404）
 *   - `page.overleap.tsx`           只进 Overleap 构建
 *
 * 这个守卫锁三件事，三件都是"本地两边都能跑、线上某一边默默错"的形状：
 *   1. 开途独有目录（后台、渠道页、路由器、发布说明……）不得出现裸 `page.tsx`——
 *      裸文件会把开途页编进 Overleap 站（第一波 /support /discovery /opensource 就是这么漏的）。
 *   2. 同一目录不得同时有 `page.tsx` 与 `page.<brand>.tsx`——那一品牌的构建会因同路径双页面失败，
 *      但另一品牌的构建是绿的，只有 CI 两个 build 都跑才会发现。
 *   3. `(manager)` 路由组整棵树（含 layout）必须带 `.kaitu.`。
 */
import { describe, expect, it } from 'vitest';
import { readdirSync, statSync } from 'node:fs';
import path from 'node:path';

const APP = path.resolve(__dirname, '../src/app');
const ROUTE_BASENAMES = ['page', 'layout', 'route', 'loading', 'error', 'not-found', 'template', 'default'];
const ROUTE_FILE_RE = new RegExp(`^(${ROUTE_BASENAMES.join('|')})(\\.(kaitu|overleap))?\\.tsx?$`);

/** 开途独有的路由目录（相对 src/app）。新增开途独有页请加到这里。 */
const KAITU_ONLY_DIRS = [
  '(manager)',
  '[locale]/discovery',
  '[locale]/opensource',
  '[locale]/routers',
  '[locale]/retailer',
  '[locale]/releases',
  '[locale]/changelog',
  '[locale]/g',
  '[locale]/s',
  '[locale]/survey',
  '[locale]/account/delegate',
  '[locale]/account/wallet',
];

interface RouteFile { dir: string; base: string; brand: 'kaitu' | 'overleap' | null; file: string }

function* walk(dir: string): Generator<string> {
  for (const name of readdirSync(dir)) {
    const p = path.join(dir, name);
    if (statSync(p).isDirectory()) yield* walk(p);
    else yield p;
  }
}

function routeFiles(): RouteFile[] {
  const out: RouteFile[] = [];
  for (const file of walk(APP)) {
    const m = path.basename(file).match(ROUTE_FILE_RE);
    if (!m) continue;
    out.push({
      dir: path.relative(APP, path.dirname(file)),
      base: m[1],
      brand: (m[3] as 'kaitu' | 'overleap' | undefined) ?? null,
      file: path.relative(APP, file),
    });
  }
  return out;
}

describe('brand page tree', () => {
  const files = routeFiles();

  it('discovers the route tree', () => {
    expect(files.length).toBeGreaterThan(20);
    expect(files.some((f) => f.file === '[locale]/page.kaitu.tsx')).toBe(true);
    expect(files.some((f) => f.file === '[locale]/page.overleap.tsx')).toBe(true);
  });

  it('kaitu-only directories carry no shared route file', () => {
    const leaks = files.filter(
      (f) => f.brand !== 'kaitu' && KAITU_ONLY_DIRS.some((d) => f.dir === d || f.dir.startsWith(`${d}/`) || f.dir.startsWith(`${d}\\`)),
    );
    expect(leaks.map((f) => f.file)).toEqual([]);
  });

  it('a directory never mixes a shared route file with a brand-specific one of the same kind', () => {
    const byKey = new Map<string, RouteFile[]>();
    for (const f of files) {
      const key = `${f.dir}::${f.base}`;
      byKey.set(key, [...(byKey.get(key) ?? []), f]);
    }
    const mixed = [...byKey.values()]
      .filter((group) => group.some((f) => f.brand === null) && group.some((f) => f.brand !== null))
      .map((group) => group.map((f) => f.file));
    expect(mixed).toEqual([]);
  });

  it('the (manager) tree is kaitu-only down to its layouts', () => {
    const manager = files.filter((f) => f.dir === '(manager)' || f.dir.startsWith('(manager)/'));
    expect(manager.length).toBeGreaterThan(20);
    expect(manager.filter((f) => f.brand !== 'kaitu').map((f) => f.file)).toEqual([]);
  });

  it('every overleap-specific page has a kaitu counterpart in the same directory (split, not fork)', () => {
    // 分裂页两边都要有：只有 overleap 版而没有 kaitu 版，等于开途站在该路径上默默 404。
    const overleapOnly = files.filter((f) => f.brand === 'overleap');
    const missing = overleapOnly.filter(
      (o) => !files.some((k) => k.brand === 'kaitu' && k.dir === o.dir && k.base === o.base),
    );
    expect(missing.map((f) => f.file)).toEqual([]);
  });
});
