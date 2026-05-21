import type { ZxcvbnResult } from '@zxcvbn-ts/core';

export const PASSWORD_MIN_LENGTH = 10;
export const PASSWORD_MIN_SCORE = 3;

export interface StrengthResult {
  score: 0 | 1 | 2 | 3 | 4;
  isValid: boolean;
  tooShort: boolean;
}

let zxcvbnPromise: Promise<typeof import('@zxcvbn-ts/core').zxcvbn> | null = null;

// Lazy-load zxcvbn + language packs so the cold-start bundle stays small —
// PasswordDialog opens on demand, not on app boot.
async function loadZxcvbn() {
  if (!zxcvbnPromise) {
    zxcvbnPromise = (async () => {
      const [core, common, en] = await Promise.all([
        import('@zxcvbn-ts/core'),
        import('@zxcvbn-ts/language-common'),
        import('@zxcvbn-ts/language-en'),
      ]);
      core.zxcvbnOptions.setOptions({
        translations: en.translations,
        dictionary: { ...common.dictionary, ...en.dictionary },
        graphs: common.adjacencyGraphs,
      });
      return core.zxcvbn;
    })().catch((e) => {
      // On failure, clear the cached promise so the next keystroke can
      // retry — otherwise an offline blip would pin the user into a
      // permanently-broken strength meter.
      zxcvbnPromise = null;
      throw e;
    });
  }
  return zxcvbnPromise;
}

/**
 * Compute zxcvbn-based password strength.
 *
 * Lazy-loads the ~250KB zxcvbn-ts language packs on first call. On
 * dynamic-import failure (offline / blocked CDN / missing chunk after
 * redeploy) the function rejects with the loader's error AND clears the
 * internal cache so the next keystroke retries.
 *
 * @param password - The password to evaluate.
 * @param userInputs - Tokens (e.g. the user's email) zxcvbn should penalize.
 */
export async function checkPasswordStrength(
  password: string,
  userInputs: string[] = [],
): Promise<StrengthResult> {
  if (!password) {
    return { score: 0, isValid: false, tooShort: true };
  }
  const tooShort = password.length < PASSWORD_MIN_LENGTH;
  const zxcvbn = await loadZxcvbn();
  const result: ZxcvbnResult = zxcvbn(password, userInputs);
  // Clamp to 0..4 defensively — zxcvbn-ts's contract is 0-4 but we don't
  // want a future minor that returns 5 to crash downstream switch
  // statements with a blank meter.
  const raw = Math.round(result.score);
  const score = (Math.max(0, Math.min(4, raw)) as StrengthResult['score']);
  return {
    score,
    tooShort,
    isValid: !tooShort && score >= PASSWORD_MIN_SCORE,
  };
}
