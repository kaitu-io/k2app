/**
 * Cross-layer gate: this site ↔ Center API (Go).
 * contracts/api-contract.json is exported from live Go values
 * (cd api && UPDATE_CONTRACT=1 go test -count=1 -run TestExportContract ./...).
 * The invariant is host ownership, not string equality.
 */
import { describe, it, expect } from 'vitest';
import { readFileSync } from 'fs';
import path from 'path';
import { SITE } from '@/lib/site';
import { ErrorCode } from '@/lib/api';

type Contract = {
  brands: Record<string, { id: string; hosts: string[]; supportEmail: string }>;
  errorCodes: { name: string; code: number }[];
};

// Hard-fail (never skip) when the contract is missing: a gate that disappears
// when its input is absent is worse than none.
const contract = JSON.parse(
  readFileSync(path.resolve(__dirname, '../../../contracts/api-contract.json'), 'utf8'),
) as Contract;

describe('cross-layer contract', () => {
  const brand = contract.brands[SITE.brandId];

  it('the X-K2-Brand value is a brand the API knows', () => {
    expect(brand?.id).toBe(SITE.brandId);
  });

  it('baseUrl host belongs to this brand', () => {
    expect(brand.hosts).toContain(new URL(SITE.baseUrl).hostname);
  });

  it('support email matches the API', () => {
    expect(SITE.contactEmail).toBe(brand.supportEmail);
  });

  it('every error code the client reacts to exists in the API', () => {
    const known = new Set(contract.errorCodes.map((e) => e.code));
    for (const [name, code] of Object.entries(ErrorCode)) {
      expect(known.has(code), `${name}=${code} not in contract`).toBe(true);
    }
  });
});
