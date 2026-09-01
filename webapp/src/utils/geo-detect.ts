/**
 * Client-side country detection — no backend dependency.
 *
 * Replaces the `/api/geo` IP-based detection, which is both frozen by a
 * server-side hotfix (always returns `cn`) and structurally unreliable: with
 * the tunnel up, control-plane requests exit at the VPN node, so the server
 * sees the exit node's country, not the user's (the geo-via-tunnel pollution
 * documented in hooks/useUser.ts). The system timezone is immune to tunnel
 * state, works offline, and sends nothing anywhere.
 *
 * The old endpoint stays untouched on the server for released clients; new
 * clients simply never call it.
 */
import { TZ_COUNTRY } from './tz-country.gen';

/** The system IANA timezone name, or null when the runtime can't provide one. */
export function getSystemTimeZone(): string | null {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone ?? null;
  } catch {
    return null;
  }
}

/**
 * Map an IANA timezone name to a lowercase ISO 3166-1 alpha-2 country code.
 * Deprecated zone aliases (Asia/Saigon, PRC, …) are covered by the table.
 */
export function countryFromTimeZone(tz: string | null | undefined): string | null {
  if (!tz) return null;
  return TZ_COUNTRY[tz] ?? null;
}

/**
 * Detect the user's country from the system timezone.
 *
 * Returns the raw detected country (which may have no dedicated routing
 * profile — callers that feed routing must clamp via `routableCountry()`),
 * or null when the timezone is unknown/unavailable.
 */
export function detectCountry(): string | null {
  return countryFromTimeZone(getSystemTimeZone());
}
