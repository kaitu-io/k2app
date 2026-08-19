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
 * BLIND SPOT — read this before trusting a green CI run
 * ----------------------------------------------------
 * `k2/` is a private git submodule. The webapp CI job checks out the repo with
 * `actions/checkout@v4` and NO submodule init (.github/workflows/
 * test-webapp-reusable.yml), so `k2/engine/error.go` does not exist there and
 * these assertions are SKIPPED in CI. They run on developer machines and in any
 * job that inits the submodule. Concretely: a commit that changes a code value
 * on both sides incorrectly, pushed without a local test run, reaches main.
 * The always-running half of the gate is `utils/__tests__/errorCatalog.test.ts`
 * — it still fails if a declared code has no copy, just not if the *value* is
 * wrong.
 */
import { describe, it, expect } from 'vitest';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { ENGINE_ERROR_CODES } from '../../utils/errorCatalog';

const here = path.dirname(fileURLToPath(import.meta.url));
// webapp/src/services/__tests__ -> repo root
const GO_FILE = path.resolve(here, '../../../../k2/engine/error.go');
const available = fs.existsSync(GO_FILE);

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

describe.skipIf(!available)('ENGINE_ERROR_CODES mirrors k2/engine/error.go', () => {
  const goCodes = available ? parseGoCodes(fs.readFileSync(GO_FILE, 'utf8')) : {};

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

describe('k2 cross-check availability', () => {
  it('records whether the submodule was present for this run', () => {
    // Not an assertion on `available` — CI legitimately runs without the
    // submodule. This exists so the reason a check did not run is visible in
    // the report rather than inferred from a silent skip.
    expect(typeof available).toBe('boolean');
    if (!available) {
      // eslint-disable-next-line no-console
      console.warn(
        `[k2-engine-codes] SKIPPED the Go cross-check: ${GO_FILE} not found ` +
          '(k2 submodule not initialised). Run `git submodule update --init k2` to enable it.'
      );
    }
  });
});
