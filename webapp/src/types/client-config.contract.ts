/**
 * Compile-time wire-contract lock for client-config.ts.
 *
 * webapp's tsconfig excludes test files from `tsc --noEmit`, and vitest
 * strips types without checking them — a .test.ts file cannot fail on a
 * type regression. This file deliberately lives outside the exclude globs:
 * CI's `npx tsc --noEmit` checks it, and nothing imports it, so it never
 * ships in a bundle. Everything is exported only to satisfy noUnusedLocals.
 */
import type { MatchConfig, PresetName, RouteConfig } from './client-config';

// PresetName must accept the SP3 additions.
export const gamesPreset: PresetName = 'games';
export const tmPreset: PresetName = 'tm-access';
export const kzPreset: PresetName = 'kz-access';
export const uzPreset: PresetName = 'uz-access';

// MatchConfig must accept SP2's connection-time dimensions.
export const portRule: MatchConfig = { network: 'udp', port: ['27015', '27000-28000'] };
export const protocolRule: MatchConfig = { protocol: ['stun', 'dtls', 'quic', 'bittorrent'] };

// protocol is a closed enum — a typo must not compile. If someone widens the
// field to string[], this @ts-expect-error goes unused and tsc turns red.
// @ts-expect-error 'http' is not a sniffable protocol
export const badProtocolRule: MatchConfig = { protocol: ['http'] };

export const gamesRoute: RouteConfig = { via: 'direct', match: { preset: 'games' } };
