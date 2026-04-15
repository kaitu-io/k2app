/**
 * Retry policy for engine connect failures.
 *
 * Engine error codes (k2/engine/error.go) are HTTP-aligned. When _k2.run('up')
 * fails on mobile smart mode, we want to swap to a different tunnel from the
 * cached candidate set IF the failure looks node-specific (network / protocol
 * / unreachable), but NOT if it's account-level (auth / quota) — picking a
 * different node won't fix those.
 */
export function isRetryableEngineError(code: number | undefined): boolean {
  if (!code) return false;
  switch (code) {
    case 502: // ProtocolError — TLS/QUIC handshake fail (likely node-specific)
    case 503: // ServerUnreachable — TCP dial failed
    case 570: // ConnectionFatal / no outbound — last-resort, try another node
      return true;
    default:
      return false;
  }
}
