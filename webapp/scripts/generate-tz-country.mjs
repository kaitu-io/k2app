/**
 * Regenerates src/utils/tz-country.gen.ts from the countries-and-timezones
 * devDependency. Run after upgrading that dependency:
 *
 *   node scripts/generate-tz-country.mjs
 *
 * The committed artifact is locked by src/utils/__tests__/tz-country.gen.test.ts.
 */
import { writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { deriveTzCountryRows } from './tz-country-data.mjs';

const rows = deriveTzCountryRows();
const body = rows.map(([zone, cc]) => `  '${zone}': '${cc}',`).join('\n');

const out = `/**
 * GENERATED FILE — do not edit by hand.
 *
 * IANA timezone → ISO 3166-1 alpha-2 country code (lowercase), covering all
 * zones including deprecated aliases (Asia/Saigon, PRC, …) that OS timezone
 * settings may still report. Derived from the \`countries-and-timezones\`
 * devDependency; regenerate with \`node scripts/generate-tz-country.mjs\`.
 * Drift guard: src/utils/__tests__/tz-country.gen.test.ts.
 */

export const TZ_COUNTRY: Readonly<Record<string, string>> = Object.freeze({
${body}
});
`;

const dest = join(dirname(fileURLToPath(import.meta.url)), '../src/utils/tz-country.gen.ts');
writeFileSync(dest, out);
console.log(`wrote ${dest} (${rows.length} zones)`);
