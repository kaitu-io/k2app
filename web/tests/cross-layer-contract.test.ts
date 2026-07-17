/**
 * Cross-layer brand contract gate (web ↔ Go).
 *
 * The `brand` concept is defined three times — `api/brand.go`, `webapp/src/brands/*`,
 * and `web/src/lib/brands.ts` — with no shared type. Drift between them is silent
 * and has already shipped a real outage (the API CORS allow-list omitted a header
 * the client always sends, killing the direct channel with no error anywhere).
 *
 * `contracts/api-contract.json` is exported from LIVE Go values by
 * `api/contract_export_test.go`. This file asserts web's registry against it, so a
 * backend change that web hasn't followed fails here instead of in production.
 *
 * Assertions use the named `KAITU` / `OVERLEAP` exports rather than `siteBrand()`:
 * `siteBrand()` reads `NEXT_PUBLIC_BRAND`, so under it this suite would only ever
 * check the one brand the current build bakes. The gate must hold for both.
 */
import { describe, it, expect } from 'vitest';
import { readFileSync } from 'fs';
import path from 'path';
import * as brandRegistry from '../src/lib/brands';
import type { Brand } from '../src/lib/brands';
import { ErrorCode } from '../src/lib/api';

const CONTRACT_PATH = path.resolve(__dirname, '../../contracts/api-contract.json');
const REGEN = 'cd api && UPDATE_CONTRACT=1 go test -run TestExportContract ./...';

type Contract = {
  brands: Record<string, { id: string; hosts: string[]; supportEmail: string }>;
  errorCodes: { name: string; code: number }[];
};

/**
 * Hard-fail — never `skip` — when the contract is absent. A gate that quietly
 * disappears when its input is missing is worse than no gate: this repo has
 * already been burned by a check that went green locally and exploded on a fresh
 * CI clone. If this throws, the contract wasn't committed or the path moved.
 */
function loadContract(): Contract {
  try {
    return JSON.parse(readFileSync(CONTRACT_PATH, 'utf8')) as Contract;
  } catch (e) {
    throw new Error(
      `Cross-layer contract not readable at ${CONTRACT_PATH}\n` +
        `This gate cannot be skipped. Regenerate with:\n  ${REGEN}\n` +
        `Underlying error: ${(e as Error).message}`,
    );
  }
}

const contract = loadContract();

/**
 * Discover brands from the module's exports rather than listing them, so a brand
 * added to the registry is picked up by this gate automatically instead of
 * silently sitting outside it.
 */
const WEB_BRANDS: Brand[] = Object.values(brandRegistry).filter(
  (v): v is Brand =>
    typeof v === 'object' &&
    v !== null &&
    typeof (v as Brand).id === 'string' &&
    typeof (v as Brand).baseUrl === 'string' &&
    typeof (v as Brand).contactEmail === 'string',
);

describe('brand registry ↔ Go contract', () => {
  it('discovery found the named registry exports (guard on the guard)', () => {
    // If this fails the filter above stopped matching Brand objects and every
    // per-brand assertion below would vacuously pass over an empty list.
    expect(WEB_BRANDS).toContain(brandRegistry.KAITU);
    expect(WEB_BRANDS).toContain(brandRegistry.OVERLEAP);
  });

  it('brand sets are identical', () => {
    const webIds = WEB_BRANDS.map((b) => b.id).sort();
    const goIds = Object.keys(contract.brands).sort();
    expect(webIds).toEqual(goIds);
  });

  describe.each(WEB_BRANDS.map((b) => [b.id, b] as const))('%s', (id, brand) => {
    it('baseUrl host is one the backend attributes to this brand', () => {
      // NOT string equality against the Go baseURL. web links to the apex
      // (kaitu.io) while api/webapp use www — both are registered hosts, and that
      // drift is legitimate. The invariant that actually matters is membership:
      // a link web hands a user must land on a host the backend maps back to THIS
      // brand. A host outside `hosts` resolves to the default (kaitu) or nothing.
      const goBrand = contract.brands[id];
      expect(goBrand, `Go contract has no brand '${id}'`).toBeDefined();
      expect(goBrand.hosts).toContain(new URL(brand.baseUrl).hostname);
    });

    it('contactEmail matches the Go supportEmail verbatim', () => {
      // No legitimate drift here: a mismatch means users are told to write to an
      // address the backend doesn't consider this brand's support channel.
      expect(brand.contactEmail).toBe(contract.brands[id].supportEmail);
    });

    it('id is a brand key the backend will accept in X-K2-Brand', () => {
      // web injects `X-K2-Brand` in src/lib/api.ts and src/middleware.ts from
      // this id. `resolveRequestBrand` silently falls back to kaitu on an
      // unknown value — an overleap deployment would serve kaitu data with no
      // error raised anywhere. So the id must be a live registry key.
      expect(Object.keys(contract.brands)).toContain(brand.id);
    });
  });
});

describe('error codes ↔ Go contract', () => {
  const goCodes = new Set(contract.errorCodes.map((e) => e.code));

  it('every web ErrorCode value exists in the Go contract', () => {
    // One-directional on purpose. web mirrors only the subset of backend codes
    // its surfaces handle, so contract ⊄ web is normal and not asserted. The
    // reverse — a web code the backend never emits — is a ghost: either the
    // backend deleted it (dead branch in web) or the number was invented.
    const ghosts = Object.entries(ErrorCode)
      .filter(([, code]) => !goCodes.has(code))
      .map(([name, code]) => `${name}=${code}`);
    expect(ghosts, `web ErrorCode values absent from the Go contract. Regenerate (${REGEN}) or fix web.`).toEqual([]);
  });
});
