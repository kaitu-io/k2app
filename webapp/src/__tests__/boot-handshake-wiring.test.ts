/**
 * Static wiring guard for the web-OTA boot handshake.
 *
 * The handshake only protects anything if main.tsx calls it AFTER
 * ReactDOM.render — that ordering is the entire fix, and it lives in the entry
 * module, which no unit test exercises. So assert on the source, the same way
 * bridge-contract.test.ts pins the native mirrors.
 *
 * What this catches: someone "tidying" the boot sequence by hoisting the
 * confirm call up next to the bridge injection, or dropping a platform's call
 * entirely. Both silently re-open the white-screen-can't-roll-back hole; both
 * turn this red.
 */
import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const MAIN = readFileSync(resolve(__dirname, '../main.tsx'), 'utf8');

/** Index of the ReactDOM render call — the line the confirms must follow. */
function renderIndex(src: string): number {
  const i = src.indexOf('ReactDOM.createRoot');
  expect(i, 'ReactDOM.createRoot not found in main.tsx — update this guard').toBeGreaterThan(-1);
  return i;
}

describe('web OTA boot handshake wiring (main.tsx)', () => {
  it('confirms desktop boot only after ReactDOM has rendered', () => {
    const call = MAIN.indexOf('confirmUiBootOk(');
    expect(call, 'main.tsx never calls confirmUiBootOk()').toBeGreaterThan(-1);
    expect(
      call,
      'confirmUiBootOk() must be called AFTER ReactDOM.createRoot — confirming ' +
        'before render clears the rollback marker for a bundle that never rendered',
    ).toBeGreaterThan(renderIndex(MAIN));
  });

  it('confirms mobile boot only after ReactDOM has rendered', () => {
    const call = MAIN.indexOf('confirmWebBootOk(');
    expect(call, 'main.tsx never calls confirmWebBootOk() — mobile web OTA cannot roll back').toBeGreaterThan(-1);
    expect(
      call,
      'confirmWebBootOk() must be called AFTER ReactDOM.createRoot — mobile has ' +
        'no hot-fix path, so a defeated rollback strands users on a white screen',
    ).toBeGreaterThan(renderIndex(MAIN));
  });

  // The Linux shell is a Go daemon serving over HTTP; its confirmation is a
  // POST, but the ordering requirement is identical.
  it('confirms Linux boot only after ReactDOM has rendered', () => {
    const call = MAIN.indexOf('confirmLinuxBootOk(');
    expect(call, 'main.tsx never calls confirmLinuxBootOk() — Linux web OTA cannot roll back').toBeGreaterThan(-1);
    expect(
      call,
      'confirmLinuxBootOk() must be called AFTER ReactDOM.createRoot — the daemon ' +
        'would clear .boot-pending for a bundle that never rendered',
    ).toBeGreaterThan(renderIndex(MAIN));
  });

  // The bridge-init call sites are where the marker used to be cleared. Neither
  // injector may confirm the boot.
  it('neither bridge injector confirms the boot', () => {
    const injectDesktop = MAIN.indexOf('injectTauriGlobals(');
    const injectMobile = MAIN.indexOf('injectCapacitorGlobals(');
    const render = renderIndex(MAIN);
    expect(injectDesktop).toBeGreaterThan(-1);
    expect(injectMobile).toBeGreaterThan(-1);
    expect(injectDesktop, 'bridge injection must precede render').toBeLessThan(render);
    expect(injectMobile, 'bridge injection must precede render').toBeLessThan(render);
  });
});
