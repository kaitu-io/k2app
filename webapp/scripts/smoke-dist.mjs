#!/usr/bin/env node
// White-screen smoke gate for a built webapp dist
// (spec: docs/superpowers/specs/2026-08-14-web-ota-design.md §6 step 4).
//
// Serves <dist-dir> over plain http (so main.tsx platform detection picks the
// standalone bridge — native call failures must degrade, not crash), loads it
// in headless Chromium, and requires:
//   1. #root gains children (the React app shell actually mounted), and
//   2. zero uncaught exceptions (pageerror) through a 1s settle window.
// console.error is reported but NOT fatal (standalone mode legitimately logs
// degraded-capability errors).
//
// Usage: node webapp/scripts/smoke-dist.mjs <dist-dir>
// Exit:  0 = smoke OK, 1 = smoke failed, 2 = usage error.

import { createServer } from 'node:http';
import { readFile } from 'node:fs/promises';
import { existsSync, statSync } from 'node:fs';
import path from 'node:path';
import { chromium } from '@playwright/test';

const MIME = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript',
  '.css': 'text/css',
  '.json': 'application/json',
  '.png': 'image/png',
  '.svg': 'image/svg+xml',
  '.ico': 'image/x-icon',
  '.webp': 'image/webp',
  '.woff2': 'font/woff2',
};

const dist = path.resolve(process.argv[2] ?? '');
if (!process.argv[2] || !existsSync(path.join(dist, 'index.html'))) {
  console.error(`usage: node smoke-dist.mjs <dist-dir> — index.html not found under: ${dist}`);
  process.exit(2);
}

const server = createServer(async (req, res) => {
  const urlPath = decodeURIComponent(new URL(req.url, 'http://localhost').pathname);
  let file = path.normalize(path.join(dist, urlPath));
  if (!file.startsWith(dist) || !existsSync(file) || statSync(file).isDirectory()) {
    file = path.join(dist, 'index.html'); // SPA fallback
  }
  try {
    const body = await readFile(file);
    res.writeHead(200, { 'content-type': MIME[path.extname(file)] ?? 'application/octet-stream' });
    res.end(body);
  } catch (err) {
    res.writeHead(500);
    res.end(String(err));
  }
});
await new Promise((resolve) => server.listen(0, '127.0.0.1', resolve));
const port = server.address().port;

const pageErrors = [];
const browser = await chromium.launch();
try {
  const page = await browser.newPage();
  page.on('pageerror', (err) => pageErrors.push(String(err)));
  page.on('console', (msg) => {
    if (msg.type() === 'error') console.warn(`[console.error] ${msg.text()}`);
  });
  await page.goto(`http://127.0.0.1:${port}/`, { waitUntil: 'load', timeout: 30_000 });
  await page.waitForFunction(
    () => {
      const root = document.getElementById('root');
      return !!root && root.children.length > 0;
    },
    undefined,
    { timeout: 20_000 },
  );
  await page.waitForTimeout(1_000); // settle window: catch late async crashes
  if (pageErrors.length > 0) {
    console.error(`SMOKE FAIL (${dist}): ${pageErrors.length} uncaught exception(s):`);
    for (const err of pageErrors) console.error(`  ${err}`);
    process.exit(1);
  }
  console.log(`smoke OK (${dist}): app shell rendered, no uncaught exceptions`);
} catch (err) {
  console.error(`SMOKE FAIL (${dist}): ${err}`);
  for (const perr of pageErrors) console.error(`  pageerror: ${perr}`);
  process.exit(1);
} finally {
  await browser.close();
  server.close();
}
