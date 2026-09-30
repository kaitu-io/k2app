/**
 * Center API client — only the endpoints this site calls.
 *
 * Origin: a trimmed copy of web/src/lib/api.ts. The auth transport (HttpOnly
 * cookie + CSRF header, localStorage Bearer fallback when the browser drops the
 * Set-Cookie) is security-relevant: a fix to it on either site must be checked
 * on the other (sites/overleap/CLAUDE.md).
 *
 * Envelope: HTTP 200 always; `code` 0 = success; `message` is backend debug
 * text and is never shown to users — map `code` through lib/api-errors.ts.
 */
import { appEvents } from './events';
import { safeStorage } from './safeStorage';
import { SITE } from './site';

interface Envelope<T> {
  code: number;
  message?: string;
  data?: T;
}

export interface ListResult<T> {
  items: T[];
}

// Error codes the site reacts to (subset of api/response.go; every value is
// checked against contracts/api-contract.json by tests/cross-layer-contract.test.ts).
export const ErrorCode = {
  NotLogin: 401,
  Forbidden: 403,
  NotFound: 404,
  InvalidArgument: 422,
  TooManyRequests: 429,
  SystemError: 500,
  ServiceUnavailable: 503,
  InvalidVerificationCode: 400003,
  InvalidCredentials: 400006,
  VerificationCodeExpired: 400013,
  ChannelUnavailable: 405001,
} as const;

export class ApiError extends Error {
  readonly code: number;
  constructor(code: number, message: string) {
    super(message);
    this.name = 'ApiError';
    this.code = code;
  }
}

/** Network failure / non-200 / malformed body — no Center code available. */
export class TransportError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'TransportError';
  }
}

// ---- Types (Go api Data* shapes, camelCase at the JSON boundary) ----------

export interface AuthUser {
  id: number;
  email: string;
  hasPassword: boolean;
}

export interface WebLoginResponse {
  user: AuthUser;
  /** Also returned in the body so we can fall back to a Bearer header when the
   *  browser fails to persist the HttpOnly cookie from the same response. */
  accessToken: string;
}

export interface ManageSurface {
  kind: 'stripe_portal' | 'apple_settings' | 'url';
  url?: string;
}

export interface DataSubscription {
  provider: string;
  tier: string;
  currentPeriodEnd: number; // unix seconds
  autoRenew: boolean;
  manage: ManageSurface;
}

/** GET /api/user/info (Go DataUser, trimmed). Note: it has no top-level email —
 *  use profileEmail(). */
export interface UserProfile {
  uuid: string;
  hasPassword: boolean;
  loginIdentifies?: { type: string; value: string }[];
  subscriptions?: DataSubscription[];
}

export function profileEmail(p: UserProfile | null): string {
  return p?.loginIdentifies?.find((i) => i.type === 'email')?.value ?? '';
}

export interface Plan {
  pid: string;
  label: string;
  price: number; // USD minor units
  originPrice: number;
  month: number;
  highlight: boolean;
  product?: string;
  /** Currency (lower-case) → minor units; present for Stripe plans when Stripe is reachable. */
  currencyPrices?: Record<string, number>;
}

interface RequestOptions extends RequestInit {
  /** On 401: clear auth state and send the visitor to /login (default true). */
  redirectOn401?: boolean;
}

const FALLBACK_TOKEN_KEY = 'auth_fallback_token';

function csrfToken(): string | null {
  if (typeof document === 'undefined') return null;
  const m = document.cookie.match(/(?:^|;\s*)csrf_token=([^;]+)/);
  return m ? m[1] : null;
}

async function hasCookieAuth(maxMs = 200): Promise<boolean> {
  if (typeof document === 'undefined') return false;
  const start = Date.now();
  while (Date.now() - start < maxMs) {
    if (csrfToken()) return true;
    await new Promise((r) => setTimeout(r, 20));
  }
  return !!csrfToken();
}

