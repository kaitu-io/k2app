#!/usr/bin/env node
// Push every URL in the live sitemap to IndexNow (Bing, Yandex, Seznam, Naver).
// Run after a deploy that adds or changes articles:
//   node web/scripts/indexnow.mjs [https://www.kaitu.io]
// The key file web/public/6915425c17a621541b6fe75a30db8d5a.txt must be live on the host first; IndexNow
// fetches it to confirm you own the site.
const KEY = '6915425c17a621541b6fe75a30db8d5a';
const origin = (process.argv[2] ?? 'https://www.kaitu.io').replace(/\/+$/, '');
const host = new URL(origin).host;

const xml = await (await fetch(`${origin}/sitemap.xml`)).text();
const urls = [...xml.matchAll(/<loc>([^<]+)<\/loc>/g)].map((m) => m[1]);
if (urls.length === 0) throw new Error('sitemap has no <loc> entries');

const res = await fetch('https://api.indexnow.org/indexnow', {
  method: 'POST',
  headers: { 'content-type': 'application/json; charset=utf-8' },
  body: JSON.stringify({ host, key: KEY, keyLocation: `${origin}/${KEY}.txt`, urlList: urls }),
});
console.log(`submitted ${urls.length} urls -> HTTP ${res.status}`);
if (res.status >= 300) process.exit(1);
