/**
 * Cross-check: the TypeScript mirror of the k2 engine error codes must equal
 * the Go declaration it claims to mirror.
 *
 * This test exists because the constants were hand-copied and drifted: the
 * webapp carried `ErrCodeTimeout = 408` while `k2/engine/error.go` has said
 * `ErrCodeTimeout = 108` since 2026-08-02. Nothing failed — the wrong number
 * simply routed timeouts to "unknown error" and made isNetworkError() lie.
 * The fix for a hand-copied constant is not a better hand-copy; it is a
 * machine-checked one.
 *
 * FAIL-CLOSED — a missing Go file is a failure, never a skip
 * ----------------------------------------------------------
 * `k2/` is a private git submodule. Until 0.4.8 the webapp CI job checked the
 * repo out WITHOUT it, and this suite used `describe.skipIf(!available)` — so
 * the gate had never once executed in CI, while every run reported green. A
 * skipped gate is an absent gate (same lesson as brands/__tests__/
 * cross-layer-contract.test.ts). test-webapp-reusable.yml now inits `k2`
 * before running vitest; if the file is still missing, the job is
 * misconfigured and this file throws at load so the run is red, not quiet.
 * Locally: `git submodule update --init k2`.
 */
import { describe, it, expect } from 'vitest';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { ENGINE_ERROR_CODES } from '../../utils/errorCatalog';

const here = path.dirname(fileURLToPath(import.meta.url));
// webapp/src/services/__tests__ -> repo root
const GO_FILE = path.resolve(here, '../../../../k2/engine/error.go');
if (!fs.existsSync(GO_FILE)) {
  throw new Error(
    `k2 engine error-code gate cannot run: ${GO_FILE} not found.\n` +
      'This gate MUST NOT be skipped — init the submodule: git submodule update --init k2'
  );
}

/** Extract `ErrCodeXxx = NNN` from the const block in k2/engine/error.go. */
function parseGoCodes(src: string): Record<string, number> {
  const block = src.match(/const \(\n([\s\S]*?)\n\)/);
  if (!block) throw new Error('k2/engine/error.go: could not locate the first const block');
  const out: Record<string, number> = {};
  for (const line of block[1].split('\n')) {
    const m = line.match(/^\s*(ErrCode\w+)\s*=\s*(\d+)/);
    if (m) out[m[1]] = Number(m[2]);
  }
  if (Object.keys(out).length === 0) throw new Error('k2/engine/error.go: no ErrCode constants parsed');
  return out;
}

describe('ENGINE_ERROR_CODES mirrors k2/engine/error.go', () => {
  const goCodes = parseGoCodes(fs.readFileSync(GO_FILE, 'utf8'));

  it('parsed a plausible number of Go constants (guards the parser itself)', () => {
    // A regex that silently matches nothing would make every assertion below
    // vacuously true.
    expect(Object.keys(goCodes).length).toBeGreaterThanOrEqual(10);
    expect(goCodes.ErrCodeTimeout).toBe(108);
  });

  it('has exactly the same constant names as the Go const block', () => {
    expect(Object.keys(ENGINE_ERROR_CODES).sort()).toEqual(Object.keys(goCodes).sort());
  });

  it('has the same numeric value for every constant', () => {
    expect(ENGINE_ERROR_CODES).toEqual(goCodes);
  });
});