function goToLogin(): void {
  if (typeof window === 'undefined') return;
  const [, maybeLocale, ...rest] = window.location.pathname.split('/');
  const locale = maybeLocale || '';
  const next = `/${rest.join('/')}${window.location.search}`;
  window.location.href = `/${locale}/login?next=${encodeURIComponent(next)}`;
}

async function request<T>(path: string, { redirectOn401 = true, ...init }: RequestOptions = {}): Promise<T> {
  const method = (init.method ?? 'GET').toUpperCase();
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    'X-K2-Brand': SITE.brandId,
  };
  const fallback = safeStorage.get(FALLBACK_TOKEN_KEY);
  if (fallback) headers.Authorization = `Bearer ${fallback}`;
  if (method !== 'GET') {
    const csrf = csrfToken();
    if (csrf) headers['X-CSRF-Token'] = csrf;
  }

  let res: Response;
  try {
    res = await fetch(path, { ...init, headers: { ...headers, ...(init.headers as Record<string, string>) }, credentials: 'include' });
  } catch {
    throw new TransportError('network');
  }
  if (!res.ok) throw new TransportError(`http ${res.status}`);
  if (res.status === 204 || res.headers.get('Content-Length') === '0') return {} as T;

  let body: Envelope<T>;
  try {
    body = await res.json();
  } catch {
    throw new TransportError('malformed');
  }

  if (body.code !== 0) {
    if (body.code === ErrorCode.NotLogin) {
      safeStorage.remove(FALLBACK_TOKEN_KEY);
      appEvents.emit('auth:unauthorized');
      if (redirectOn401) goToLogin();
    }
    throw new ApiError(body.code, body.message ?? '');
  }
  return (body.data ?? {}) as T;
}

const post = (body: unknown): RequestInit => ({ method: 'POST', body: JSON.stringify(body) });

export const api = {
  sendCode(email: string, language: string) {
    return request<{ userExists: boolean; isActivated: boolean }>('/api/auth/code', {
      ...post({ email, language }),
      redirectOn401: false,
    });
  },

  webLogin(email: string, verificationCode: string, language: string) {
    return request<WebLoginResponse>('/api/auth/web-login', {
      ...post({ email, verificationCode, language }),
      redirectOn401: false,
    });
  },

  passwordLogin(email: string, password: string, language: string) {
    return request<WebLoginResponse>('/api/auth/web-login/password', {
      ...post({ email, password, language }),
      redirectOn401: false,
    });
  },

  async logout(): Promise<void> {
    try {
      await request<void>('/api/auth/logout', { method: 'POST', redirectOn401: false });
    } catch {
      /* cookies may already be gone; local state is cleared regardless */
    }
    safeStorage.remove(FALLBACK_TOKEN_KEY);
    appEvents.emit('auth:unauthorized');
  },

  /** After a successful login: prefer the HttpOnly cookie; keep the body token
   *  as a Bearer fallback only when the browser dropped the Set-Cookie. */
  async applyLoginCredentials(accessToken: string): Promise<void> {
    if (await hasCookieAuth()) {
      safeStorage.remove(FALLBACK_TOKEN_KEY);
      return;
    }
    if (accessToken) safeStorage.set(FALLBACK_TOKEN_KEY, accessToken);
  },

  getUserProfile(opts: { redirectOn401?: boolean } = {}) {
    return request<UserProfile>('/api/user/info', opts);
  },

  setPassword(password: string, confirmPassword: string) {
    return request<void>('/api/user/password', post({ password, confirmPassword }));
  },

  getPlans() {
    return request<ListResult<Plan>>('/api/plans', { redirectOn401: false });
  },

  createStripeCheckout(planPid: string) {
    return request<{ url: string }>('/api/user/stripe/checkout', post({ plan: planPid }));
  },

  createStripePortal() {
    return request<{ url: string }>('/api/user/stripe/portal', post({}));
  },
};
