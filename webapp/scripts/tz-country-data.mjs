/**
 * Shared derivation for the IANA timezone → country table.
 *
 * Single source: the `countries-and-timezones` devDependency (data generated
 * from the IANA tzdb). Both the generator (generate-tz-country.mjs) and the
 * drift-guard test (tz-country.gen.test.ts) call this function, so the
 * committed artifact can never silently diverge from the library version
 * pinned in yarn.lock — upgrading the dep turns the test red until the table
 * is regenerated.
 */
import ct from 'countries-and-timezones';

/**
 * @returns {Array<[string, string]>} sorted [zoneName, lowercaseCountryCode]
 *   pairs covering ALL zones (deprecated aliases like Asia/Saigon included —
 *   OS timezone settings may still report them).
 */
export function deriveTzCountryRows() {
  const rows = [];
  for (const name of Object.keys(ct.getAllTimezones({ deprecated: true }))) {
    const country = ct.getCountryForTimezone(name);
    if (country) rows.push([name, country.id.toLowerCase()]);
  }
  rows.sort((a, b) => (a[0] < b[0] ? -1 : a[0] > b[0] ? 1 : 0));
  return rows;
}
