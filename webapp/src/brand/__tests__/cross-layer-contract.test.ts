/**
 * Cross-layer contract gate: webapp ⇄ Center API (Go).
 *
 * WHY THIS EXISTS
 * ---------------
 * The "brand" concept is declared three times — api/brand.go (Go),
 * webapp/src/brand/*.ts, web/src/lib/brands.ts — and nothing used to compare
 * them. That gap shipped a real outage: `X-K2-Brand` was added to every webapp
 * request but never added to the backend's CORS allow-list, so every
 * browser-context client's direct transport failed preflight and users saw a
 * bare `code:-1`. Go tests don't run a browser; webapp tests mock fetch — so
 * NO test crossed the header contract's runtime surface.
 *
 * This file is that crossing. It reads `contracts/api-contract.json`, which is
 * GENERATED from live Go values (brand registry structs, CORS allow-headers
 * harvested from a real middleware response, error codes parsed out of
 * response.go via go/ast). Both sides of every assertion below are real values:
 * the Go side comes from the contract, the webapp side comes from importing the
 * actual registry modules and from driving the actual `cloudApi` code paths.
 *
 * Hand-copied lists are banned here. A mirror of a mirror catches nothing.
 *
 * Brand-adaptive: asserts against the `KAITU_BRAND` / `OVERLEAP_BRAND` named
 * imports (never `brandConfig`, which changes with the baked brand), so this
 * suite is green under both `vitest run` and `K2_BRAND=overleap vitest run`.
 *
 * Run:
 *   cd webapp && npx vitest run src/brand/__tests__/cross-layer-contract.test.ts
 *   cd webapp && K2_BRAND=overleap npx vitest run src/brand/__tests__/cross-layer-contract.test.ts
 */
