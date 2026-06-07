#!/usr/bin/env node
// ASC IAP setup — idempotent creation of the subscription group + 4 auto-renewable
// subscriptions + localizations (+ optional CHN-base pricing with territory
// equalization) on the ANC app via the App Store Connect REST API.
//
// Covers Phase 0 items #2–#5 (see docs/superpowers/plans/2026-06-04-ios-storekit-iap.md).
// Items #1 (Paid Apps agreement), #6 (App Store Server API .p8), #7 (S2S URL),
// #8 (sandbox tester) are web-UI-only and stay manual.
//
//   node scripts/asc-iap-setup.mjs            # dry-run: print the plan, mutate nothing
//   node scripts/asc-iap-setup.mjs --apply    # create group + subs + localizations
//   node scripts/asc-iap-setup.mjs --apply --prices  # also set CHN base price + equalize
//
// Auth: reads the same ASC API key the `asc` MCP uses, from env first, then the
// known on-disk fallback. No secret is hardcoded in this file.
//   APP_STORE_CONNECT_KEY_ID, APP_STORE_CONNECT_ISSUER_ID, APP_STORE_CONNECT_P8_PATH

import crypto from 'node:crypto';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';

const KEY_ID = process.env.APP_STORE_CONNECT_KEY_ID || '964C28484J';
const ISSUER = process.env.APP_STORE_CONNECT_ISSUER_ID || '2013a105-1952-4f9d-969e-922db074a833';
const P8_PATH = process.env.APP_STORE_CONNECT_P8_PATH ||
  path.join(os.homedir(), '.appstoreconnect', `AuthKey_${KEY_ID}.p8`);

const APP_ID = '6448744655';                 // 开途 - 越拥堵，越从容 (com.allnationconnect.anc.wgios)
const GROUP_REF = 'Kaitu Pro';               // internal reference name (not user-facing)
const GROUP_DISPLAY = { 'zh-Hans': '开途会员', 'en-US': 'Kaitu Membership' };
const BASE_TERRITORY = 'USA';                // home territory; Center plan prices are USD

// iOS launch = a single 1-year auto-renewable subscription at parity with the
// Center `1y` plan ($49/yr). basic-only (no family tier); Apple auto-renew max
// period is 1 year so the 2y/3y/5y bulk plans cannot be auto-renewable and are
// web-only. `name` = internal reference (<=64). Localized user-facing display
// names live in `display` (zh-Hans uses 开途, never "Kaitu").
const PRODUCTS = [
  { productId: 'io.kaitu.sub.basic.1y',  name: 'Kaitu Annual',  period: 'ONE_YEAR',  level: 1, family: false,
    display: { 'zh-Hans': { name: '开途会员 · 年付', desc: '开途会员，按年自动续订，可随时取消。' },
               'en-US':   { name: 'Kaitu Annual', desc: 'Kaitu membership, billed yearly. Cancel anytime.' } } },
];

// USD customer price for the base territory (USA), matching the Center plan.
// The script picks the nearest available USD subscription price point, then
// equalizes to all other territories. ($49 likely maps to $48.99 / $49.99.)
const PRICES = {
  'io.kaitu.sub.basic.1y': 49,
};

