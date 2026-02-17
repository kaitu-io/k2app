#!/usr/bin/env node
/**
 * i18n key consistency checker.
 *
 * Compares all locale directories against zh-CN (primary).
 * Reports missing and extra keys per locale per namespace.
 *
 * Usage:
 *   node scripts/check-i18n.mjs            # Full detailed report
 *   node scripts/check-i18n.mjs --ci       # Summary only, exit 1 on missing keys
 */
import { readdir, readFile } from 'node:fs/promises';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const LOCALES_DIR = join(__dirname, '../webapp/src/i18n/locales');
const PRIMARY = 'zh-CN';
const ciMode = process.argv.includes('--ci');

function flattenKeys(obj, prefix = '') {
  const keys = [];
  for (const [k, v] of Object.entries(obj)) {
    const fullKey = prefix ? `${prefix}.${k}` : k;
    if (v !== null && typeof v === 'object' && !Array.isArray(v)) {
      keys.push(...flattenKeys(v, fullKey));
    } else {
      keys.push(fullKey);
    }
  }
  return keys;
}

async function getKeysFromFile(filePath) {
  const raw = await readFile(filePath, 'utf-8');
  return new Set(flattenKeys(JSON.parse(raw)));
}

async function getNamespaceFiles(localeDir) {
  const entries = await readdir(localeDir);
  return entries.filter(f => f.endsWith('.json')).sort();
}

async function main() {
  const allEntries = await readdir(LOCALES_DIR, { withFileTypes: true });
  const localeDirs = allEntries
    .filter(e => e.isDirectory() && e.name !== PRIMARY)
    .map(e => e.name)
    .sort();

  const primaryDir = join(LOCALES_DIR, PRIMARY);
  const primaryNamespaces = await getNamespaceFiles(primaryDir);

  const primaryKeysMap = new Map();
  let totalPrimaryKeys = 0;
  for (const ns of primaryNamespaces) {
    const keys = await getKeysFromFile(join(primaryDir, ns));
    primaryKeysMap.set(ns, keys);
    totalPrimaryKeys += keys.size;
  }

  if (!ciMode) {
    console.log(`Primary: ${PRIMARY} (${primaryNamespaces.length} namespaces, ${totalPrimaryKeys} keys)`);
    console.log();
  }

  const issues = [];
  const localeSummary = new Map();

  for (const locale of localeDirs) {
    const localeDir = join(LOCALES_DIR, locale);
    const localeFiles = await getNamespaceFiles(localeDir);
    const localeFileSet = new Set(localeFiles);
    let totalMissing = 0;
    let totalExtra = 0;
    const missingNamespaces = [];

    for (const ns of primaryNamespaces) {
      const primaryKeys = primaryKeysMap.get(ns);
      if (!localeFileSet.has(ns)) {
        missingNamespaces.push(ns.replace('.json', ''));
        issues.push({ locale, namespace: ns.replace('.json', ''), missing: [...primaryKeys], extra: [], missingNs: true });
        totalMissing += primaryKeys.size;
        continue;
      }
      const localeKeys = await getKeysFromFile(join(localeDir, ns));
      const missing = [...primaryKeys].filter(k => !localeKeys.has(k));
      const extra = [...localeKeys].filter(k => !primaryKeys.has(k));
      if (missing.length > 0 || extra.length > 0) {
        issues.push({ locale, namespace: ns.replace('.json', ''), missing, extra, missingNs: false });
      }
      totalMissing += missing.length;
      totalExtra += extra.length;
    }

    const extraNsFiles = localeFiles.filter(f => !primaryKeysMap.has(f));
    for (const ns of extraNsFiles) {
      const localeKeys = await getKeysFromFile(join(localeDir, ns));
      issues.push({ locale, namespace: ns.replace('.json', '') + ' (EXTRA NS)', missing: [], extra: [...localeKeys], missingNs: false });
      totalExtra += localeKeys.size;
    }

    localeSummary.set(locale, { missing: totalMissing, extra: totalExtra, missingNs: missingNamespaces });
  }

  // Summary table
  console.log('Locale'.padEnd(10) + 'Missing'.padStart(10) + 'Extra'.padStart(10) + '  Missing Namespaces');
  console.log('-'.repeat(70));
  let anyMissing = false;
  for (const locale of localeDirs) {
    const s = localeSummary.get(locale);
    console.log(
      locale.padEnd(10) +
      String(s.missing).padStart(10) +
      String(s.extra).padStart(10) +
      '  ' + (s.missingNs.length > 0 ? s.missingNs.join(', ') : '-')
    );
    if (s.missing > 0) anyMissing = true;
  }

  const totalMissing = [...localeSummary.values()].reduce((sum, s) => sum + s.missing, 0);
  const totalExtra = [...localeSummary.values()].reduce((sum, s) => sum + s.extra, 0);
  console.log('-'.repeat(70));
  console.log(`Total: ${totalMissing} missing, ${totalExtra} extra across ${localeDirs.length} locales (baseline: ${totalPrimaryKeys} keys)`);

  // Detailed report (skip in CI mode)
  if (!ciMode && issues.length > 0) {
    console.log();
    for (const locale of localeDirs) {
      const localeIssues = issues.filter(i => i.locale === locale);
      if (localeIssues.length === 0) continue;
      console.log(`\n--- ${locale} ---`);
      for (const issue of localeIssues) {
        if (issue.missingNs) {
          console.log(`  [${issue.namespace}] ENTIRE NAMESPACE MISSING (${issue.missing.length} keys)`);
          continue;
        }
        if (issue.missing.length > 0) {
          console.log(`  [${issue.namespace}] Missing ${issue.missing.length} key(s):`);
          for (const k of issue.missing) console.log(`    - ${k}`);
        }
        if (issue.extra.length > 0) {
          console.log(`  [${issue.namespace}] Extra ${issue.extra.length} key(s):`);
          for (const k of issue.extra) console.log(`    + ${k}`);
        }
      }
    }
  }

  if (anyMissing) process.exitCode = 1;
}

main().catch(err => { console.error(err); process.exitCode = 2; });
