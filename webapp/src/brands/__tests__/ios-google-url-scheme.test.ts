/**
 * GoogleSignIn on iOS refuses to start unless the reversed iOS client ID is a
 * registered URL scheme. That scheme comes from the brand xcconfig
 * (K2_GOOGLE_REVERSED_CLIENT_ID → Info.plist CFBundleURLTypes) while the client
 * ID itself comes from the brand registry — two hand-written copies, so lock them.
 * brand-active.xcconfig is the committed copy of the kaitu one (the build
 * overwrites it per brand) and must stay identical to it.
 */
import { readFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { describe, it, expect } from 'vitest';
import { KAITU_BRAND } from '../kaitu';
import { OVERLEAP_BRAND } from '../overleap';

const configDir = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  '../../../../mobile/ios/App/App/Config'
);

function reversedScheme(xcconfig: string): string | undefined {
  const text = readFileSync(path.join(configDir, xcconfig), 'utf-8');
  return /^K2_GOOGLE_REVERSED_CLIENT_ID\s*=\s*(\S+)\s*$/m.exec(text)?.[1];
}

function reverse(clientId: string): string {
  const suffix = '.apps.googleusercontent.com';
  expect(clientId.endsWith(suffix)).toBe(true);
  return `com.googleusercontent.apps.${clientId.slice(0, -suffix.length)}`;
}

describe('iOS Google URL scheme matches the brand iOS client ID', () => {
  it.each([
    ['brand-kaitu.xcconfig', KAITU_BRAND],
    ['brand-active.xcconfig', KAITU_BRAND],
    ['brand-overleap.xcconfig', OVERLEAP_BRAND],
  ])('%s', (file, brand) => {
    expect(reversedScheme(file)).toBe(reverse(brand.social.googleIosClientId));
  });
});