// ---- auth ----
const b64 = (b) => Buffer.from(b).toString('base64').replace(/=/g, '').replace(/\+/g, '-').replace(/\//g, '_');
function makeJwt() {
  const p8 = fs.readFileSync(P8_PATH, 'utf8');
  const header = b64(JSON.stringify({ alg: 'ES256', kid: KEY_ID, typ: 'JWT' }));
  const now = Math.floor(Date.now() / 1000);
  const payload = b64(JSON.stringify({ iss: ISSUER, iat: now, exp: now + 600, aud: 'appstoreconnect-v1' }));
  const si = `${header}.${payload}`;
  const sig = crypto.sign('SHA256', Buffer.from(si), { key: crypto.createPrivateKey(p8), dsaEncoding: 'ieee-p1363' });
  return `${si}.${b64(sig)}`;
}
const TOKEN = makeJwt();
const BASE = 'https://api.appstoreconnect.apple.com';

const APPLY = process.argv.includes('--apply');
const DO_PRICES = process.argv.includes('--prices');

async function api(method, p, body) {
  const r = await fetch(`${BASE}${p}`, {
    method,
    headers: { Authorization: `Bearer ${TOKEN}`, 'Content-Type': 'application/json' },
    body: body ? JSON.stringify(body) : undefined,
  });
  const text = await r.text();
  let json; try { json = text ? JSON.parse(text) : null; } catch { json = text; }
  if (!r.ok) {
    const err = new Error(`${method} ${p} -> ${r.status}`);
    err.status = r.status; err.body = json;
    throw err;
  }
  return json;
}
const get = (p) => api('GET', p);
function plan(label, payload) {
  console.log(`  [DRY-RUN] would create ${label}:`, JSON.stringify(payload?.data?.attributes ?? payload));
}

// ---- idempotent steps ----
async function ensureGroup() {
  const existing = await get(`/v1/apps/${APP_ID}/subscriptionGroups?limit=100`);
  const found = existing.data.find((g) => g.attributes.referenceName === GROUP_REF);
  if (found) { console.log(`✓ group "${GROUP_REF}" exists: ${found.id}`); return found.id; }
  const payload = { data: { type: 'subscriptionGroups', attributes: { referenceName: GROUP_REF },
    relationships: { app: { data: { type: 'apps', id: APP_ID } } } } };
  if (!APPLY) { plan(`subscriptionGroup "${GROUP_REF}"`, payload); return '<group-id>'; }
  const created = await api('POST', '/v1/subscriptionGroups', payload);
  console.log(`+ created group "${GROUP_REF}": ${created.data.id}`);
  return created.data.id;
}

async function ensureGroupLocalizations(groupId) {
  const existing = APPLY && !groupId.startsWith('<')
    ? (await get(`/v1/subscriptionGroups/${groupId}/subscriptionGroupLocalizations?limit=50`)).data : [];
  for (const [locale, name] of Object.entries(GROUP_DISPLAY)) {
    if (existing.find((l) => l.attributes.locale === locale)) { console.log(`  ✓ group loc ${locale}`); continue; }
    const payload = { data: { type: 'subscriptionGroupLocalizations', attributes: { name, locale },
      relationships: { subscriptionGroup: { data: { type: 'subscriptionGroups', id: groupId } } } } };
    if (!APPLY) { plan(`group loc ${locale} "${name}"`, payload); continue; }
    await api('POST', '/v1/subscriptionGroupLocalizations', payload);
    console.log(`  + group loc ${locale} "${name}"`);
  }
}

async function ensureSubscription(groupId, prod) {
  const existing = APPLY && !groupId.startsWith('<')
    ? (await get(`/v1/subscriptionGroups/${groupId}/subscriptions?limit=100`)).data : [];
  let sub = existing.find((s) => s.attributes.productId === prod.productId);
  if (sub) { console.log(`✓ sub ${prod.productId} exists: ${sub.id}`); }
  else {
    const payload = { data: { type: 'subscriptions', attributes: {
      name: prod.name, productId: prod.productId, subscriptionPeriod: prod.period,
      familySharable: prod.family, groupLevel: prod.level },
      relationships: { group: { data: { type: 'subscriptionGroups', id: groupId } } } } };
    if (!APPLY) { plan(`subscription ${prod.productId}`, payload); sub = { id: `<sub:${prod.productId}>` }; }
    else { const c = await api('POST', '/v1/subscriptions', payload); sub = c.data; console.log(`+ created sub ${prod.productId}: ${sub.id}`); }
  }
  // localizations
  const locs = APPLY && !sub.id.startsWith('<')
    ? (await get(`/v1/subscriptions/${sub.id}/subscriptionLocalizations?limit=50`)).data : [];
  for (const [locale, d] of Object.entries(prod.display)) {
    if (locs.find((l) => l.attributes.locale === locale)) { console.log(`  ✓ sub loc ${locale}`); continue; }
    const payload = { data: { type: 'subscriptionLocalizations', attributes: { name: d.name, locale, description: d.desc },
      relationships: { subscription: { data: { type: 'subscriptions', id: sub.id } } } } };
    if (!APPLY) { plan(`sub loc ${prod.productId}/${locale} "${d.name}"`, payload); continue; }
    await api('POST', '/v1/subscriptionLocalizations', payload);
    console.log(`  + sub loc ${locale} "${d.name}"`);
  }
  return sub;
}

// Phase B — USA base price (BASE_TERRITORY) + equalize to all territories.
async function ensurePrice(sub, prod) {
  const target = PRICES[prod.productId];
  if (!target || target <= 0) { console.log(`  ! no price set for ${prod.productId} (PRICES map is 0) — skipping`); return; }
  if (!APPLY || sub.id.startsWith('<')) { console.log(`  [DRY-RUN] would set ${prod.productId} ${BASE_TERRITORY} $${target} + equalize`); return; }

  // 1. find the BASE_TERRITORY price point whose customerPrice == target (nearest if exact missing)
  let url = `/v1/subscriptions/${sub.id}/pricePoints?filter[territory]=${BASE_TERRITORY}&limit=200`;
  const points = [];
  while (url) {
    const r = await get(url);
    points.push(...r.data);
    url = r.links?.next ? r.links.next.replace(BASE, '') : null;
  }
  const byPrice = points.map((p) => ({ id: p.id, price: parseFloat(p.attributes.customerPrice) }))
    .sort((a, b) => Math.abs(a.price - target) - Math.abs(b.price - target));
  const pick = byPrice[0];
  if (!pick) { console.log(`  ! no ${BASE_TERRITORY} price points for ${prod.productId}`); return; }
  if (pick.price !== target) console.log(`  ~ ${prod.productId}: no exact ¥${target}, nearest ¥${pick.price}`);

  // 2. existing price already set?
  const existing = (await get(`/v1/subscriptions/${sub.id}/prices?limit=200`)).data;
  if (existing.length) { console.log(`  ✓ ${prod.productId} already has ${existing.length} price(s) — skipping`); return; }

  // 3. equalize: pull equalized points for the picked CHN point across all territories
  const eqUrl = `/v1/subscriptionPricePoints/${pick.id}/equalizations?limit=200`;
  const eqPoints = [pick.id];
  let next = eqUrl;
  while (next) {
    const r = await get(next);
    eqPoints.push(...r.data.map((d) => d.id));
    next = r.links?.next ? r.links.next.replace(BASE, '') : null;
  }
  // 4. create a subscriptionPrice per equalized point (immediate start)
  let n = 0; let firstErr = null;
  for (const ppId of eqPoints) {
    try {
      await api('POST', '/v1/subscriptionPrices', { data: { type: 'subscriptionPrices',
        attributes: { preserveCurrentPrice: false },
        relationships: {
          subscription: { data: { type: 'subscriptions', id: sub.id } },
          subscriptionPricePoint: { data: { type: 'subscriptionPricePoints', id: ppId } } } } });
      n++;
    } catch (e) { if (!firstErr) firstErr = e; /* some territories reject; continue */ }
  }
  console.log(`  + ${prod.productId}: ${BASE_TERRITORY} base ${pick.price} + equalized ${n}/${eqPoints.length} territories`);
  if (n === 0 && firstErr) {
    console.log(`  ! ALL price writes failed — likely the Paid Applications Agreement (#1) is not yet active.`);
    console.log(`    first error: ${firstErr.status} ${JSON.stringify(firstErr.body?.errors?.[0]?.detail || firstErr.body)}`);
  }
}

// ---- main ----
console.log(`ASC IAP setup — app ${APP_ID}, ${APPLY ? 'APPLY' : 'DRY-RUN'}${DO_PRICES ? ' +prices' : ''}\n`);
const groupId = await ensureGroup();
await ensureGroupLocalizations(groupId);
const subs = [];
for (const prod of PRODUCTS) subs.push({ sub: await ensureSubscription(groupId, prod), prod });
if (DO_PRICES) { console.log('\n-- pricing --'); for (const { sub, prod } of subs) await ensurePrice(sub, prod); }
console.log('\nDone.', APPLY ? '' : '(dry-run — re-run with --apply to create)');
console.log('Remaining manual (web UI): #1 Paid Apps agreement+banking/tax, #6 App Store Server API .p8, #7 S2S URL, #8 sandbox tester.');
console.log('Review screenshots per subscription are also required before App Review submission.');