import { existsSync, readFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

import { describe, it, expect, vi, beforeEach } from 'vitest';

import { KAITU_BRAND } from '../kaitu';
import { OVERLEAP_BRAND } from '../overleap';
import type { WebappBrandConfig } from '../types';
import { allowedEmbedOrigins } from '../../utils/embed-origins';
import { ERROR_CODES } from '../../utils/errorCode';

// ---------------------------------------------------------------------------
// Mocks for the cloud-api dependency graph (§B drives the real request path).
// Declared before the `cloud-api` import; vi.mock is hoisted above imports.
// ---------------------------------------------------------------------------

vi.mock('../../services/auth-service', () => ({
  authService: {
    getToken: vi.fn(),
    getRefreshToken: vi.fn(),
    setTokens: vi.fn(),
    clearTokens: vi.fn(),
    getTokenEpoch: vi.fn().mockReturnValue(0),
  },
}));

vi.mock('../../stores/auth.store', () => ({
  useAuthStore: { setState: vi.fn(), getState: vi.fn(() => ({ isAuthenticated: true })) },
}));

vi.mock('../../services/cache-store', () => ({
  cacheStore: { clear: vi.fn(), get: vi.fn(), set: vi.fn(), delete: vi.fn() },
}));

vi.mock('../../stores/login-dialog.store', () => ({
  useLoginDialogStore: { getState: () => ({ open: vi.fn() }) },
}));

vi.mock('../../i18n/i18n', () => ({ default: { t: (key: string) => key } }));

vi.mock('../../services/entry-pool', () => ({ addNodes: vi.fn() }));

// The transport boundary. cloud-api hands it the fully-built headers object and
// resolve-and-fetch forwards it verbatim to fetch()/relay (`headers: req.headers`
// in both tryDirect and tryRelay), so capturing here captures exactly what goes
// on the wire — and therefore exactly what CORS preflight will be asked to allow.
vi.mock('../../services/resolve-and-fetch', () => ({
  resolveAndFetch: vi.fn(),
  CONTROL_PLANE_HOST: 'k2.52j.me',
}));

import { cloudApi } from '../../services/cloud-api';
import { authService } from '../../services/auth-service';
import { resolveAndFetch } from '../../services/resolve-and-fetch';

// ---------------------------------------------------------------------------
// Contract loading — hard failure, never skip.
// ---------------------------------------------------------------------------

interface ContractBrand {
  id: string;
  displayName: string;
  hosts: string[];
  webOrigins: string[];
  redirectRootDomain: string;
  baseURL: string;
  supportEmail: string;
  edmFromName: string;
  paymentChannels: string[];
}

interface ApiContract {
  brands: Record<string, ContractBrand>;
  cors: { api: { allowHeaders: string[] }; app: { allowHeaders: string[] } };
  errorCodes: { name: string; code: number }[];
}

const REGENERATE_CMD =
  'cd api && UPDATE_CONTRACT=1 go test -run TestExportContract ./...';

/**
 * Read the generated contract. A missing file is a HARD failure, never a skip:
 * a skipped gate is an absent gate, and this repo has already been burned by a
 * guard that silently evaporated (gitignored artifact → green locally, blind CI).
 * If this throws, the contract wasn't generated or was deleted — regenerate it.
 */
function loadContract(): ApiContract {
  const here = path.dirname(fileURLToPath(import.meta.url));
  // webapp/src/brand/__tests__ → repo root
  const contractPath = path.resolve(here, '../../../../contracts/api-contract.json');
  if (!existsSync(contractPath)) {
    throw new Error(
      `Cross-layer contract missing at ${contractPath}.\n` +
        `This gate cannot run without it and MUST NOT be skipped.\n` +
        `Regenerate with:\n  ${REGENERATE_CMD}`
    );
  }
  return JSON.parse(readFileSync(contractPath, 'utf-8')) as ApiContract;
}

const contract = loadContract();

/** The webapp's brand registry, keyed by id — the webapp side of every §A assertion. */
const WEBAPP_BRANDS: Record<string, WebappBrandConfig> = {
  [KAITU_BRAND.id]: KAITU_BRAND,
  [OVERLEAP_BRAND.id]: OVERLEAP_BRAND,
};

const brandIds = Object.keys(contract.brands);

// ---------------------------------------------------------------------------
// A. Brand registry contract
// ---------------------------------------------------------------------------

describe('cross-layer contract: brand registry (webapp ⇄ api/brand.go)', () => {
  it('contract file is present and well-formed', () => {
    expect(contract.brands, `contract.brands missing — regenerate: ${REGENERATE_CMD}`).toBeTruthy();
    expect(contract.cors?.api?.allowHeaders).toBeInstanceOf(Array);
    expect(contract.errorCodes.length).toBeGreaterThan(0);
  });

  it('brand id sets are identical across layers', () => {
    // Set equality, order-independent. A brand present in only one layer means
    // one side can mint accounts/hosts the other has never heard of.
    expect(new Set(brandIds)).toEqual(new Set(Object.keys(WEBAPP_BRANDS)));
  });

  describe.each(brandIds)('brand: %s', (id) => {
    const backend = contract.brands[id];
    const webapp = WEBAPP_BRANDS[id];

    it('baseURL hostname is owned by this brand on the backend', () => {
      // NOT string equality: api resolves `www.kaitu.io` while web serves the
      // apex `kaitu.io`, and BOTH are legitimately in `hosts`. The invariant that
      // actually matters is ownership — a link the webapp hands a user must land
      // on a host the backend attributes to THIS brand. A hostname outside
      // `hosts` is either unrecognised (→ default brand) or, worse, owned by the
      // peer brand (→ silent cross-brand traffic, then 403003).
      const hostname = new URL(webapp.baseURL).hostname;
      expect(
        backend.hosts,
        `${id}: baseURL host "${hostname}" is not in the backend's host list for this brand ` +
          `(${backend.hosts.join(', ')}) — requests to it resolve to a different brand`
      ).toContain(hostname);
    });

    it('every origin trusted for iframe postMessage is owned by this brand on the backend', () => {
      // Drives the REAL production helper (Discover/Changelog call it on the
      // live iframe URL) rather than a hand-listed mirror of its output: the
      // apex/www sibling it derives must be brand-owned too, or an iframe
      // postMessage could be trusted from a host the backend attributes to the
      // peer brand. Only the default baseURL is checkable here — an
      // appConfig-issued baseURL is runtime state, not contract.
      const origins = [...allowedEmbedOrigins(webapp.baseURL)];
      expect(
        origins.length,
        `${id}: allowedEmbedOrigins("${webapp.baseURL}") derived nothing — the helper rejected ` +
          `the brand's own baseURL, so this assertion would pass over an empty list`
      ).toBeGreaterThan(0);

      for (const origin of origins) {
        const hostname = new URL(origin).hostname;
        expect(
          backend.hosts,
          `${id}: trusted embed origin "${origin}" resolves to host "${hostname}", which the ` +
            `backend does not attribute to this brand (${backend.hosts.join(', ')}) — iframe ` +
            `postMessage would trust an origin the backend treats as another brand`
        ).toContain(hostname);
      }
    });

    it('supportEmail matches the backend verbatim', () => {
      // No legitimate drift here, unlike hosts: both layers print the same
      // address to the same user.
      expect(webapp.supportEmail).toBe(backend.supportEmail);
    });

    it('wordgatePurchase gate matches the backend payment-channel allow-list', () => {
      // Both directions are real bugs, hence `===`:
      //   gate on / backend off  → user taps Buy, eats 405001, dead funnel.
      //   gate off / backend on  → brand can never take money.
      const backendAllows = backend.paymentChannels.includes('wordgate');
      expect(
        webapp.features.wordgatePurchase,
        `${id}: webapp wordgatePurchase=${webapp.features.wordgatePurchase} but backend ` +
          `paymentChannels=[${backend.paymentChannels.join(', ')}]`
      ).toBe(backendAllows);
    });

    it('stripeCheckout gate matches the backend payment-channel allow-list', () => {
      const backendAllows = backend.paymentChannels.includes('stripe');
      expect(
        webapp.features.stripeCheckout,
        `${id}: webapp stripeCheckout=${webapp.features.stripeCheckout} but backend ` +
          `paymentChannels=[${backend.paymentChannels.join(', ')}]`
      ).toBe(backendAllows);
    });
  });
});

// ---------------------------------------------------------------------------
// A6. Error-code mirror
// ---------------------------------------------------------------------------

/**
 * Backend codes the webapp is structurally incapable of receiving.
 *
 * Every entry needs a verified reason — "webapp doesn't use it today" is NOT a
 * reason (400007-400011 aren't called by webapp either, yet they belong: the
 * webapp could redeem tomorrow and the constitution wants the mapping ready).
 * The bar is UNREACHABLE: no webapp-reachable route can emit it.
 */
const EXEMPT_CODES: { code: number; name: string; reason: string }[] = [
  {
    code: 202,
    name: 'ErrorPendingApproval',
    // Verified 2026-07-15: `PendingApproval()` (api/response.go:152) has 13
    // callers and every one lives in an `api_admin_*.go` handler mounted under
    // the `/app/*` admin route group (api/route.go opsAdmin, RoleRequired).
    // The webapp calls `/api/*` exclusively — a repo-wide grep for `/app/` paths
    // in webapp/src returns zero hits — and `/app/*` is served by a different
    // CORS middleware entirely. The admin approval surface is web/'s /manager
    // dashboard plus the MCP server, neither of which uses this error table.
    reason:
      'admin-only: emitted solely by /app/* admin approval handlers; webapp calls /api/* only',
  },
];

describe('cross-layer contract: error codes (webapp ⇄ api/response.go)', () => {
  const knownCodes = new Set<number>(Object.values(ERROR_CODES));
  const exemptByCode = new Map(EXEMPT_CODES.map((e) => [e.code, e]));

  it.each(contract.errorCodes.map((e) => [e.name, e.code] as const))(
    '%s (%i) is mirrored in ERROR_CODES or explicitly exempt',
    (name, code) => {
      if (exemptByCode.has(code)) return; // exhaustiveness of exemptions checked below
      expect(
        knownCodes.has(code),
        `Backend error code ${name}=${code} has no entry in webapp ERROR_CODES.\n` +
          `Per webapp/CLAUDE.md "API Error Code Constitution", add:\n` +
          `  1. a constant in utils/errorCode.ts\n` +
          `  2. a case in getErrorMessage() with an i18n key\n` +
          `  3. translations in all 7 locales\n` +
          `If the webapp genuinely cannot receive it, add it to EXEMPT_CODES with a verified reason.`
      ).toBe(true);
    }
  );

  // Anti-rot: an exemption that stopped being true must not linger. If a code
  // gets mirrored later, this fails and tells you to delete the stale entry —
  // otherwise the list slowly becomes a graveyard nobody trusts.
  it.each(EXEMPT_CODES.map((e) => [e.name, e.code] as const))(
    'exemption for %s (%i) is still warranted',
    (name, code) => {
      expect(
        knownCodes.has(code),
        `${name}=${code} is listed in EXEMPT_CODES but IS now present in ERROR_CODES. ` +
          `The exemption is stale — remove it from EXEMPT_CODES so the code is gated normally.`
      ).toBe(false);
    }
  );

  it('every exempt code still exists in the backend contract', () => {
    const contractCodes = new Set(contract.errorCodes.map((e) => e.code));
    for (const { name, code } of EXEMPT_CODES) {
      expect(
        contractCodes.has(code),
        `${name}=${code} is exempt but no longer exists in the backend contract — delete the exemption.`
      ).toBe(true);
    }
  });
});

// ---------------------------------------------------------------------------
// B. Header runtime contract — the CORS-outage gate
// ---------------------------------------------------------------------------

describe('cross-layer contract: request headers ⇄ backend CORS allow-list', () => {
  /** Header names actually built by cloudApi, harvested at the transport boundary. */
  const sentHeaders = new Set<string>();

  beforeEach(() => {
    // vi.clearAllMocks() elsewhere wipes implementations, not just call counts —
    // re-arm every mock we depend on, every time.
    vi.mocked(authService.getTokenEpoch).mockReturnValue(0);
    vi.mocked(authService.setTokens).mockResolvedValue(undefined as never);
    vi.mocked(authService.clearTokens).mockResolvedValue(undefined as never);
    vi.mocked(resolveAndFetch).mockImplementation(async (req: { headers: Record<string, string> }) => {
      for (const name of Object.keys(req.headers)) sentHeaders.add(name);
      return {
        transport: 'ok' as const,
        status: 200,
        json: async () => ({ code: 0, data: { token: 'new-access', refreshToken: 'new-refresh' } }),
      };
    });
  });

  /**
   * Drive the REAL code paths. Nothing here enumerates header names — the set is
   * whatever cloud-api.ts happens to construct today, which is the entire point:
   * a header added to cloud-api.ts tomorrow is picked up with no test edit, and
   * fails loudly if the backend wasn't told about it.
   */
  async function exerciseAllRequestPaths(): Promise<void> {
    // Authenticated request(): Content-Type + X-K2-Brand + Authorization + X-K2-Client
    (window as unknown as { _platform?: unknown })._platform = {
      os: 'macos',
      version: '0.4.5',
      arch: 'arm64',
    };
    vi.mocked(authService.getToken).mockResolvedValue('access-token');
    await cloudApi.request('POST', '/api/user/info', { hello: 'world' });

    // Gateway/router client class — different X-K2-Client product token, same header name.
    (window as unknown as { _platform?: unknown })._platform = {
      os: 'linux',
      version: '0.4.5',
      arch: 'mips',
      platformType: 'gateway',
    };
    await cloudApi.request('GET', '/api/tunnels');

    // Unauthenticated request(): no Authorization branch.
    vi.mocked(authService.getToken).mockResolvedValue(null as never);
    await cloudApi.request('GET', '/api/tunnels');

    // Unauthenticated + no _platform: the minimal header set.
    delete (window as unknown as { _platform?: unknown })._platform;
    await cloudApi.request('POST', '/api/auth/login', { email: 'a@b.c' });

    // The refresh path builds its own headers object — a separate origination
    // site in cloud-api.ts, and therefore a separate way to drift.
    (window as unknown as { _platform?: unknown })._platform = {
      os: 'windows',
      version: '0.4.5',
      arch: 'x86_64',
    };
    await cloudApi._doRefresh('refresh-token');

    delete (window as unknown as { _platform?: unknown })._platform;
  }

  it('every header cloudApi sends is in the backend /api/* CORS allow-list', async () => {
    await exerciseAllRequestPaths();

    // Liveness guard, NOT the contract assertion: proves the probe actually ran
    // the real code instead of silently capturing nothing. A trivially-empty set
    // would otherwise satisfy the subset check below and the gate would be a no-op.
    expect(
      sentHeaders.size,
      'no headers captured — the resolveAndFetch mock did not observe a real cloudApi request'
    ).toBeGreaterThan(0);
    expect(sentHeaders.has('Authorization')).toBe(true);
    expect(sentHeaders.has('X-K2-Client')).toBe(true);

    // HTTP header names are case-insensitive; compare on that basis so a rename
    // to `x-k2-brand` doesn't produce a bogus failure.
    const allowed = new Set(contract.cors.api.allowHeaders.map((h) => h.toLowerCase()));
    const offenders = [...sentHeaders].filter((h) => !allowed.has(h.toLowerCase()));

    expect(
      offenders,
      offenders
        .map(
          (h) =>
            `${h} is sent by cloudApi but missing from the backend CORS allow-list — every ` +
            `browser-context client's direct transport will fail preflight and surface as a bare ` +
            `code:-1 (this is exactly the X-K2-Brand outage). Add it to the api CORS AllowHeaders ` +
            `in api/, then regenerate: ${REGENERATE_CMD}`
        )
        .join('\n')
    ).toEqual([]);
  });

  it('X-K2-Brand specifically is allowed (regression: the outage that motivated this file)', () => {
    const allowed = contract.cors.api.allowHeaders.map((h) => h.toLowerCase());
    expect(allowed).toContain('x-k2-brand');
  });
});
